package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sectionEntries 返回顶层键 sectionKey 下所有非空、非注释条目的缩进宽度。
// 用于断言同一块映射里的条目缩进一致（YAML 块映射一旦混排缩进就是语法错误）。
func sectionEntries(t *testing.T, lines []string, sectionKey string) []string {
	t.Helper()
	start, end, found := findSection(lines, sectionKey)
	if !found {
		t.Fatalf("区块 %s 不存在:\n%s", sectionKey, strings.Join(lines, "\n"))
	}
	var out []string
	for i := start + 1; i < end; i++ {
		ln := lines[i]
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		out = append(out, ln)
	}
	return out
}

// 回归：文件里已有同键但值不同时（升级固定版本），改写必须保持同区缩进一致。
// 旧实现写成 "  " + entryLine（entryLine 自带 2 格），产出 4 格条目与 2 格兄弟混排，
// yaml 解析直接 ParserError，pnpm install 会持续失败。
func TestEnsureWorkspaceYAMLRewriteKeepsIndent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pnpm-workspace.yaml")
	before := "overrides:\n  node-pty: 1.2.0-beta.14\n  other-pkg: 1.0.0\nallowBuilds:\n  node-pty: true\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, _, err := ensureWorkspaceYAML(dir)
	if err != nil {
		t.Fatalf("ensureWorkspaceYAML: %v", err)
	}
	if !changed {
		t.Fatal("固定版本从 beta.14 升到 beta.15，应判定为发生改动")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")

	for _, section := range []string{"overrides", "allowBuilds"} {
		entries := sectionEntries(t, lines, section)
		if len(entries) == 0 {
			t.Fatalf("%s 区块没有条目:\n%s", section, string(data))
		}
		for _, e := range entries {
			if !strings.HasPrefix(e, "  ") || strings.HasPrefix(e, "   ") {
				t.Fatalf("%s 区块条目缩进不是 2 格（会产出非法 YAML）: %q\n全文:\n%s", section, e, string(data))
			}
		}
	}

	// 值确实被改写，且原有兄弟条目仍在。
	body := string(data)
	if !strings.Contains(body, "  node-pty: 1.2.0-beta.15\n") {
		t.Fatalf("固定版本未改写为 beta.15:\n%s", body)
	}
	if !strings.Contains(body, "  other-pkg: 1.0.0\n") {
		t.Fatalf("同区其它条目被破坏:\n%s", body)
	}

	// 幂等：再跑一次不应再改动。
	changed, _, err = ensureWorkspaceYAML(dir)
	if err != nil {
		t.Fatalf("第二次 ensureWorkspaceYAML: %v", err)
	}
	if changed {
		t.Fatal("值已一致，第二次调用不应判定为改动")
	}
}

// 区块存在但缺该键：插入的条目缩进必须与同区其它条目一致。
func TestEnsureWorkspaceYAMLInsertKeepsIndent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pnpm-workspace.yaml")
	before := "overrides:\n  other-pkg: 1.0.0\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ensureWorkspaceYAML(dir); err != nil {
		t.Fatalf("ensureWorkspaceYAML: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range sectionEntries(t, strings.Split(string(data), "\n"), "overrides") {
		if !strings.HasPrefix(e, "  ") || strings.HasPrefix(e, "   ") {
			t.Fatalf("插入的条目缩进不是 2 格: %q\n全文:\n%s", e, string(data))
		}
	}
}

// 文件不存在：创建最小配置，缩进同样必须是 2 格。
func TestEnsureWorkspaceYAMLCreateFile(t *testing.T) {
	dir := t.TempDir()
	changed, _, err := ensureWorkspaceYAML(dir)
	if err != nil {
		t.Fatalf("ensureWorkspaceYAML: %v", err)
	}
	if !changed {
		t.Fatal("文件不存在时应报告创建了配置")
	}
	data, err := os.ReadFile(filepath.Join(dir, "pnpm-workspace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"overrides", "allowBuilds"} {
		for _, e := range sectionEntries(t, strings.Split(string(data), "\n"), section) {
			if !strings.HasPrefix(e, "  ") || strings.HasPrefix(e, "   ") {
				t.Fatalf("%s 条目缩进不是 2 格: %q", section, e)
			}
		}
	}
}
