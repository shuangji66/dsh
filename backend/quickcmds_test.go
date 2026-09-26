package main

// quickcmds_test.go —— 快捷指令持久化的原子写（审查 C10）。
//
// 旧实现用固定的 `<path>.tmp`：两次保存共用同一个临时文件（config.go 的 SaveConfig
// 修的是同一类问题）。现在改成 os.CreateTemp（同目录唯一名）+ Chmod(0600) +
// Write + Sync + Close + Rename，失败路径清掉临时文件。
//
// 说明：本包内 saveQuickCmds 由 quickCmdsMu 串行化，所以「同一进程内两次保存互相
// 覆盖临时文件」在今天并非可直接触发的路径；这次改动消除的是共用名字这个隐患
// （跨进程/未来去掉锁的实现都会踩），同时把权限收到 0600 并补上 fsync。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// 落盘文件的权限与 config.json 同档（0600），且不残留任何临时文件。
func TestSaveQuickCmdsUsesPrivateModeAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	cmds := []QuickCmd{{ID: "1", Name: "列表", Content: "ls -la", Auto: true}}
	if err := saveQuickCmds(path, cmds); err != nil {
		t.Fatalf("saveQuickCmds: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("权限 = %o, want 600（旧实现写 0644）", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "quickcmds.json" {
			t.Fatalf("保存后残留临时文件: %s", e.Name())
		}
	}

	got, err := loadQuickCmds(path)
	if err != nil {
		t.Fatalf("loadQuickCmds: %v", err)
	}
	if len(got) != 1 || got[0].Content != "ls -la" {
		t.Fatalf("读回内容不符: %+v", got)
	}
}

// 失败路径（rename 落到一个目录上）必须清掉自己的临时文件：旧实现会留下
// `<path>.tmp`，在配置目录里越堆越多。
func TestSaveQuickCmdsCleansTempFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	// 目标路径本身是一个目录 → rename 必定失败。
	path := filepath.Join(dir, "quickcmds.json")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := saveQuickCmds(path, []QuickCmd{{ID: "1", Name: "n", Content: "ls"}}); err == nil {
		t.Fatal("rename 到目录上应失败")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("失败路径残留临时文件: %s", e.Name())
		}
	}
}

// 并发保存若干份不同的列表：最终文件必须始终是一份完整可解析的 JSON
// （不出现「文件在但内容被截断」），并且每次读到的都是某一次完整保存的内容。
func TestSaveQuickCmdsConcurrentStaysParseable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")

	const writers, rounds = 6, 15
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				cmds := []QuickCmd{{
					ID:      fmt.Sprintf("id-%d-%d", w, r),
					Name:    "n",
					Content: strings.Repeat("x", 2048),
				}}
				if err := saveQuickCmds(path, cmds); err != nil {
					t.Errorf("并发保存失败: %v", err)
					return
				}
			}
		}(w)
	}

	// 保存过程中持续读取：任何时刻都不允许读到半写文件。
	done := make(chan struct{})
	readErr := make(chan error, 1)
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			raw, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue // 第一次 rename 之前文件还不存在
				}
				select {
				case readErr <- err:
				default:
				}
				return
			}
			var envelope quickCmdsFile
			if err := json.Unmarshal(raw, &envelope); err != nil {
				select {
				case readErr <- fmt.Errorf("读到半写文件: %v", err):
				default:
				}
				return
			}
		}
	}()

	wg.Wait()
	<-done
	select {
	case err := <-readErr:
		t.Fatal(err)
	default:
	}

	if _, err := loadQuickCmds(path); err != nil {
		t.Fatalf("最终文件不可解析: %v", err)
	}
}
