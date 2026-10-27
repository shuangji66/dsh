package main

// server_shim_test.go —— 终端里的 dsh 稳定入口（${数据目录}/server/.bin/dsh）。
//
// 终端 PATH 只在会话创建时构造一次，而 dsh 变成多版本后每个版本各有自己的
// node_modules/.bin。这里钉住三件事：
//   - 切换选中版本时稳定链接跟着改指向；
//   - 选中版本被删掉（配置没来得及改）时链接要移除，不能留下悬空入口；
//   - .bin 这个隐藏目录不会被 installedVersions / 版本列表当成一个版本。

import (
	"os"
	"testing"
)

func TestSyncDshShimFollowsSelectedVersion(t *testing.T) {
	dataDir := t.TempDir()
	prevCfg := GetConfig()
	initConfig(&AppConfig{})
	t.Cleanup(func() { initConfig(&prevCfg) })

	renv := &RuntimeEnv{DataDir: dataDir}
	bin1 := fakeInstalledVersion(t, dataDir, "0.2.0-rc.2")
	bin2 := fakeInstalledVersion(t, dataDir, "0.2.1-rc.2")

	cfg := GetConfig()
	cfg.DshVersion = "0.2.0-rc.2"
	initConfig(&cfg)
	if err := syncDshShim(renv); err != nil {
		t.Fatalf("sync shim: %v", err)
	}
	if got, err := os.Readlink(serverShimDshFor(renv)); err != nil || got != bin1 {
		t.Fatalf("shim target = %q, err = %v, want %q", got, err, bin1)
	}

	// 切换版本后同一个路径改指向新版本（终端会话不必重开）。
	cfg = GetConfig()
	cfg.DshVersion = "0.2.1-rc.2"
	initConfig(&cfg)
	if err := syncDshShim(renv); err != nil {
		t.Fatalf("sync shim after switch: %v", err)
	}
	if got, err := os.Readlink(serverShimDshFor(renv)); err != nil || got != bin2 {
		t.Fatalf("shim target after switch = %q, err = %v, want %q", got, err, bin2)
	}

	// 选中版本的目录被外部删掉：链接必须移除，不能指向一个不存在的可执行文件。
	if err := os.RemoveAll(versionDirFor(renv, "0.2.1-rc.2")); err != nil {
		t.Fatal(err)
	}
	if err := syncDshShim(renv); err != nil {
		t.Fatalf("sync shim with missing version: %v", err)
	}
	if _, err := os.Lstat(serverShimDshFor(renv)); !os.IsNotExist(err) {
		t.Fatalf("dangling shim should be removed, got err = %v", err)
	}
}

func TestShimDirIsNotListedAsInstalledVersion(t *testing.T) {
	dataDir := t.TempDir()
	_, srv := newServerTestManager(t, dataDir)
	fakeInstalledVersion(t, dataDir, "0.2.0-rc.2")

	cfg := GetConfig()
	cfg.DshVersion = "0.2.0-rc.2"
	initConfig(&cfg)
	if err := syncDshShim(srv.renv); err != nil {
		t.Fatalf("sync shim: %v", err)
	}

	installed := srv.installedVersions()
	if len(installed) != 1 || installed[0] != "0.2.0-rc.2" {
		t.Fatalf("installed versions = %v, want only 0.2.0-rc.2 (shim dir must be ignored)", installed)
	}
}
