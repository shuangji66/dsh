package main

// update_tgz_test.go —— dsh 数据备份的排除规则（审查 C8）。
//
// 旧实现按**相对路径后缀**排除：`if strings.HasSuffix(rel, ".tar.gz") { return nil }`，
// 而备份产物落在 backupDir（$TRIM_PKGVAR/backup），根本不在 srcDir（$HOME/.dsh）内 ——
// 于是 srcDir 下用户自己的任何 .tar.gz 都不会进备份（静默丢数据，恢复时才发现）。
// 现在只排除「与本次 destFile 同一个绝对路径」的那个文件。

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readTarEntries 读回 tgz 的条目名与内容（只取常规文件）。
func readTarEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	out := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = string(data)
	}
	return out
}

// ~/.dsh 下用户自己的 .tar.gz 必须进备份（旧实现整类跳过）。
func TestTgzDirAsKeepsUserArchives(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "x.tar.gz"), []byte("user-archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "backups", "y.tar.gz"), []byte("nested-archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "settings.yaml"), []byte("k: v"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "dsh-data-1.0.0-20260101000000.tar.gz")

	if err := tgzDirAs(src, dest, ".dsh"); err != nil {
		t.Fatalf("tgzDirAs: %v", err)
	}
	entries := readTarEntries(t, dest)
	for name, want := range map[string]string{
		".dsh/x.tar.gz":         "user-archive",
		".dsh/backups/y.tar.gz": "nested-archive",
		".dsh/settings.yaml":    "k: v",
	} {
		got, ok := entries[name]
		if !ok {
			t.Fatalf("备份缺少条目 %q（旧实现按 .tar.gz 后缀整类排除，用户数据静默丢失）；实得 %v", name, keysOf(entries))
		}
		if got != want {
			t.Fatalf("条目 %q 内容 = %q, want %q", name, got, want)
		}
	}
}

// 备份产物自身仍要排除（极端部署下 destFile 可能落在 srcDir 内：一边写一边读自己
// 会让包体膨胀、内容不可控）。
func TestTgzDirAsSkipsBackupArtifactItself(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(src, "self.tar.gz")

	if err := tgzDirAs(src, dest, ".dsh"); err != nil {
		t.Fatalf("tgzDirAs: %v", err)
	}
	entries := readTarEntries(t, dest)
	if _, ok := entries[".dsh/self.tar.gz"]; ok {
		t.Fatalf("备份产物自身不得被打进包里；实得 %v", keysOf(entries))
	}
	if _, ok := entries[".dsh/keep.txt"]; !ok {
		t.Fatalf("普通文件必须仍在备份里；实得 %v", keysOf(entries))
	}
}

func keysOf(m map[string]string) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}
