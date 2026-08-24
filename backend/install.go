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

// ensureNodePty 检测并安装 node-pty 预构建文件。
// 等待 dsh 自动创建 $HOME/.dsh/profiles/web 目录，然后执行 pnpm 安装和修补。
func ensureNodePty(renv *RuntimeEnv) error {
    logger().Printf("[node-pty] starting installation check")

    // 使用 HOME 作为基础目录
    home := renv.Home
    if home == "" {
        home = os.Getenv("HOME")
        if home == "" {
            home = "/root" // 降级默认值
        }
    }
    logger().Printf("[node-pty] using HOME=%s", home)

    // 目标补丁文件（patch-commit 成功后生成）
    patchFile := filepath.Join(home, ".dsh/profiles/web/patches/node-pty@1.1.0.patch")
    logger().Printf("[node-pty] checking patch file: %s", patchFile)
    if _, err := os.Stat(patchFile); err == nil {
        logger().Printf("[node-pty] patch file exists, skip installation")
        return nil
    } else if !os.IsNotExist(err) {
        logger().Printf("[node-pty] stat patch file error: %v", err)
        return fmt.Errorf("checking patch file: %w", err)
    }
    logger().Printf("[node-pty] patch file not found, proceeding with installation")

    // pnpm 可执行文件路径（固定位置）
    const appRoot = "/var/apps/Harness"
    pnpmPath := filepath.Join(appRoot, "var/pnpm/pnpm")
    logger().Printf("[node-pty] waiting for pnpm at %s", pnpmPath)

    // 等待 pnpm 生成，超时 5 分钟，每10秒打印一次状态
    timeout := time.After(5 * time.Minute)
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-timeout:
            return fmt.Errorf("timed out waiting for pnpm at %s", pnpmPath)
        case <-ticker.C:
            if _, err := os.Stat(pnpmPath); err == nil {
                logger().Printf("[node-pty] pnpm found at %s", pnpmPath)
                goto waitWebDir
            } else if !os.IsNotExist(err) {
                return fmt.Errorf("checking pnpm existence: %w", err)
            } else {
                logger().Printf("[node-pty] pnpm not yet present, waiting...")
            }
        }
    }

waitWebDir:
    webDir := filepath.Join(home, ".dsh/profiles/web")
    logger().Printf("[node-pty] waiting for web dir to be created by dsh: %s", webDir)

    // 等待 webDir 出现，超时 2 分钟（dsh 启动后应很快创建）
    webTimeout := time.After(2 * time.Minute)
    webTicker := time.NewTicker(3 * time.Second)
    defer webTicker.Stop()
    for {
        select {
        case <-webTimeout:
            return fmt.Errorf("timed out waiting for web dir %s", webDir)
        case <-webTicker.C:
            if _, err := os.Stat(webDir); err == nil {
                logger().Printf("[node-pty] web dir found at %s", webDir)
                goto install
            } else if !os.IsNotExist(err) {
                return fmt.Errorf("checking web dir existence: %w", err)
            } else {
                logger().Printf("[node-pty] web dir not yet present, waiting...")
            }
        }
    }

install:
    // 此时 webDir 已存在
    // 1. pnpm install node-pty --ignore-scripts
    logger().Printf("[node-pty] step 1: pnpm install node-pty --ignore-scripts (dir=%s)", webDir)
    cmd := exec.Command(pnpmPath, "install", "node-pty", "--ignore-scripts")
    cmd.Dir = webDir
    cmd.Env = append(os.Environ(), "PNPM_HOME="+renv.PnpmHome)
    out, err := cmd.CombinedOutput()
    if err != nil {
        logger().Printf("[node-pty] install failed: %v, output:\n%s", err, out)
        return fmt.Errorf("pnpm install failed: %w, output: %s", err, out)
    }
    logger().Printf("[node-pty] install output:\n%s", out)

    // 2. pnpm patch node-pty@1.1.0
    logger().Printf("[node-pty] step 2: pnpm patch node-pty@1.1.0")
    cmd = exec.Command(pnpmPath, "patch", "node-pty@1.1.0")
    cmd.Dir = webDir
    out, err = cmd.CombinedOutput()
    if err != nil {
        logger().Printf("[node-pty] patch failed: %v, output:\n%s", err, out)
        return fmt.Errorf("pnpm patch failed: %w, output: %s", err, out)
    }
    logger().Printf("[node-pty] patch output:\n%s", out)

    // 3. 拷贝预构建文件到补丁目录（保持目录结构）
    srcArm := filepath.Join(renv.TRIMAppDest, "server/node_modules/node-pty/prebuilds/linux-arm64")
    srcX64 := filepath.Join(renv.TRIMAppDest, "server/node_modules/node-pty/prebuilds/linux-x64")
    logger().Printf("[node-pty] srcArm: %s, srcX64: %s", srcArm, srcX64)

    patchRelDir := "node_modules/.pnpm_patches/node-pty@1.1.0"
    patchAbsDir := filepath.Join(webDir, patchRelDir)
    patchPrebuildsDir := filepath.Join(patchAbsDir, "prebuilds")
    logger().Printf("[node-pty] cleaning patch prebuilds dir: %s", patchPrebuildsDir)
    if err := os.RemoveAll(patchPrebuildsDir); err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("clean patch prebuilds dir: %w", err)
    }

    // 复制 arm64 目录（目标为 prebuilds/linux-arm64）
    dstArm := filepath.Join(patchPrebuildsDir, "linux-arm64")
    logger().Printf("[node-pty] copying arm64 prebuilds to %s", dstArm)
    if err := copyDir(srcArm, dstArm); err != nil {
        logger().Printf("[node-pty] copy arm64 failed: %v", err)
        return fmt.Errorf("copy arm64 prebuilds: %w", err)
    }
    // 复制 x64 目录（目标为 prebuilds/linux-x64）
    dstX64 := filepath.Join(patchPrebuildsDir, "linux-x64")
    logger().Printf("[node-pty] copying x64 prebuilds to %s", dstX64)
    if err := copyDir(srcX64, dstX64); err != nil {
        logger().Printf("[node-pty] copy x64 failed: %v", err)
        return fmt.Errorf("copy x64 prebuilds: %w", err)
    }

    // 验证复制结果
    if entries, err := os.ReadDir(patchPrebuildsDir); err == nil {
        logger().Printf("[node-pty] contents of %s:", patchPrebuildsDir)
        for _, e := range entries {
            logger().Printf("[node-pty]   - %s", e.Name())
        }
    } else {
        logger().Printf("[node-pty] failed to read patch prebuilds dir: %v", err)
    }

    // 4. pnpm patch-commit（使用相对路径）
    logger().Printf("[node-pty] step 4: pnpm patch-commit %s (relative)", patchRelDir)
    cmd = exec.Command(pnpmPath, "patch-commit", patchRelDir)
    cmd.Dir = webDir
    out, err = cmd.CombinedOutput()
    if err != nil {
        // 检查是否是因为 Ignored build scripts 导致的错误（常见且不影响补丁应用）
        if strings.Contains(string(out), "ERR_PNPM_IGNORED_BUILDS") {
            logger().Printf("[node-pty] patch-commit completed with ignored build scripts warning (likely fine), output:\n%s", out)
        } else {
            logger().Printf("[node-pty] patch-commit failed: %v, output:\n%s", err, out)
            return fmt.Errorf("pnpm patch-commit failed: %w, output: %s", err, out)
        }
    } else {
        logger().Printf("[node-pty] patch-commit output:\n%s", out)
    }

    // 最终验证：检查补丁文件是否生成
    if _, err := os.Stat(patchFile); err == nil {
        logger().Printf("[node-pty] installation and patching completed successfully, patch file exists")
    } else {
        logger().Printf("[node-pty] installation completed but patch file still missing: %v", err)
        return fmt.Errorf("post-install check: patch file %s not found", patchFile)
    }
    return nil
}

// copyDir 递归拷贝目录（保持权限）
func copyDir(src, dst string) error {
    srcInfo, err := os.Stat(src)
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
        if entry.IsDir() {
            if err := copyDir(srcPath, dstPath); err != nil {
                return err
            }
        } else {
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