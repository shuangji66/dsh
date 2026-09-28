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
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

type tarEntry struct {
	name     string
	body     string
	linkname string // 非空表示软链条目
	isDir    bool
}

// writeTar 把条目写成一个（未压缩的）tar 流。
func writeTar(t *testing.T, w io.Writer, entries []tarEntry) {
	t.Helper()
	tw := tar.NewWriter(w)
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
}

func writeTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	writeTar(t, gz, entries)
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeTarXz 生成 .tar.xz（dsh 服务发布资产的格式）。
func writeTarXz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	xw, err := xz.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	writeTar(t, xw, entries)
	if err := xw.Close(); err != nil {
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

// --- .tar.xz（dsh 服务发布资产）---

// dsh 的 server 包是 .tar.xz：必须能解压，且内容/软链与 gz 通路一致。
func TestExtractTarXzServerPackage(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "server-x86-0.1.7.tar.xz")
	writeTarXz(t, archive, []tarEntry{
		{name: "server/", isDir: true},
		{name: "server/package.json", body: `{"name":"server"}`},
		{name: "server/node_modules/.bin/", isDir: true},
		{name: "server/node_modules/pkg/", isDir: true},
		{name: "server/node_modules/pkg/cli.js", body: "#!/usr/bin/env node\n"},
		{name: "server/node_modules/.bin/pkg", linkname: "../pkg/cli.js"},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("解压 .tar.xz 失败: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(dest, "server/package.json")); err != nil || string(body) == "" {
		t.Fatalf("包内文件缺失: %v", err)
	}
	if got, err := os.Readlink(filepath.Join(dest, "server/node_modules/.bin/pkg")); err != nil || got != "../pkg/cli.js" {
		t.Fatalf("包内相对软链应保留（got=%q err=%v）", got, err)
	}
}

// xz 通路必须与 gz 共用同一套越界防护（extractTar）：软链逃逸在 xz 包里同样被拒绝。
func TestExtractTarXzRejectsSymlinkEscapeOnWrite(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "evil.tar.xz")
	writeTarXz(t, archive, []tarEntry{
		{name: "link", linkname: outside},
		{name: "link/evil.txt", body: "pwned"},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := extractArchive(archive, dest); err == nil {
		t.Fatal("xz 包里通过软链向 dest 之外写文件必须被拒绝")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "evil.txt")); statErr == nil {
		t.Fatal("dest 之外出现了被写入的文件（zip-slip 逃逸）")
	}
}

// 扩展名决定解压器：.tar.gz / .tgz 走 gzip，.tar.xz / .txz 走 xz。
func TestExtractArchiveDispatchesBySuffix(t *testing.T) {
	dir := t.TempDir()
	entries := []tarEntry{{name: "f.txt", body: "hello"}}

	cases := []struct {
		name    string
		write   func(t *testing.T, path string, entries []tarEntry)
		wantErr bool
	}{
		{name: "a.tar.gz", write: writeTarGz},
		{name: "b.tgz", write: writeTarGz},
		{name: "c.tar.xz", write: writeTarXz},
		{name: "d.txz", write: writeTarXz},
		// 无扩展名的（旧控制台留下的 dsh 半成品没有别的可能）按 gzip 处理：
		// 内容确实不是 gz，因此必须报错而不是静默产出空目录。
		{name: "e.bin", write: writeTarXz, wantErr: true},
	}
	for _, tc := range cases {
		archive := filepath.Join(dir, tc.name)
		tc.write(t, archive, entries)
		dest := filepath.Join(dir, "out-"+tc.name)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		err := extractArchive(archive, dest)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: 期望解压失败（按 gzip 处理 xz 字节）", tc.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: 解压失败: %v", tc.name, err)
			continue
		}
		if body, rerr := os.ReadFile(filepath.Join(dest, "f.txt")); rerr != nil || string(body) != "hello" {
			t.Errorf("%s: 内容不符（body=%q err=%v）", tc.name, body, rerr)
		}
	}
}

// --- .tar.xz 的解码通路：外部 xz 优先、纯 Go 兜底 ---

// writeTarPlain 生成一个未压缩的 tar（假 xz 直接 cat 它即可冒充「解码结果」）。
func writeTarPlain(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writeTar(t, f, entries)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// useFakeXz 注入「xz 命令解析」的结果（真实实现见 xzPathFn）。
func useFakeXz(t *testing.T, fn func() (string, error)) {
	t.Helper()
	prev := xzPathFn
	xzPathFn = fn
	t.Cleanup(func() { xzPathFn = prev })
}

// writeFakeXz 造一个假 xz：执行时先把参数追加进 mark 文件（用于确认真的走了外部通路），
// 再执行 body。
func writeFakeXz(t *testing.T, dir, mark, body string) string {
	t.Helper()
	p := filepath.Join(dir, "fake-xz.sh")
	script := "#!/bin/sh\necho \"$@\" >> " + mark + "\n" + body + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// 外部 xz 可用时优先走它（liblzma 比纯 Go 快一个数量级），且按 `-dc <包>` 调用。
func TestExtractTarXzPrefersExternalXz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "server-x86-0.1.7.tar.xz")
	// 内容无关紧要：假 xz 不读它（真实实现由 xz 自己解）。
	if err := os.WriteFile(archive, []byte("not really xz"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(dir, "payload.tar")
	writeTarPlain(t, payload, []tarEntry{
		{name: "server/", isDir: true},
		{name: "server/from-external.txt", body: "external"},
	})
	mark := filepath.Join(dir, "mark")
	fake := writeFakeXz(t, dir, mark, "cat "+payload)
	useFakeXz(t, func() (string, error) { return fake, nil })

	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("外部 xz 通路应解压成功: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dest, "server/from-external.txt"))
	if err != nil || string(body) != "external" {
		t.Fatalf("解压内容应来自外部命令的输出（body=%q err=%v）", body, err)
	}
	got, err := os.ReadFile(mark)
	if err != nil {
		t.Fatalf("假 xz 未被调用: %v", err)
	}
	if !strings.Contains(string(got), "-dc "+archive) {
		t.Fatalf("外部命令参数 = %q, want 含 `-dc %s`", strings.TrimSpace(string(got)), archive)
	}
}

// 外部 xz 异常退出（吐了内容但退出码非 0）：必须回退纯 Go 重解，且**先清空**外部通路
// 写下的那份，不能把两次结果混在一起。
func TestExtractTarXzFallsBackAndWipesExternalOutput(t *testing.T) {
	dir := t.TempDir()
	// 真包（纯 Go 编码）：回退通路会读它。
	archive := filepath.Join(dir, "server-x86-0.1.7.tar.xz")
	writeTarXz(t, archive, []tarEntry{
		{name: "server/", isDir: true},
		{name: "server/real.txt", body: "real"},
	})
	// 假 xz：先吐一份「看起来解开了」的 tar（含诱饵文件），再以非零码退出。
	decoy := filepath.Join(dir, "decoy.tar")
	writeTarPlain(t, decoy, []tarEntry{
		{name: "server/", isDir: true},
		{name: "server/decoy.txt", body: "decoy"},
	})
	mark := filepath.Join(dir, "mark")
	fake := writeFakeXz(t, dir, mark, "cat "+decoy+"\nexit 1")
	useFakeXz(t, func() (string, error) { return fake, nil })

	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("外部通路失败后应回退纯 Go 并成功: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "server/decoy.txt")); err == nil {
		t.Fatal("回退前必须清空外部通路写下的内容（否则会留下半份/坏文件）")
	}
	body, err := os.ReadFile(filepath.Join(dest, "server/real.txt"))
	if err != nil || string(body) != "real" {
		t.Fatalf("回退后应由纯 Go 通路解出真内容（body=%q err=%v）", body, err)
	}
}

// 设备上没有 xz（LookPath 失败）：直接走纯 Go，不报错。
func TestExtractTarXzFallsBackWhenXzMissing(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "server-x86-0.1.7.tar.xz")
	writeTarXz(t, archive, []tarEntry{
		{name: "server/", isDir: true},
		{name: "server/only-builtin.txt", body: "builtin"},
	})
	useFakeXz(t, func() (string, error) { return "", errors.New("exec: \"xz\": executable file not found in $PATH") })

	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("没有 xz 命令时应回退纯 Go 并成功: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dest, "server/only-builtin.txt"))
	if err != nil || string(body) != "builtin" {
		t.Fatalf("纯 Go 通路内容不符（body=%q err=%v）", body, err)
	}
}

// 外部通路本身没问题、是包坏了（乱字节）：报错，且不得留下半份目录内容。
func TestExtractTarXzReportsBrokenPackage(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "server-broken.tar.xz")
	if err := os.WriteFile(archive, []byte("这不是一个 xz 流"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive, dest); err == nil {
		t.Fatal("坏包必须报错")
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("坏包不应留下任何内容，实际 %d 项", len(entries))
	}
}
