package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// setSSEHeaders prepares a response as a Server-Sent Events stream.
func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

// sseSend writes a named SSE event and flushes it.
func sseSend(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// sseJSON encodes v as a compact JSON string for an SSE data line.
func sseJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// handleVisitorsStream streams the visitor list to the overview page via SSE.
// The backend pushes an update whenever the visitor set changes (login/logout);
// the client never polls.
func (m *AdminMux) handleVisitorsStream(w http.ResponseWriter, r *http.Request) {
	setSSEHeaders(w)
	ctx := r.Context()

	// 初始快照
	sseSend(w, "visitors", sseJSON(m.auth.Visitors()))

	ch, unsub := m.auth.visitors.subscribe()
	defer unsub()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			sseSend(w, "visitors", sseJSON(m.auth.Visitors()))
		case <-keepalive.C:
			// 心跳：顺带清理已过期的访客并重新推送，保证过期记录自动消失
			m.auth.visitors.PurgeExpired(time.Now())
			sseSend(w, "visitors", sseJSON(m.auth.Visitors()))
		}
	}
}

// handleDshStream 每秒推送一次 dsh 状态（运行状态、CPU 使用率、内存占用等），
// 供概览页实时刷新 CPU / 内存占用，取代客户端轮询。
func (m *AdminMux) handleDshStream(w http.ResponseWriter, r *http.Request) {
	setSSEHeaders(w)
	ctx := r.Context()

	// 初始快照
	sseSend(w, "status", sseJSON(m.dsh.Status()))

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sseSend(w, "status", sseJSON(m.dsh.Status()))
		}
	}
}

// readLogSnapshot reads the current log file content (capped) plus its path and
// existence flag. It returns an empty payload when the file is unreadable.
func readLogSnapshot() map[string]interface{} {
	path := os.Getenv("HARNESS_LOG_FILE")
	payload := map[string]interface{}{
		"path":   path,
		"exists": path != "",
		"content": "",
	}
	if path == "" {
		return payload
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return payload
	}
	content := string(b)
	const maxLen = 500 * 1024
	if len(content) > maxLen {
		content = "…（日志过长，仅显示末尾）\n" + content[len(content)-maxLen:]
	}
	payload["content"] = content
	payload["exists"] = true
	return payload
}

// handleLogsStream streams log content to the log page via SSE. The backend
// checks for file changes and only pushes when content actually changes, so
// the client receives push updates instead of polling. The detection interval
// is adaptive: it starts at 1s, switches to 15s after 15 consecutive no-change
// checks, and reverts to 1s as soon as a change is detected.
func (m *AdminMux) handleLogsStream(w http.ResponseWriter, r *http.Request) {
	setSSEHeaders(w)
	ctx := r.Context()

	const fastInterval = time.Second
	const slowInterval = 15 * time.Second
	const idleThreshold = 15

	last := ""
	noChangeCount := 0
	interval := fastInterval

	sendIfChanged := func() bool {
		snap := readLogSnapshot()
		content, _ := snap["content"].(string)
		if content == last {
			return false
		}
		last = content
		sseSend(w, "log", sseJSON(snap))
		return true
	}

	sendIfChanged() // 初始推送

	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			changed := sendIfChanged()
			if changed {
				// 检测到变化：恢复快频率
				noChangeCount = 0
				interval = fastInterval
			} else {
				// 连续多次无变化：降低检测频率，减轻后端压力
				noChangeCount++
				if noChangeCount >= idleThreshold {
					interval = slowInterval
				}
			}
			timer.Reset(interval)
		}
	}
}