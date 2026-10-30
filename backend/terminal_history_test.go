//go:build !noterminal

// 终端会话历史文件的上限（见 terminal.go）。终端被编译剔除时本文件一并跳过。

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
	"strings"
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

// 清屏之后不能因为「文件变小」而出现任何异常：后续追加与读取都要正常。
// （这份历史整份没有换行，长度又远超保留窗口，所以清屏后为空 —— 见后面的
// TestClearKeepsCurrentPromptLine 与 TestClearDropsOverlongUnsplittableTail。）
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

// 清屏必须留下**当前行**。终端的可见状态就是「当前行 + 光标列」，而当前行（提示符，
// 可能还带着用户敲了一半的命令）在 PTY 侧并没有消失；历史文件是重连时唯一的回放源，
// 整份截到 0 的话前端 reset 掉屏幕后没有任何字节可回放 —— bash 以为提示符还在屏幕上、
// 不会重打，重连后就是一个空白终端，必须手动回车一次才「有消息」（用户报告的现象）。
func TestClearKeepsCurrentPromptLine(t *testing.T) {
	s, _ := newHistorySession(t, "keep")
	mgr := &SessionManager{renv: &RuntimeEnv{}, sessions: map[string]*Session{"keep": s}}

	appendHistory(s, []byte("old output\r\nsecond line\r\n"))
	// 真实会话的当前行：readline 的粘贴模式开关 + 提示符（无结尾换行）
	prompt := []byte("\x1b[?2004l\r\x1b[?2004hHarness@fnOS:~$ ")
	appendHistory(s, prompt)

	if err := mgr.clearHistory("keep"); err != nil {
		t.Fatalf("clearHistory: %v", err)
	}
	got, err := mgr.history("keep")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, prompt) {
		t.Fatalf("清屏后应只留当前行 %q，实际 %q", prompt, got)
	}

	// 清屏后继续输出：新内容接在保留的当前行后面（回放顺序与真实屏幕一致）
	appendHistory(s, []byte("ls\r\nfile1\r\n"))
	got, err = mgr.history("keep")
	if err != nil {
		t.Fatal(err)
	}
	if want := string(prompt) + "ls\r\nfile1\r\n"; string(got) != want {
		t.Fatalf("清屏后追加内容错位：want %q, got %q", want, got)
	}
}

// 当前行为空（文件以换行结尾，光标停在空行行首）时没有可保留的内容：整份清空。
func TestClearDropsEmptyCurrentLine(t *testing.T) {
	s, _ := newHistorySession(t, "emptyline")
	mgr := &SessionManager{renv: &RuntimeEnv{}, sessions: map[string]*Session{"emptyline": s}}

	appendHistory(s, []byte("first\r\nsecond\r\n"))
	if err := mgr.clearHistory("emptyline"); err != nil {
		t.Fatalf("clearHistory: %v", err)
	}
	got, err := mgr.history("emptyline")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("当前行为空时清屏应清空整份历史，实际 %q", got)
	}
}

// 最后一行长到超过保留窗口（进度条 / 全屏 TUI 这类长时间不换行的输出）时不该被留下：
// 清屏要真的清掉，不能让「找不到换行」把几 MB 的原样留在文件里。
func TestClearDropsOverlongUnsplittableTail(t *testing.T) {
	s, _ := newHistorySession(t, "longtail")
	mgr := &SessionManager{renv: &RuntimeEnv{}, sessions: map[string]*Session{"longtail": s}}

	appendHistory(s, []byte("done\r\n"))
	appendHistory(s, bytes.Repeat([]byte("y"), clearKeepLineBytes*2))
	if err := mgr.clearHistory("longtail"); err != nil {
		t.Fatalf("clearHistory: %v", err)
	}
	got, err := mgr.history("longtail")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("超长当前行应整份清空，实际剩 %d 字节", len(got))
	}
}

// 整份历史只有一行（连换行都没有）：它就是当前行，清屏后要留下（回放即屏幕内容）。
func TestClearKeepsSingleLineHistory(t *testing.T) {
	s, _ := newHistorySession(t, "oneline")
	mgr := &SessionManager{renv: &RuntimeEnv{}, sessions: map[string]*Session{"oneline": s}}

	line := []byte("Harness@fnOS:~$ echo hi")
	appendHistory(s, line)
	if err := mgr.clearHistory("oneline"); err != nil {
		t.Fatalf("clearHistory: %v", err)
	}
	got, err := mgr.history("oneline")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, line) {
		t.Fatalf("单行历史应原样保留，实际 %q", got)
	}
}

// 清屏 → 重连（attach 回放）这条路径的端到端回归：回放的字节里必须还带着当前行的
// 提示符。整份清零时这里回放不出任何内容，前端 reset 掉屏幕后就是一片空白 ——
// 用户看到的现象正是「清空后点重连没有消息，要手动回车一次」。
func TestReplayAfterClearStillShowsPrompt(t *testing.T) {
	s, _ := newHistorySession(t, "replay")
	mgr := &SessionManager{renv: &RuntimeEnv{}, sessions: map[string]*Session{"replay": s}}

	appendHistory(s, []byte("Harness@fnOS:~$ ls\r\nfile1  file2\r\n"))
	prompt := []byte("\x1b[?2004l\r\x1b[?2004hHarness@fnOS:~$ ")
	appendHistory(s, prompt)

	if err := mgr.clearHistory("replay"); err != nil {
		t.Fatalf("clearHistory: %v", err)
	}

	c := &fakeConn{}
	if _, err := s.attach(c); err != nil {
		t.Fatalf("attach: %v", err)
	}
	replay := c.bytes()
	if !bytes.Contains(replay, prompt) {
		t.Fatalf("重连回放里必须带当前行提示符 %q，实际 %q", prompt, replay)
	}
	if bytes.Contains(replay, []byte("file1")) {
		t.Fatalf("清屏后不该再回放旧内容，实际 %q", replay)
	}
	if !bytes.HasSuffix(replay, []byte("\x1b]ready\x07")) {
		t.Fatal("回放结束后必须紧跟 ready 帧（前端据此结束历史回放）")
	}
}

// 终端会话镜像目录的默认解析：空 RuntimeEnv（部分单测直接构造字面量）必须落回统一
// 数据目录下的 terminal-sessions，不能各自拼路径。
//
// 这条断言原本在 data_dir_test.go：无终端构建（-tags noterminal）里 SessionManager
// 只是空壳，该文件要能在两种构建下编译，因此把终端专属的断言挪到这里。
func TestSessionDirFallsBackToDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dataDir)
	if p := (&SessionManager{}).sessionDir(); p != filepath.Join(dataDir, "terminal-sessions") {
		t.Errorf("sessionDir(空 renv) = %q", p)
	}
}

func TestTerminalEnvPrependsStableDshShimDir(t *testing.T) {
	renv := &RuntimeEnv{DataDir: t.TempDir(), Path: "/usr/bin:/bin"}
	env := terminalEnv(renv, "")

	pathVal := ""
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			pathVal = strings.TrimPrefix(e, "PATH=")
		}
	}
	want := serverShimDirFor(renv)
	if !strings.HasPrefix(pathVal, want+":") {
		t.Fatalf("PATH = %q, want prefix %q", pathVal, want+":")
	}
	if !strings.HasSuffix(pathVal, ":/usr/bin:/bin") {
		t.Fatalf("PATH = %q, want original platform PATH preserved", pathVal)
	}
}
