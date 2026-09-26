package main

// pending_test.go —— 「待安装更新包按 kind 隔离」的回归测试（审查第 7 条）。
//
// 旧实现只有一个 pending 槽：先后下载 harness 与 dsh 时，后一份把前一份挤掉
// （文件不删、前一份的状态还停在「已下载待安装」，点安装报「尚未下载」）；
// 而「删除更新包」不看 kind，会把另一个 kind 刚下好的包删掉。
// dsh-data-* 与市场包同理。

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFakePkg(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPendingIsolatedPerKind(t *testing.T) {
	m := &UpdateManager{}
	dir := t.TempDir()
	harnessPkg := writeFakePkg(t, dir, "harness-1.0.0.tar.gz")
	dshPkg := writeFakePkg(t, dir, "dsh-0.2.0.tar.gz")

	m.setPending(&PendingUpdate{Kind: updateKindHarness, Version: "1.0.0", PkgPath: harnessPkg})
	m.setPending(&PendingUpdate{Kind: updateKindDsh, Version: "0.2.0", PkgPath: dshPkg})

	if m.getPending(updateKindHarness) == nil || m.getPending(updateKindDsh) == nil {
		t.Fatal("两种 kind 的待安装包应各自保留（旧实现里后下载的会挤掉前一个）")
	}

	// 删除 harness 的待安装包：不得动 dsh 的文件与条目。
	m.clearPending(updateKindHarness)
	if m.getPending(updateKindHarness) != nil {
		t.Fatal("harness 的待安装条目应被清除")
	}
	if m.getPending(updateKindDsh) == nil {
		t.Fatal("dsh 的待安装条目不应被清除")
	}
	if _, err := os.Stat(dshPkg); err != nil {
		t.Fatalf("删除 harness 更新包不应删掉 dsh 的包: %v", err)
	}
	if _, err := os.Stat(harnessPkg); err == nil {
		t.Fatal("被清除的 harness 更新包文件应被删除")
	}

	// 同 kind 重下（换文件名）时清理旧文件 —— 原有语义保持不变。
	newer := writeFakePkg(t, dir, "harness-1.0.1.tar.gz")
	m.setPending(&PendingUpdate{Kind: updateKindHarness, Version: "1.0.1", PkgPath: newer})
	if _, err := os.Stat(harnessPkg); err == nil {
		t.Fatal("同 kind 重下应清掉旧文件")
	}
	if _, err := os.Stat(dshPkg); err != nil {
		t.Fatalf("同 kind 重下不应影响另一种 kind: %v", err)
	}
}

// DiscardUpdate 必须只清自己这个 kind：另一种 kind 的待安装包与其状态不受影响。
func TestDiscardUpdateDoesNotTouchOtherKind(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	m := &UpdateManager{
		statuses: map[updateKind]*UpdateStatus{
			updateKindHarness: {Kind: updateKindHarness},
			updateKindDsh:     {Kind: updateKindDsh},
		},
	}
	dir := m.pendingDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	harnessPkg := writeFakePkg(t, dir, "harness-1.0.0.tar.gz")
	dshPkg := writeFakePkg(t, dir, "dsh-0.2.0.tar.gz")
	m.setPending(&PendingUpdate{Kind: updateKindHarness, Version: "1.0.0", PkgPath: harnessPkg})
	m.setPending(&PendingUpdate{Kind: updateKindDsh, Version: "0.2.0", PkgPath: dshPkg})

	if err := m.DiscardUpdate(updateKindHarness); err != nil {
		t.Fatalf("DiscardUpdate: %v", err)
	}

	if m.getPending(updateKindHarness) != nil {
		t.Fatal("harness 的待安装条目应被删除")
	}
	if m.getPending(updateKindDsh) == nil {
		t.Fatal("dsh 的待安装条目不应被删除")
	}
	if _, err := os.Stat(dshPkg); err != nil {
		t.Fatalf("删除 harness 更新包不得删掉 dsh 的包: %v", err)
	}
	if st := m.getStatus(updateKindDsh); st.ReadyToInstall {
		t.Fatal("dsh 的状态不应被 harness 的删除操作复位（其待安装包仍在）")
	}
}
