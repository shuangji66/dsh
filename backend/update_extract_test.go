package main

// update_extract_test.go —— 解压路径安全的回归测试（审查第 5 条）。
//
// 缺陷：越界检查只做**未解析软链的字符串前缀比较**，且完全不看软链条目的 Linkname，
// 于是「先建软链、再从软链路径写文件」能在 dest 之外落字节（zip-slip 变体）。
//
// 关键约束（本机实测的真实归档行为，决定了修法不能过激）：
//   - server 包（`tar czf` 自 npm install 产物）里的软链共 12 条，全部是包内相对链接
//     （node_modules/.bin/x -> ../pkg/cli.js），解析后都在 server 目录内；
//   - dsh 数据备份里的 ~/.dsh 共 546 条软链，其中 505 条是**指向 server 目录的绝对链接**
//     （profiles/node_modules/@deepseek-ai/* 那层镜像）。
// 因此：写内容前必须校验「父目录解析后仍在 dest 内」（挡死越界写），但软链条目本身
// 照原样创建（否则恢复出来的 profile 会少掉整层镜像）。

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type tarEntry struct {
	name     string
	body     string
	linkname string // 非空表示软链条目
	isDir    bool
}

func writeTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644}
		switch {
		case e.linkname != "":
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = e.linkname
		case e.isDir:
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
		default:
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// server 包的真实形态：包内相对软链 + 通过它访问的文件，必须照常解压。
func TestExtractKeepsInTreeSymlinks(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "server.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "node_modules/.bin/", isDir: true},
		{name: "node_modules/pkg/", isDir: true},
		{name: "node_modules/pkg/cli.js", body: "#!/usr/bin/env node\n"},
		{name: "node_modules/.bin/pkg", linkname: "../pkg/cli.js"},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("包内相对软链不应导致解压失败: %v", err)
	}
	link := filepath.Join(dest, "node_modules", ".bin", "pkg")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("包内相对软链应被创建: %v", err)
	}
	if got != "../pkg/cli.js" {
		t.Fatalf("软链目标 = %q, want ../pkg/cli.js", got)
	}
	if body, err := os.ReadFile(filepath.Join(dest, "node_modules/pkg/cli.js")); err != nil || string(body) == "" {
		t.Fatalf("通过软链可达的文件缺失: %v", err)
	}
}

// 恶意形态：先建指向 dest 之外的软链，再从软链路径写文件 —— 必须被拒绝，且外部目录
// 不得出现任何字节。
func TestExtractRejectsSymlinkEscapeOnWrite(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "evil.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "link", linkname: outside},
		{name: "link/evil.txt", body: "pwned"},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	err := extractTarGz(archive, dest)
	if err == nil {
		t.Fatal("通过软链向 dest 之外写文件必须被拒绝")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "evil.txt")); statErr == nil {
		t.Fatal("dest 之外出现了被写入的文件（zip-slip 逃逸）")
	}
}

// 相对形式（../ 逃逸）同样必须被拒绝。
func TestExtractRejectsRelativeSymlinkEscapeOnWrite(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "evil2.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "out/", isDir: true},
		{name: "out/link", linkname: "../../outside"},
		{name: "out/link/evil.txt", body: "pwned"},
	})
	dest := filepath.Join(dir, "outdest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := extractTarGz(archive, dest); err == nil {
		t.Fatal("相对路径逃逸必须被拒绝")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "evil.txt")); statErr == nil {
		t.Fatal("dest 之外出现了被写入的文件（相对软链逃逸）")
	}
}

// dsh 数据备份的真实形态：~/.dsh 里有指向 server 目录的**绝对链接**（本机实测 505 条），
// 解压必须照常成功并保留这些链接（否则恢复出来的 profile 会缺一层镜像）。
func TestExtractKeepsOutOfTreeMirrorLinks(t *testing.T) {
	dir := t.TempDir()
	serverDir := filepath.Join(dir, "server", "node_modules", "@deepseek-ai", "dsh")
	if err := os.MkdirAll(serverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "dsh-data.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: ".dsh/", isDir: true},
		{name: ".dsh/profiles/", isDir: true},
		{name: ".dsh/profiles/node_modules/", isDir: true},
		{name: ".dsh/profiles/node_modules/@deepseek-ai/", isDir: true},
		{name: ".dsh/profiles/node_modules/@deepseek-ai/dsh", linkname: serverDir},
		{name: ".dsh/profiles/web/", isDir: true},
		{name: ".dsh/profiles/web/package.json", body: `{"name":"web"}`},
	})
	dest := filepath.Join(dir, "home")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("指向 server 目录的镜像软链不应导致解压失败: %v", err)
	}
	link := filepath.Join(dest, ".dsh/profiles/node_modules/@deepseek-ai/dsh")
	if got, err := os.Readlink(link); err != nil || got != serverDir {
		t.Fatalf("镜像软链应原样保留（got=%q err=%v）", got, err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".dsh/profiles/web/package.json")); err != nil {
		t.Fatalf("同包的普通文件应正常解压: %v", err)
	}
}
