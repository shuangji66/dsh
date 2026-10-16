package main

// accountvault_crypt_test.go —— 控制台账号库（dsh-accounts.json）的落盘加密。
//
// 判据与 quickcmds_crypt_test.go 同一套，只是对象换成「整份账号列表」：
//  1. 磁盘上不许出现任何 token（或联系方式）的明文；
//  2. 同一把主密钥（换一把 cryptor = 模拟重启）读回，条目逐字段相同；
//  3. **解不开必须报错**，绝不静默返回空库 —— 调用方（状态查询的自愈导入、登录后的 upsert）
//     拿到空库就会把原始密文覆盖掉；
//  4. 主密钥不可用时**一个字节都不写**（不退化成写明文）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// accountVaultDiskPath 是当前数据目录下的账号库路径（测试都先 Setenv HARNESS_DATA_DIR）。
func accountVaultDiskPath() string { return dataPath(accountVaultFile) }

// 密文落盘 + 0600 + 同密钥读回逐字段一致（换一把同路径的 cryptor = 重启）。
func TestAccountVaultEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dir)
	keyPath := filepath.Join(dir, masterKeyFileName)
	crypt := testCryptorAt(t, keyPath)

	const token = "tok-account-secret-9f3a"
	const contact = "13900000059"
	v := &accountVault{}
	v.upsert(accountVaultEntry{
		ID: "u-1", Contact: contact, Name: "某人", AvatarURL: "https://x/a.png",
		Token: token, Issuer: accountPlatformOrigin, AddedAt: "2026-01-01T00:00:00Z",
	})
	if err := saveAccountVault(accountVaultDiskPath(), crypt, v); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := accountVaultDiskPath()
	raw := readFileStr(t, path)
	for _, leak := range []string{token, contact, "某人"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("磁盘上不该出现 %q：\n%s", leak, raw)
		}
	}
	var stored accountVaultStored
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("磁盘形态不是 JSON：%v\n%s", err, raw)
	}
	if stored.Version != accountVaultDocVersion || stored.Vault == nil {
		t.Fatalf("磁盘形态不对（应为 version %d 的密文信封）：%+v\n%s", accountVaultDocVersion, stored, raw)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("账号库权限应为 0600: %v (%v)", fi.Mode().Perm(), err)
	}

	back, err := loadAccountVault(accountVaultDiskPath(), testCryptorAt(t, keyPath))
	if err != nil {
		t.Fatalf("同密钥读回失败: %v", err)
	}
	e := back.find("u-1")
	if e == nil || e.Token != token || e.Contact != contact || e.Name != "某人" ||
		e.AvatarURL != "https://x/a.png" || e.Issuer != accountPlatformOrigin ||
		e.AddedAt != "2026-01-01T00:00:00Z" || back.Version != accountVaultVersion {
		t.Fatalf("读回的条目不一致: %+v (版本 %d)", e, back.Version)
	}
}

// 解不开的三种情形都必须报错（且绝不是「空库」）：换密钥、跨字段剪贴、文件截断。
func TestAccountVaultDecryptFailuresAreErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dir)
	keyPath := filepath.Join(dir, masterKeyFileName)
	crypt := testCryptorAt(t, keyPath)

	v := &accountVault{}
	v.upsert(accountVaultEntry{ID: "u-1", Token: "tok-1"})
	if err := saveAccountVault(accountVaultDiskPath(), crypt, v); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := accountVaultDiskPath()
	original := readFileStr(t, path)

	// ① 换了一把主密钥（master.key 被删/换过）：报错，不能当成「库里没账号」。
	if got, err := loadAccountVault(accountVaultDiskPath(), testCryptor(t)); err == nil {
		t.Fatalf("换密钥后应报错，实际读回 %+v", got)
	}
	// ② 剪贴：把**快捷指令**那条密文塞进 vault 字段（AAD 不同）→ 解不开。
	env, err := crypt.encrypt(quickCmdAADField("db-01"), quickCmdScopeGlobal, "df -h")
	if err != nil || env == nil {
		t.Fatalf("造一条快捷指令密文失败: %v", err)
	}
	body, err := json.Marshal(accountVaultStored{Version: accountVaultDocVersion, Vault: env})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := loadAccountVault(accountVaultDiskPath(), crypt); err == nil {
		t.Fatalf("AAD 不同的密文应解不开，实际读回 %+v", got)
	}
	// ③ 截断（只剩版本号）：报错而不是空库。
	if err := os.WriteFile(path, []byte("{\"version\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := loadAccountVault(accountVaultDiskPath(), crypt); err == nil {
		t.Fatalf("没有信封的文档应报错，实际读回 %+v", got)
	}
	// 读失败之后也不该有人把原文件改掉（这条路径上唯一的写是自愈导入，见下一个测试）。
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 主密钥不可用（crypt == nil，见 main.go）：读写都拒绝，且**一个字节都不写**。
func TestAccountVaultRefusesWithoutMasterKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dir)
	path := accountVaultDiskPath()

	v := &accountVault{}
	v.upsert(accountVaultEntry{ID: "u-1", Token: "tok-1"})
	if err := saveAccountVault(accountVaultDiskPath(), nil, v); err == nil {
		t.Fatal("主密钥不可用时保存应报错")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("主密钥不可用时不该创建文件: %v", err)
	}

	// 已有密文：读要报错，写要拒绝且原文一个字节不变。
	crypt := testCryptor(t)
	if err := saveAccountVault(accountVaultDiskPath(), crypt, v); err != nil {
		t.Fatalf("save: %v", err)
	}
	before := readFileStr(t, path)
	if _, err := loadAccountVault(accountVaultDiskPath(), nil); err == nil {
		t.Fatal("主密钥不可用时读取应报错")
	}
	if err := saveAccountVault(accountVaultDiskPath(), nil, &accountVault{}); err == nil {
		t.Fatal("主密钥不可用时保存应报错")
	}
	if after := readFileStr(t, path); after != before {
		t.Fatalf("主密钥不可用时账号库被改写了：\n%s", after)
	}
}

// 旧版明文库（version 1）：报错提示删掉重登，并且**不被任何路径覆盖**。
//
// 这一版没对外发布，所以不写迁移：用户直接删文件重登即可。但「不迁移」不等于「可以当空库
// 读」—— 状态查询里的自愈导入会顺手写一次盘，那一下就会把用户还没重新登录的旧库抹掉。
func TestAccountVaultLegacyPlaintextRejectedAndNotOverwritten(t *testing.T) {
	home := writeAccountCreds(t, accountCredsFixture)
	t.Setenv("HARNESS_DATA_DIR", t.TempDir())
	legacy := "{\"version\": 1, \"accounts\": [{\"id\": \"u1\", \"token\": \"legacy-token\"}]}\n"
	if err := os.WriteFile(accountVaultDiskPath(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadAccountVault(accountVaultDiskPath(), testCryptor(t)); err == nil || !strings.Contains(err.Error(), "legacy plaintext") {
		t.Fatalf("旧明文库应报错并点明原因，实际 %v", err)
	}

	// 状态查询（dsh 说已登录 → 会走到自愈导入）不得改写这份读不出来的库。
	m := newAccountTestMux(t, home)
	prevCfg := GetConfig()
	initConfig(&AppConfig{ProxyPort: 3079})
	t.Cleanup(func() { initConfig(&prevCfg) })
	prevState := accountDshStateFn
	accountDshStateFn = func(*AdminMux) (string, error) { return "credential-stored", nil }
	t.Cleanup(func() { accountDshStateFn = prevState })
	if err := m.accountStoreGrant("tok-live"); err != nil {
		t.Fatal(err)
	}
	st := m.accountStatusSnapshot()
	if len(st.Accounts) != 0 {
		t.Fatalf("读不出来的库不该给出账号列表: %+v", st.Accounts)
	}
	if got := readFileStr(t, accountVaultDiskPath()); got != legacy {
		t.Fatalf("自愈导入覆盖了读不出来的库：\n%s", got)
	}
}

// 并发变更不许互相覆盖：没有锁时「load 一份快照 → 改 → save 整份写回」会丢掉别人的账号
// （最后一个写的人赢，其余全消失）。accountVaultMutate 在锁内重读再写，因此 N 个并发
// upsert 之后 N 条都必须在盘上 —— 把 accountVaultMu 去掉这条会红。
func TestAccountVaultMutateNoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dir)
	crypt := testCryptor(t)
	path := accountVaultDiskPath()

	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- accountVaultMutate(path, crypt, func(v *accountVault) error {
				v.upsert(accountVaultEntry{
					ID:    fmt.Sprintf("acc-%02d", i),
					Token: fmt.Sprintf("tok-%02d", i),
				})
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发变更报错: %v", err)
		}
	}

	back, err := loadAccountVault(path, crypt)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(back.Accounts) != n {
		t.Fatalf("并发 upsert 后库里有 %d 条，want %d（说明有更新被整份覆盖）", len(back.Accounts), n)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("acc-%02d", i)
		e := back.find(id)
		if e == nil || e.Token != fmt.Sprintf("tok-%02d", i) {
			t.Fatalf("条目 %s 丢了或串了: %+v", id, e)
		}
	}
}

// accountVaultMu 的纪律：fn 返回错误时**一个字节都不写**（调用方据此报错，而不是留下半改的库）。
func TestAccountVaultMutateAbortsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dir)
	crypt := testCryptor(t)
	path := accountVaultDiskPath()
	if err := accountVaultMutate(path, crypt, func(v *accountVault) error {
		v.upsert(accountVaultEntry{ID: "keep", Token: "tok-keep"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := readFileStr(t, path)
	wantErr := errors.New("stop")
	if err := accountVaultMutate(path, crypt, func(v *accountVault) error {
		v.upsert(accountVaultEntry{ID: "extra", Token: "tok-extra"})
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("fn 报错时应原样返回: %v", err)
	}
	if after := readFileStr(t, path); after != before {
		t.Fatalf("fn 报错时不该写盘：\n%s", after)
	}
}

// 账号库路径跟随 RuntimeEnv（与 quickCmdsPath / masterKeyPath 同一口径）：renv.DataDir
// 优先于环境变量，拿不到 renv 时才退回统一数据目录。
func TestAccountVaultPathFollowsRuntimeEnv(t *testing.T) {
	dataDir := t.TempDir()
	envDir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", envDir)

	renv := &RuntimeEnv{DataDir: dataDir}
	if got, want := accountVaultPath(renv), filepath.Join(dataDir, accountVaultFile); got != want {
		t.Fatalf("accountVaultPath(renv) = %q, want %q", got, want)
	}
	// 拿不到 renv（或 DataDir 为空）时退回环境变量指的数据目录。
	if got, want := accountVaultPath(&RuntimeEnv{}), filepath.Join(envDir, accountVaultFile); got != want {
		t.Fatalf("RuntimeEnv 为空时应退回 dataPath: got %q want %q", got, want)
	}
	if got, want := accountVaultPath(nil), filepath.Join(envDir, accountVaultFile); got != want {
		t.Fatalf("renv 为 nil 时应退回 dataPath: got %q want %q", got, want)
	}

	// 端到端：库真的落在 renv.DataDir 下，而不是环境变量指的那个目录。
	crypt := testCryptor(t)
	if err := accountVaultMutate(accountVaultPath(renv), crypt, func(v *accountVault) error {
		v.upsert(accountVaultEntry{ID: "u-1", Token: "tok-1"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, accountVaultFile)); err != nil {
		t.Fatalf("库没落在 RuntimeEnv.DataDir 下: %v", err)
	}
	if _, err := os.Stat(filepath.Join(envDir, accountVaultFile)); !os.IsNotExist(err) {
		t.Fatalf("不该写到环境变量指的目录: %v", err)
	}
}
