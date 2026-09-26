package main

// terminal_history_test.go —— 会话历史文件的**写入侧上限**（审查 C7）。
//
// 旧实现只在读取侧限制 maxHistoryBytes（attach 回放 / /api/session/history），
// 历史文件本身只追加不截断：会话一直开着、输出一直在刷，临时目录就会无限增长。
//
// 现在 pump 的每次追加都经 Session.appendHistoryLocked → trimHistoryLocked：
// 超过 maxHistoryBytes 就滚动截断、只保留末尾 maxHistoryBytes 字节。
// 语义保持不变：/api/sessions 仍能列出会话（浏览器断开不杀会话），重连仍能回放
// 「最近」的历史，closeByID/CloseAll 仍负责删除文件。
//
// 为什么不做「已退出会话的 TTL 回收」：读取侧本来只看末尾一段，滚动截断已让磁盘占用
// 有界；TTL 回收会让「浏览器断开 → 稍后回来重连」这条被明确要求保留的路径出现新语义
// （过期后历史为空、会话从列表消失），风险更大且解决不了「运行中的会话」那一半。
//
// 沙箱里建不起 PTY（/dev/ptmx 权限），所以这里不测 pump 本体，只测它与 pump 共用
// 的这段纯文件逻辑（appendHistoryLocked / history 读取路径）。

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// newHistorySession 造一个只带历史文件的 Session（不建 PTY）。
func newHistorySession(t *testing.T, id string) (*Session, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), id+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return &Session{id: id, hist: f}, path
}

// appendHistory 在 histMu 内追加一段输出（与 pump 的调用方式一致）。
func appendHistory(s *Session, chunk []byte) {
	s.histMu.Lock()
	s.appendHistoryLocked(chunk)
	s.histMu.Unlock()
}

// 连续写入远超上限的数据后，文件必须被截断到上限以内，且保留的是**末尾**（最近输出）。
func TestHistoryFileIsTrimmedOnWrite(t *testing.T) {
	s, path := newHistorySession(t, "trim")
	chunk := bytes.Repeat([]byte("A"), 64*1024)
	written := 0
	for written < maxHistoryBytes*3/2 {
		appendHistory(s, chunk)
		written += len(chunk)
	}
	// 末尾写一段独特内容：滚动截断必须保留它（回放「最近历史」的关键）。
	tail := []byte("THE-VERY-LAST-OUTPUT\n")
	appendHistory(s, tail)

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > int64(maxHistoryBytes) {
		t.Fatalf("历史文件超过写入侧上限：%d > %d（旧实现只追加不截断）", st.Size(), maxHistoryBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, tail) {
		t.Fatal("滚动截断必须保留末尾（最近输出），否则重连回放会缺最新内容")
	}
	if len(data) != int(st.Size()) {
		t.Fatalf("文件长度与读到的字节不一致: %d vs %d", st.Size(), len(data))
	}
}

// 截断之后读取路径（SessionManager.history，即 /api/session/history 与 attach 回放
// 共用的一段）仍要能拿到最近的输出、且不超过读取侧上限。
func TestHistoryReadAfterTrim(t *testing.T) {
	s, _ := newHistorySession(t, "read")
	mgr := &SessionManager{renv: &RuntimeEnv{}, sessions: map[string]*Session{"read": s}}

	chunk := bytes.Repeat([]byte("B"), 128*1024)
	for i := 0; i < 40; i++ { // 5 MiB，超过上限
		appendHistory(s, chunk)
	}
	tail := []byte("recent-output\n")
	appendHistory(s, tail)

	got, err := mgr.history("read")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(got) > maxHistoryBytes {
		t.Fatalf("history 返回 %d 字节，超过读取侧上限 %d", len(got), maxHistoryBytes)
	}
	if !bytes.HasSuffix(got, tail) {
		t.Fatal("history 必须能读到最近输出")
	}
}

// 清屏（clearHistory → Truncate(0)）之后不能因为「文件变小」而出现任何异常：
// 后续追加与读取都要正常。
func TestHistoryAppendAfterClear(t *testing.T) {
	s, path := newHistorySession(t, "clear")
	mgr := &SessionManager{renv: &RuntimeEnv{}, sessions: map[string]*Session{"clear": s}}

	appendHistory(s, bytes.Repeat([]byte("x"), 256*1024))
	if err := mgr.clearHistory("clear"); err != nil {
		t.Fatalf("clearHistory: %v", err)
	}
	appendHistory(s, []byte("after-clear\n"))

	got, err := mgr.history("clear")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "after-clear\n" {
		t.Fatalf("清屏后应只剩新内容，实际 %q", got)
	}
	if st, err := os.Stat(path); err != nil || st.Size() != int64(len("after-clear\n")) {
		t.Fatalf("文件长度不符: %v %v", st, err)
	}
}
