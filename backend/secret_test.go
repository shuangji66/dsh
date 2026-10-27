package main

// secret_test.go —— 主密钥管理与密文信封（secret.go）。
//
// 两条贯穿的判据：
//  1. 主密钥**不能悄悄换**：同一目录第二次加载必须拿到同一把；文件长度不对必须直接失败
//     （悄悄重新生成等于把已存的密文全部变成废数据）；
//  2. 外部注入的密钥解不出来时**绝不回退到本地文件**（那种静默回退的表现是「换了密钥后
//     旧内容全部解密失败，而进程一切正常」）。

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testCryptorAt 用指定的主密钥文件路径构造 cryptor（路径相同 = 同一把密钥，用来模拟重启）。
func testCryptorAt(t *testing.T, keyPath string) *cryptor {
	t.Helper()
	c, err := loadCryptor(keyPath, "")
	if err != nil {
		t.Fatalf("loadCryptor(%s): %v", keyPath, err)
	}
	return c
}

// testCryptor 造一把「用完就丢」的临时主密钥。
func testCryptor(t *testing.T) *cryptor {
	t.Helper()
	return testCryptorAt(t, filepath.Join(t.TempDir(), masterKeyFileName))
}

// readFileStr 读一个文件（测试断言失败时打印现场用）。
func readFileStr(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	return string(raw)
}

// 主密钥的解析：标准/URL/裸 base64 与 hex 都认，长度不对或非法编码一律拒绝。
func TestDecodeMasterKey(t *testing.T) {
	key := make([]byte, masterKeyLen)
	for i := range key {
		key[i] = byte(i * 7)
	}
	for _, encoded := range []string{
		base64.StdEncoding.EncodeToString(key),
		base64.RawStdEncoding.EncodeToString(key),
		base64.URLEncoding.EncodeToString(key),
		hex.EncodeToString(key),
	} {
		got, err := decodeMasterKey(encoded)
		if err != nil || !bytes.Equal(got, key) {
			t.Errorf("解析 %q 失败：%v", encoded, err)
		}
	}
	for _, bad := range []string{"", "abc", base64.StdEncoding.EncodeToString(key[:8]), strings.Repeat("z", 64)} {
		if _, err := decodeMasterKey(bad); err == nil {
			t.Errorf("%q 不该被接受", bad)
		}
	}
}

// master.key 的生成、权限与稳定性。
func TestMasterKeyFileLifecycle(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, masterKeyFileName)
	c1, err := loadCryptor(keyPath, "")
	if err != nil {
		t.Fatalf("首次生成 master.key 失败：%v", err)
	}
	fi, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("master.key 不存在：%v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("权限 = %o，期望 600", fi.Mode().Perm())
	}
	key, _ := os.ReadFile(keyPath)
	if len(key) != masterKeyLen {
		t.Fatalf("密钥长度 = %d，期望 %d", len(key), masterKeyLen)
	}

	// 同一目录第二次加载必须拿到同一把密钥（否则已存的内容全部解不开）
	c2, err := loadCryptor(keyPath, "")
	if err != nil {
		t.Fatalf("再次加载失败：%v", err)
	}
	env, err := c1.encrypt("quickcmd:x", quickCmdScopeGlobal, "hello")
	if err != nil {
		t.Fatalf("加密失败：%v", err)
	}
	if plain, err := c2.decrypt("quickcmd:x", quickCmdScopeGlobal, env); err != nil || plain != "hello" {
		t.Fatalf("同一密钥应当能解密：%q %v", plain, err)
	}

	// 长度不对的密钥文件必须让加载失败（不能悄悄重新生成）
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("写坏文件失败：%v", err)
	}
	if _, err := loadCryptor(keyPath, ""); err == nil {
		t.Fatal("损坏的 master.key 应当被拒绝")
	}
}

// 外部注入的密钥优先，且**解不出来时不许回退到本地文件**。
func TestLoadCryptorEnvKeyDoesNotFallBack(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, masterKeyFileName)
	// 先造一份可用的本地密钥
	if _, err := loadCryptor(keyPath, ""); err != nil {
		t.Fatalf("准备本地密钥失败：%v", err)
	}

	if _, err := loadCryptor(keyPath, "not-a-key"); err == nil {
		t.Fatal("非法 HARNESS_MASTER_KEY 应当直接失败，而不是回退到 master.key")
	}

	// 合法的外部密钥：与本地文件那把不同，用它写的密文本地那把解不开
	external := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, masterKeyLen))
	cExt, err := loadCryptor(keyPath, external)
	if err != nil {
		t.Fatalf("外部密钥应当可用：%v", err)
	}
	env, err := cExt.encrypt("quickcmd:x", quickCmdScopeGlobal, "hello")
	if err != nil {
		t.Fatalf("加密失败：%v", err)
	}
	cLocal, err := loadCryptor(keyPath, "")
	if err != nil {
		t.Fatalf("加载本地密钥失败：%v", err)
	}
	if _, err := cLocal.decrypt("quickcmd:x", quickCmdScopeGlobal, env); err == nil {
		t.Fatal("本地密钥不该能解开外部密钥写的密文")
	}
}

// AAD 绑定（field + scope）：换字段/换作用域/换字节都必须解不开；空明文与空密文的语义。
func TestCryptorAADAndEmptyValues(t *testing.T) {
	c := testCryptor(t)

	// 空明文 = 没有这个值（不写密文）
	if env, err := c.encrypt("quickcmd:x", quickCmdScopeGlobal, ""); err != nil || env != nil {
		t.Fatalf("空明文不该产生密文：%v %v", env, err)
	}
	// nil / 空密文解出空串
	if plain, err := c.decrypt("quickcmd:x", quickCmdScopeGlobal, nil); err != nil || plain != "" {
		t.Fatalf("nil 密文应当解出空串：%q %v", plain, err)
	}
	if plain, err := c.decrypt("quickcmd:x", quickCmdScopeGlobal, &secretEnvelope{V: 1, CT: "  "}); err != nil || plain != "" {
		t.Fatalf("空 CT 应当解出空串：%q %v", plain, err)
	}

	env, err := c.encrypt("quickcmd:x", quickCmdScopeGlobal, "secret")
	if err != nil {
		t.Fatalf("加密失败：%v", err)
	}
	if _, err := c.decrypt("quickcmd:y", quickCmdScopeGlobal, env); err == nil {
		t.Error("换一个指令 id（AAD field）应当解不开")
	}
	if _, err := c.decrypt("quickcmd:x", "other-scope", env); err == nil {
		t.Error("换一个作用域（AAD scope）应当解不开")
	}
	if _, err := c.decrypt("quickcmd:x", quickCmdScopeGlobal, &secretEnvelope{V: 99, CT: env.CT}); err == nil {
		t.Error("不支持的信封版本应当报错")
	}
	if _, err := c.decrypt("quickcmd:x", quickCmdScopeGlobal, &secretEnvelope{V: 1, CT: "!!!"}); err == nil {
		t.Error("非法 base64 应当报错")
	}
	if _, err := c.decrypt("quickcmd:x", quickCmdScopeGlobal, &secretEnvelope{V: 1, CT: env.CT[:4]}); err == nil {
		t.Error("被截断的密文应当报错")
	}

	// nil crypt（主密钥不可用）：加密与解密都报错，绝不静默放过
	var nilCrypt *cryptor
	if _, err := nilCrypt.encrypt("quickcmd:x", quickCmdScopeGlobal, "x"); err == nil {
		t.Error("主密钥不可用时加密必须报错")
	}
	if _, err := nilCrypt.decrypt("quickcmd:x", quickCmdScopeGlobal, env); err == nil {
		t.Error("主密钥不可用时解密必须报错")
	}

	// 同一明文两次加密的密文不同（每次都是新 nonce），但都能解回原文
	again, err := c.encrypt("quickcmd:x", quickCmdScopeGlobal, "secret")
	if err != nil {
		t.Fatalf("加密失败：%v", err)
	}
	if again.CT == env.CT {
		t.Error("两次加密用了同一个 nonce（密文相同）")
	}
	if plain, err := c.decrypt("quickcmd:x", quickCmdScopeGlobal, again); err != nil || plain != "secret" {
		t.Fatalf("解回的内容不对：%q %v", plain, err)
	}
}
