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
// home 为当前生效的主目录路径（dsh 切换主目录后会变化）。
func ensureNodePty(renv *RuntimeEnv, home string) error {
    logger().Printf("[node-pty] starting installation check")

    if home == "" {
        home = renv.Home
    }
    if home == "" {
        home = os.Getenv("HOME")
        if home == "" {
            home = "/root"
        }
    }
    logger().Printf("[node-pty] using HOME=%s", home)

    patchFile := filepath.Join(home, ".dsh/profiles/web/patches/node-pty@1.1.0.patch")
    logger().Printf("[node-pty] checking patch file: %s", patchFile)
    if _, err := os.Stat(patchFile); err == nil {
        logger().Printf("[node-pty] patch file exists, skip installation")
        return nil
    } else if !os.IsNotExist(err) {
        return fmt.Errorf("checking patch file: %w", err)
    }
    logger().Printf("[node-pty] patch file not found, proceeding with installation")

    const appRoot = "/var/apps/Harness"
    pnpmPath := filepath.Join(appRoot, "var/pnpm/pnpm")
    logger().Printf("[node-pty] waiting for pnpm at %s", pnpmPath)

    timeout := time.After(5 * time.Minute)
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-timeout:
            return fmt.Errorf("timed out waiting for pnpm at %s", pnpmPath)
        case <-ticker.C:
            if _, err := os.Stat(pnpmPath); err == nil {
                logger().Printf("[node-pty] pnpm found")
                goto waitWebDir
            } else if !os.IsNotExist(err) {
                return fmt.Errorf("checking pnpm existence: %w", err)
            }
        }
    }

waitWebDir:
    webDir := filepath.Join(home, ".dsh/profiles/web")
    logger().Printf("[node-pty] waiting for web dir: %s", webDir)

    webTimeout := time.After(2 * time.Minute)
    webTicker := time.NewTicker(3 * time.Second)
    defer webTicker.Stop()
    for {
        select {
        case <-webTimeout:
            return fmt.Errorf("timed out waiting for web dir %s", webDir)
        case <-webTicker.C:
            if _, err := os.Stat(webDir); err == nil {
                logger().Printf("[node-pty] web dir found")
                goto install
            } else if !os.IsNotExist(err) {
                return fmt.Errorf("checking web dir existence: %w", err)
            }
        }
    }

install:
    logger().Printf("[node-pty] step 1: pnpm install node-pty --ignore-scripts")
    cmd := exec.Command(pnpmPath, "install", "node-pty", "--ignore-scripts")
    cmd.Dir = webDir
    cmd.Env = append(os.Environ(), "PNPM_HOME="+renv.PnpmHome)
    if out, err := cmd.CombinedOutput(); err != nil {
        logger().Printf("[node-pty] install failed: %v, output:\n%s", err, out)
        return fmt.Errorf("pnpm install failed: %w", err)
    }
    logger().Printf("[node-pty] install succeeded")

    logger().Printf("[node-pty] step 2: pnpm patch node-pty@1.1.0")
    cmd = exec.Command(pnpmPath, "patch", "node-pty@1.1.0")
    cmd.Dir = webDir
    if out, err := cmd.CombinedOutput(); err != nil {
        logger().Printf("[node-pty] patch failed: %v, output:\n%s", err, out)
        return fmt.Errorf("pnpm patch failed: %w", err)
    }
    logger().Printf("[node-pty] patch succeeded")

    srcArm := filepath.Join(renv.TRIMAppDest, "server/node_modules/node-pty/prebuilds/linux-arm64")
    srcX64 := filepath.Join(renv.TRIMAppDest, "server/node_modules/node-pty/prebuilds/linux-x64")
    logger().Printf("[node-pty] copying prebuilds from %s and %s", srcArm, srcX64)

    patchRelDir := "node_modules/.pnpm_patches/node-pty@1.1.0"
    patchAbsDir := filepath.Join(webDir, patchRelDir)
    patchPrebuildsDir := filepath.Join(patchAbsDir, "prebuilds")
    if err := os.RemoveAll(patchPrebuildsDir); err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("clean patch prebuilds dir: %w", err)
    }

    dstArm := filepath.Join(patchPrebuildsDir, "linux-arm64")
    if err := copyDir(srcArm, dstArm); err != nil {
        return fmt.Errorf("copy arm64 prebuilds: %w", err)
    }
    dstX64 := filepath.Join(patchPrebuildsDir, "linux-x64")
    if err := copyDir(srcX64, dstX64); err != nil {
        return fmt.Errorf("copy x64 prebuilds: %w", err)
    }
    logger().Printf("[node-pty] prebuilds copied")

    logger().Printf("[node-pty] step 4: pnpm patch-commit %s", patchRelDir)
    cmd = exec.Command(pnpmPath, "patch-commit", patchRelDir)
    cmd.Dir = webDir
    out, err := cmd.CombinedOutput()
    if err != nil {
        if strings.Contains(string(out), "ERR_PNPM_IGNORED_BUILDS") {
            logger().Printf("[node-pty] patch-commit succeeded with ignored build scripts warning")
        } else {
            logger().Printf("[node-pty] patch-commit failed: %v, output:\n%s", err, out)
            return fmt.Errorf("pnpm patch-commit failed: %w", err)
        }
    } else {
        logger().Printf("[node-pty] patch-commit succeeded")
    }

    if _, err := os.Stat(patchFile); err == nil {
        logger().Printf("[node-pty] installation and patching completed successfully")
    } else {
        return fmt.Errorf("post-install check: patch file %s not found", patchFile)
    }
    return nil
}

// copyDir 递归拷贝目录（保持权限，保留符号链接本身）。既用于 node-pty 预构建
// 拷贝，也用于资源页把当前 HOME 的 ~/.dsh 迁移到新的主目录。
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