package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ensureNodePty 把 node-pty 固定到 1.2.0-beta.15 并清理旧的 patch 产物。
//
// 与旧的「检测缺失→安装→pnpm patch→拷贝 prebuilds→patch-commit」修补逻辑不同，
// 新逻辑只做最小改动：
//
//  1. 检测 ~/.dsh/profiles/web/pnpm-workspace.yaml 是否存在 overrides 里的
//     `node-pty: 1.2.0-beta.15`；缺失则写入该字段。
//  2. 检测是否残留 patchedDependencies 里的 `node-pty@1.1.0`；存在则删除该内容。
//  3. 删除 ~/.dsh/profiles/web/patches 与
//     ~/.dsh/profiles/web/node_modules/.pnpm_patches/node-pty@1.1.0。
//  4. 只要上面发生任何改动，就标记“需要重启 dsh”。
//  5. 进入 ~/.dsh/profiles/web 执行 pnpm install，让 node-pty 1.2.0-beta.15 生效。
//
// 返回的 bool 表示是否需要重启 dsh（依据步骤 4 的标记），由调用方在
// pnpm install 完成后按该标记重启 dsh。
func ensureNodePty(renv *RuntimeEnv, home string) (restartNeeded bool, err error) {
	logger().Printf("[node-pty] starting setup check")

	// 优先读取 config.json 的 homeDir，这才是 dsh 服务实际使用的 HOME；
	// 其次回退到启动时默认主目录与环境变量。
	if home == "" {
		home = GetConfig().HomeDir
	}
	if home == "" {
		home = "/var/apps/Harness/shares/Harness"
	}
	logger().Printf("[node-pty] using HOME=%s", home)

	// 等待 pnpm 可用（冷启动时 harness 可能刚安装完 pnpm）。
	pnpmPath := pnpmBinPath()
	logger().Printf("[node-pty] waiting for pnpm at %s", pnpmPath)
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			return false, fmt.Errorf("timed out waiting for pnpm at %s", pnpmPath)
		case <-ticker.C:
			if _, err := os.Stat(pnpmPath); err == nil {
				logger().Printf("[node-pty] pnpm found")
				goto waitWebDir
			} else if !os.IsNotExist(err) {
				return false, fmt.Errorf("checking pnpm existence: %w", err)
			}
		}
	}

waitWebDir:
	webDir := filepath.Join(home, ".dsh", "profiles", "web")
	logger().Printf("[node-pty] waiting for web dir: %s", webDir)
	webTimeout := time.After(2 * time.Minute)
	webTicker := time.NewTicker(3 * time.Second)
	defer webTicker.Stop()
	for {
		select {
		case <-webTimeout:
			return false, fmt.Errorf("timed out waiting for web dir %s", webDir)
		case <-webTicker.C:
			if _, err := os.Stat(webDir); err == nil {
				logger().Printf("[node-pty] web dir found")
				goto setup
			} else if !os.IsNotExist(err) {
				return false, fmt.Errorf("checking web dir existence: %w", err)
			}
		}
	}

setup:
	// 1) 确保 overrides 固定 node-pty 版本；2) 清理 patchedDependencies。
	yamlChanged, hadPatched, err := ensureWorkspaceYAML(webDir)
	if err != nil {
		return false, fmt.Errorf("update pnpm-workspace.yaml: %w", err)
	}

	// 3) 删除旧的 patch 产物目录。
	hadPatchArtifacts := false
	patchesDir := filepath.Join(webDir, "patches")
	if _, err := os.Stat(patchesDir); err == nil {
		hadPatchArtifacts = true
		if rerr := os.RemoveAll(patchesDir); rerr != nil {
			return false, fmt.Errorf("remove %s: %w", patchesDir, rerr)
		}
		logger().Printf("[node-pty] removed %s", patchesDir)
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", patchesDir, err)
	}
	pnpmPatchesDir := filepath.Join(webDir, "node_modules", ".pnpm_patches", "node-pty@1.1.0")
	if _, err := os.Stat(pnpmPatchesDir); err == nil {
		hadPatchArtifacts = true
		if rerr := os.RemoveAll(pnpmPatchesDir); rerr != nil {
			return false, fmt.Errorf("remove %s: %w", pnpmPatchesDir, rerr)
		}
		logger().Printf("[node-pty] removed %s", pnpmPatchesDir)
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", pnpmPatchesDir, err)
	}

	// 4) 只要发生了任何改动就标记需要重启 dsh。
	restartNeeded = yamlChanged || hadPatchArtifacts

	// 5) 进入 web 目录执行 pnpm install，让 node-pty 1.2.0-beta.15 真正生效。
	logger().Printf("[node-pty] running pnpm install in %s", webDir)
	cmd := exec.Command(pnpmPath, "install", "--no-frozen-lockfile")
	cmd.Dir = webDir
	cmd.Env = append(os.Environ(), "PNPM_HOME="+renv.PnpmHome)
	// 若清理了残留的 patchedDependencies 条目，node_modules 目录会被 pnpm 移除
	// 重建；在无 TTY 环境下需显式放行，否则会触发
	// ERR_PNPM_ABORTED_REMOVE_MODULES_DIR_NO_TTY 而中止安装。已在上一步把顶层
	// confirmModulesPurge:false 写入 pnpm-workspace.yaml（主手段）；此处再以
	// 环境变量兜底（覆盖同名旧值，避免 POSIX getenv 取到首个旧条目而失效）。
	if hadPatched {
		cmd.Env = setEnv(cmd.Env, "npm_config_confirm_modules_purge", "false")
		cmd.Env = setEnv(cmd.Env, "CI", "true")
		logger().Printf("[node-pty] stale patch removed, allowing modules-dir purge for install")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("pnpm install failed: %w, output:\n%s", err, out)
	}
	logger().Printf("[node-pty] pnpm install succeeded, restartNeeded=%v", restartNeeded)
	return restartNeeded, nil
}

// pnpmBinPath 返回 pnpm 可执行文件路径（平台部署目录的固定位置）。
func pnpmBinPath() string {
	return filepath.Join("/var/apps/Harness", "var/pnpm/pnpm")
}

// setEnv 在 env 切片中把键 key 的值设为 value：先剔除已有的同名条目再追加，
// 避免环境变量列表中残留旧值（POSIX getenv 返回首个匹配项，旧值在前会遮蔽
// 新值，导致 pnpm 等子进程读不到新配置）。
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return append(out, key+"="+value)
}

// ensureWorkspaceYAML 确保 pnpm-workspace.yaml 里 overrides 固定 node-pty 版本、
// allowBuilds 放行 node-pty 构建，并删除 patchedDependencies 残留的
// `node-pty@1.1.0` 条目。返回是否发生了改动、以及是否删除了残留的 patched 条目。
// 文件不存在时创建最小配置。
func ensureWorkspaceYAML(webDir string) (changed bool, hadPatched bool, err error) {
	path := filepath.Join(webDir, "pnpm-workspace.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, false, err
		}
		// 文件不存在：创建固定版本 + 放行构建的最小配置。
		base := "overrides:\n  node-pty: 1.2.0-beta.15\nallowBuilds:\n  node-pty: true\n"
		if werr := os.WriteFile(path, []byte(base), 0o644); werr != nil {
			return false, false, werr
		}
		logger().Printf("[node-pty] %s missing, created with overrides + allowBuilds", path)
		return true, false, nil
	}

	lines := strings.Split(string(content), "\n")
	changed = false
	hadPatched = false

	// --- overrides：固定 node-pty 版本 ---
	var ovChanged bool
	lines, ovChanged = ensureSetting(lines, "overrides", "  node-pty: 1.2.0-beta.15")
	if ovChanged {
		changed = true
	}

	// --- allowBuilds：放行 node-pty 构建，防止 pnpm install 被阻止 ---
	var abChanged bool
	lines, abChanged = ensureSetting(lines, "allowBuilds", "  node-pty: true")
	if abChanged {
		changed = true
	}

	// --- patchedDependencies：清理残留的 node-pty@1.1.0 ---
	if pdStart, pdEnd, pdFound := findSection(lines, "patchedDependencies"); pdFound {
		newLines, removed := removePatchedEntry(lines, pdStart, pdEnd)
		if removed {
			changed = true
			hadPatched = true
			// 若该区块已无任何条目，连同 patchedDependencies 键一并删除。
			if pds, pde, ok := findSection(newLines, "patchedDependencies"); ok {
				empty := true
				for i := pds + 1; i < pde; i++ {
					t := strings.TrimSpace(newLines[i])
					if t != "" && !strings.HasPrefix(t, "#") {
						empty = false
						break
					}
				}
				if empty {
					newLines = append(append([]string{}, newLines[:pds]...), newLines[pds+1:]...)
				}
			}
			lines = newLines
			// 残留 patch 被清理会触发 node_modules 目录移除重装；pnpm 在无 TTY
			// 环境下会弹确认并中止（ERR_PNPM_ABORTED_REMOVE_MODULES_DIR_NO_TTY）。
			// 在 pnpm-workspace.yaml 顶层写入 confirmModulesPurge: false 显式放行
			// （settings 在 pnpm-workspace.yaml 是顶层键，不能包在 settings: 里）。
			var seChanged bool
			lines, seChanged = ensureTopLevelScalar(lines, "confirmModulesPurge", "false")
			if seChanged {
				changed = true
			}
		}
	}

	if !changed {
		return false, hadPatched, nil
	}
	if werr := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); werr != nil {
		return false, hadPatched, werr
	}
	logger().Printf("[node-pty] pnpm-workspace.yaml updated")
	return true, hadPatched, nil
}

// ensureSetting 确保顶层键 sectionKey 下的条目 entryLine 存在且值一致。
// 区块缺失则新建；键存在但值不同则改写；缺失则插入。返回新的行集合与是否改动。
// node-pty 相关配置（overrides 固定版本、allowBuilds 放行构建）复用本函数。
func ensureSetting(lines []string, sectionKey, entryLine string) ([]string, bool) {
	entryKey := strings.TrimSpace(strings.SplitN(entryLine, ":", 2)[0])
	entryVal := strings.TrimSpace(strings.SplitN(entryLine, ":", 2)[1])
	start, end, found := findSection(lines, sectionKey)
	if !found {
		if lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, sectionKey+":", entryLine, "")
		return lines, true
	}
	for i := start + 1; i < end; i++ {
		parts := strings.SplitN(lines[i], ":", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == entryKey {
			if strings.TrimSpace(parts[1]) == entryVal {
				return lines, false
			}
			lines[i] = "  " + entryLine
			return lines, true
		}
	}
	return insertSectionEntry(lines, start, entryLine)
}

// ensureTopLevelScalar 确保顶层存在形如 `key: value` 的标量键（如
// confirmModulesPurge: false）。缺失则追加到文件末尾；值不同则改写原行。
// 返回新的行集合与是否改动。
func ensureTopLevelScalar(lines []string, key, value string) ([]string, bool) {
	want := key + ": " + value
	for i := 0; i < len(lines); i++ {
		if k, ok := topLevelKey(lines[i]); ok && k == key {
			if strings.TrimSpace(lines[i]) == want {
				return lines, false
			}
			lines[i] = want
			return lines, true
		}
	}
	if lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	lines = append(lines, want, "")
	return lines, true
}

// insertSectionEntry 把 entryLine 插入到 start 所指顶层键行的下一行。
func insertSectionEntry(lines []string, start int, entryLine string) ([]string, bool) {
	insert := []string{entryLine}
	lines = append(lines, "")
	copy(lines[start+1+len(insert):], lines[start+1:])
	copy(lines[start+1:], insert)
	return lines, true
}

// topLevelKey 判断一行是否为顶层键（无缩进、形如 `key:` 的行），并返回键名。
func topLevelKey(ln string) (string, bool) {
	if strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "\t") {
		return "", false
	}
	if idx := strings.Index(ln, ":"); idx > 0 {
		return strings.TrimSpace(ln[:idx]), true
	}
	return "", false
}

// findSection 在行集合中定位顶层键 key 对应的区块，返回起始行号、结束行号
// （下一顶层键行号，或行集合长度）以及是否找到。
func findSection(lines []string, key string) (start, end int, found bool) {
	for i := 0; i < len(lines); i++ {
		if k, ok := topLevelKey(lines[i]); ok && k == key {
			start = i
			end = len(lines)
			for j := i + 1; j < len(lines); j++ {
				if _, ok := topLevelKey(lines[j]); ok {
					end = j
					break
				}
			}
			return start, end, true
		}
	}
	return 0, 0, false
}

// removePatchedEntry 删除 patchedDependencies 区块内的 `node-pty@1.1.0` 条目
// （连同其缩进的子行）。返回新的行集合与是否删除了条目。
func removePatchedEntry(lines []string, start, end int) ([]string, bool) {
	entryStart, entryEnd := -1, -1
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "node-pty@1.1.0:") {
			entryStart = i
			entryEnd = i + 1
			// 仅吞并比该条目本身缩进更深的子行，避免误删同级的其它条目。
			entryIndent := indentOf(lines[i])
			for j := i + 1; j < end; j++ {
				if indentOf(lines[j]) > entryIndent {
					entryEnd = j + 1
				} else {
					break
				}
			}
			break
		}
	}
	if entryStart < 0 {
		return lines, false
	}
	logger().Printf("[node-pty] removing patchedDependencies entry node-pty@1.1.0")
	newLines := append([]string{}, lines[:entryStart]...)
	newLines = append(newLines, lines[entryEnd:]...)
	return newLines, true
}

// indentOf 返回一行前导空白字符的数量（用于 YAML 缩进层级判断）。
func indentOf(ln string) int {
	n := 0
	for _, c := range ln {
		if c == ' ' || c == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// copyDir 递归拷贝目录（保持权限，保留符号链接本身）。既用于资源页把当前
// HOME 的 ~/.dsh 迁移到新的主目录，也用于更新时的资源拷贝。
func copyDir(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		fi, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			// 复制符号链接本身，避免迁移后链接被解析内容替换。
			link, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			os.Remove(dstPath)
			if err := os.Symlink(link, dstPath); err != nil {
				return err
			}
		case fi.IsDir():
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		default:
			srcFile, err := os.Open(srcPath)
			if err != nil {
				return err
			}
			dstFile, err := os.Create(dstPath)
			if err != nil {
				srcFile.Close()
				return err
			}
			_, err = io.Copy(dstFile, srcFile)
			srcFile.Close()
			dstFile.Close()
			if err != nil {
				return err
			}
			if info, err := os.Stat(srcPath); err == nil {
				os.Chmod(dstPath, info.Mode())
			}
		}
	}
	return nil
}