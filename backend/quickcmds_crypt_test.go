//go:build !noterminal

// 快捷指令属于终端功能（见 quickcmds.go），无终端构建里一并跳过。

package main

// quickcmds_crypt_test.go —— 快捷指令命令内容的落盘加密（quickcmds.go + secret.go）。
//
// 参考实现：terminal / webssh 两个项目的同名测试（webssh 那侧还多一轮复审，本文件把这轮
// 复审的加固也钉住：非法/重复 id 重发、迁移与保存之间的丢失更新）。
//
// 三条贯穿的判据：
//  1. **磁盘上不许出现命令内容的明文，但名称必须是明文**；
//  2. **保存 → 读回 → 换一把 cryptor 再读（模拟重启），内容逐字节相同** —— 加密不能动内容，
//     尤其是前导/行尾空白与多行格式；
//  3. **解不开必须报错**，绝不能静默返回空内容（那会让用户以为指令没了，一保存就把服务端
//     那份覆盖掉）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// trickyContent 是「加解密不能破坏命令的内容和格式」的判据：前导空格、行尾空格、续行反斜杠
// 与多行换行都必须原样回来（后端曾在 sanitize 里 trim 过内容，这几项正好是它会改掉的东西）。
const trickyContent = "  mysql -uroot -pS3cret \\\n    -h db.internal \\\n    app_db   \n"

// quickCmdsFileAt 读回磁盘上的那一份（用后端自己的类型，自动走 UnmarshalJSON）。
func quickCmdsFileAt(t *testing.T, path string) quickCmdsFile {
	t.Helper()
	var f quickCmdsFile
	raw := readFileStr(t, path)
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("解析 %s 失败：%v\n%s", path, err, raw)
	}
	return f
}

// writeLegacyQuickCmds 写一份**旧格式**（`content` 是明文）的表。
func writeLegacyQuickCmds(t *testing.T, path string, entries ...map[string]interface{}) {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{"version": 1, "commands": entries})
	if err != nil {
		t.Fatalf("构造旧文件失败：%v", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("写旧文件失败：%v", err)
	}
}

// legacyEntry 是一条旧格式的指令（明文 content）。
func legacyEntry(id, name, content string, auto bool) map[string]interface{} {
	return map[string]interface{}{"id": id, "name": name, "content": content, "auto": auto}
}

// 内容密文落盘、名称明文落盘、内容原样往返（含「重启」= 换一把同路径的 cryptor）。
func TestQuickCmdsContentEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	keyPath := filepath.Join(dir, masterKeyFileName)
	crypt := testCryptorAt(t, keyPath)

	cmds := []QuickCmd{{ID: "db-01", Name: "数据库", Content: trickyContent, Auto: true}}
	if err := saveQuickCmds(path, crypt, cmds); err != nil {
		t.Fatalf("saveQuickCmds: %v", err)
	}

	raw := readFileStr(t, path)
	for _, leak := range []string{"mysql -uroot", "pS3cret", "app_db"} {
		if strings.Contains(raw, leak) {
			t.Errorf("磁盘上出现了明文的命令内容 %q：\n%s", leak, raw)
		}
	}
	if !strings.Contains(raw, "数据库") {
		t.Errorf("名称应当是明文（排查文件时要看得懂）：\n%s", raw)
	}
	f := quickCmdsFileAt(t, path)
	if f.Version != 2 {
		t.Errorf("文件版本 = %d，期望 2", f.Version)
	}
	if len(f.Commands) != 1 {
		t.Fatalf("指令数 = %d", len(f.Commands))
	}
	if c := f.Commands[0].Content; c == nil || c.V != 1 || len(c.CT) == 0 {
		t.Errorf("内容不是密文信封：%+v", c)
	}

	// 同一把密钥读回：内容逐字节相同
	got, err := loadQuickCmds(path, crypt)
	if err != nil {
		t.Fatalf("loadQuickCmds: %v", err)
	}
	if len(got) != 1 || got[0].Content != trickyContent || got[0].Name != "数据库" || !got[0].Auto {
		t.Fatalf("读回的内容不符：%+v", got)
	}

	// 「重启」= 用同一把 master.key 新建 cryptor
	restarted := testCryptorAt(t, keyPath)
	got, err = loadQuickCmds(path, restarted)
	if err != nil {
		t.Fatalf("重启后 loadQuickCmds: %v", err)
	}
	if got[0].Content != trickyContent {
		t.Fatalf("重启后内容 = %q，期望 %q", got[0].Content, trickyContent)
	}
}

// 换了一把主密钥（密钥文件被替换/删除重建）时，读必须明确报错。
func TestQuickCmdsWrongMasterKeyFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	if err := saveQuickCmds(path, testCryptorAt(t, filepath.Join(dir, masterKeyFileName)),
		[]QuickCmd{{ID: "a1", Name: "甲", Content: "echo 甲", Auto: false}}); err != nil {
		t.Fatalf("saveQuickCmds: %v", err)
	}

	other := testCryptor(t) // 另一把随机密钥
	_, err := loadQuickCmds(path, other)
	if err == nil {
		t.Fatal("换密钥后应当报错，而不是静默给出空内容")
	}
	if !strings.Contains(err.Error(), "甲") {
		t.Errorf("错误里应当指出是哪条指令：%v", err)
	}
}

// 旧格式（明文 content）的表：**读到就加密重写**，内容与格式不变。
func TestQuickCmdsLegacyPlaintextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	keyPath := filepath.Join(dir, masterKeyFileName)
	crypt := testCryptorAt(t, keyPath)

	writeLegacyQuickCmds(t, path, legacyEntry("legacy01", "旧表", trickyContent, true))

	got, err := loadQuickCmds(path, crypt)
	if err != nil {
		t.Fatalf("旧文件读不出来：%v", err)
	}
	if len(got) != 1 || got[0].Content != trickyContent || got[0].ID != "legacy01" ||
		got[0].Name != "旧表" || !got[0].Auto {
		t.Fatalf("旧文件读出来的内容不对：%+v", got)
	}

	// **仅仅读了一次**（没有任何保存）：磁盘上就已经没有明文了
	raw := readFileStr(t, path)
	for _, leak := range []string{"mysql -uroot", "pS3cret"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("读一次之后磁盘上还有明文命令 %q：\n%s", leak, raw)
		}
	}
	f := quickCmdsFileAt(t, path)
	if f.Version != 2 || len(f.Commands) != 1 || f.Commands[0].ID != "legacy01" ||
		f.Commands[0].Content == nil || !f.Commands[0].Auto {
		t.Fatalf("迁移后的文件不对：%+v", f)
	}

	// 换一把同路径的 cryptor（= 重启）照样读得出同样的内容
	got, err = loadQuickCmds(path, testCryptorAt(t, keyPath))
	if err != nil {
		t.Fatalf("重启后读取失败：%v", err)
	}
	if got[0].Content != trickyContent {
		t.Fatalf("重启后内容 = %q", got[0].Content)
	}
}

// 更早的**裸数组**格式（`[{...}]`，没有版本信封）同样要能读并迁移。
func TestQuickCmdsLegacyBareArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	crypt := testCryptorAt(t, filepath.Join(dir, masterKeyFileName))

	bare, err := json.Marshal([]map[string]interface{}{legacyEntry("bare01", "裸数组", "  ls -l\n", true)})
	if err != nil {
		t.Fatalf("构造裸数组文件失败：%v", err)
	}
	if err := os.WriteFile(path, bare, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadQuickCmds(path, crypt)
	if err != nil {
		t.Fatalf("裸数组读不出来：%v", err)
	}
	if len(got) != 1 || got[0].Content != "  ls -l\n" || got[0].Name != "裸数组" {
		t.Fatalf("裸数组读出来的内容不对：%+v", got)
	}
	if raw := readFileStr(t, path); strings.Contains(raw, "ls -l") {
		t.Fatalf("裸数组读一次之后磁盘上还有明文：\n%s", raw)
	}
	if f := quickCmdsFileAt(t, path); f.Version != 2 || f.Commands[0].Content == nil {
		t.Fatalf("裸数组没有被迁移：%+v", f)
	}
}

// 启动迁移：**没有任何接口被调用**，磁盘上的明文也要被加密（前端只在打开弹窗时才加载列表，
// 用户几个月不点它，明文就会一直躺着）。
func TestQuickCmdsLegacyMigrationAtStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	crypt := testCryptorAt(t, filepath.Join(dir, masterKeyFileName))
	writeLegacyQuickCmds(t, path, legacyEntry("g1", "全局旧表", trickyContent, true))

	renv := &RuntimeEnv{DataDir: dir, QuickCmdsFile: path}
	if err := migrateQuickCmdsAtStartup(renv, crypt); err != nil {
		t.Fatalf("启动迁移失败：%v", err)
	}
	raw := readFileStr(t, path)
	if strings.Contains(raw, "pS3cret") || strings.Contains(raw, "mysql -uroot") {
		t.Fatalf("启动迁移之后磁盘上还有明文：\n%s", raw)
	}
	f := quickCmdsFileAt(t, path)
	if f.Version != 2 || len(f.Commands) != 1 || f.Commands[0].Content == nil {
		t.Fatalf("启动迁移后的文件不对：%+v", f)
	}

	// 迁移只换信封：内容与 auto 逐字节保住
	got, err := loadQuickCmds(path, crypt)
	if err != nil {
		t.Fatalf("迁移后读取失败：%v", err)
	}
	if got[0].Content != trickyContent || !got[0].Auto {
		t.Fatalf("迁移改动了内容或 auto：%+v", got[0])
	}

	// 文件不存在时是正常空状态，不报错。注意 QuickCmdsFile 必须显式给出：空着会退回统一
	// 数据目录（TRIM_PKGVAR），在真机上那就是**线上**的数据目录。
	empty := &RuntimeEnv{DataDir: t.TempDir(), QuickCmdsFile: filepath.Join(t.TempDir(), "quickcmds.json")}
	if err := migrateQuickCmdsAtStartup(empty, crypt); err != nil {
		t.Fatalf("没有文件时不该报错：%v", err)
	}
}

// 明文内容没法保留时（空内容）：换成**加密后**的占位文案并关掉 auto —— 磁盘上不留空内容、
// 更不留明文；用户在界面上看到这句话，知道要重新填。
func TestQuickCmdsLegacyEmptyContentBecomesPlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	keyPath := filepath.Join(dir, masterKeyFileName)
	crypt := testCryptorAt(t, keyPath)
	writeLegacyQuickCmds(t, path, legacyEntry("empty01", "空内容", "", true))

	got, err := loadQuickCmds(path, crypt)
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if len(got) != 1 || got[0].Content != quickCmdMigrationPlaceholder {
		t.Fatalf("空内容的迁移结果 = %+v，期望占位文案", got)
	}
	if got[0].Auto {
		t.Error("占位文案不该保留「点一下就自动回车」（那会把一句提示当命令跑掉）")
	}
	if got[0].Name != "空内容" {
		t.Errorf("名称不该被改：%+v", got[0])
	}

	// 磁盘上：有信封、没有明文占位文案
	raw := readFileStr(t, path)
	if strings.Contains(raw, "命令加密失败已清空") {
		t.Fatalf("占位文案本身也必须密文落盘：\n%s", raw)
	}
	if f := quickCmdsFileAt(t, path); f.Commands[0].Content == nil || f.Commands[0].Auto {
		t.Fatalf("迁移后的文件不对：%+v", f.Commands[0])
	}

	// 重启后解出来的还是同一句占位（说明它真是密文）
	got, err = loadQuickCmds(path, testCryptorAt(t, keyPath))
	if err != nil {
		t.Fatalf("重启后读取失败：%v", err)
	}
	if got[0].Content != quickCmdMigrationPlaceholder {
		t.Fatalf("重启后内容 = %q", got[0].Content)
	}
}

// 三类「密文不可信」：字节被改、密文在表内互换、主密钥不可用。三种情况都必须是**明确的
// 错误**，绝不能静默返回空内容。
func TestQuickCmdsDecryptFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	crypt := testCryptorAt(t, filepath.Join(dir, masterKeyFileName))

	cmds := []QuickCmd{
		{ID: "aaa", Name: "甲", Content: "echo 甲", Auto: false},
		{ID: "bbb", Name: "乙", Content: "echo 乙", Auto: false},
	}
	if err := saveQuickCmds(path, crypt, cmds); err != nil {
		t.Fatalf("saveQuickCmds: %v", err)
	}

	// ① 密文被改了一个字节（GCM 认证失败）
	f := quickCmdsFileAt(t, path)
	ct := []byte(f.Commands[0].Content.CT)
	mid := len(ct) / 2
	if ct[mid] == 'A' {
		ct[mid] = 'B'
	} else {
		ct[mid] = 'A'
	}
	f.Commands[0].Content.CT = string(ct)
	rewriteStoredQuickCmds(t, path, f)
	assertQuickCmdsDecryptError(t, path, crypt, "甲")

	// ② 同一张表里两条指令的密文互换（AAD 绑定了指令 id，所以解不开）
	if err := saveQuickCmds(path, crypt, cmds); err != nil {
		t.Fatalf("saveQuickCmds: %v", err)
	}
	f = quickCmdsFileAt(t, path)
	f.Commands[0].Content, f.Commands[1].Content = f.Commands[1].Content, f.Commands[0].Content
	rewriteStoredQuickCmds(t, path, f)
	assertQuickCmdsDecryptError(t, path, crypt, "甲")

	// ③ 主密钥不可用（crypt == nil）：同样报错，且**不把明文交出去**
	_ = saveQuickCmds(path, crypt, []QuickCmd{{ID: "aaa", Name: "甲", Content: "echo 甲"}})
	if _, err := loadQuickCmds(path, nil); err == nil {
		t.Fatal("主密钥不可用时读密文表必须报错")
	}
}

// assertQuickCmdsDecryptError 断言读这张表会明确报「解密失败」，并带上出问题的指令名。
func assertQuickCmdsDecryptError(t *testing.T, path string, crypt *cryptor, wantName string) {
	t.Helper()
	_, err := loadQuickCmds(path, crypt)
	if err == nil {
		t.Fatalf("应当报解密错误（%s），实得 nil", wantName)
	}
	if !strings.Contains(err.Error(), "decrypt") || !strings.Contains(err.Error(), wantName) {
		t.Fatalf("错误文案没说清是哪条指令解密失败：%v", err)
	}
}

// rewriteStoredQuickCmds 把改过的磁盘形态写回去（模拟「有人动过数据目录」）。
func rewriteStoredQuickCmds(t *testing.T, path string, f quickCmdsFile) {
	t.Helper()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("写回 %s 失败：%v", path, err)
	}
}

// 主密钥不可用时：保存**必须失败**，且磁盘上不能出现明文；已有的明文文件也一个字节都不动
// （宁可留着明文等用户修好密钥，也不能「看起来迁移成功」地写坏）。
func TestQuickCmdsSaveRefusesWithoutKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	crypt := testCryptorAt(t, filepath.Join(dir, masterKeyFileName))

	err := saveQuickCmds(path, nil, []QuickCmd{{ID: "a", Name: "甲", Content: "echo 秘密"}})
	if err == nil {
		t.Fatal("主密钥不可用时保存必须失败")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		raw := readFileStr(t, path)
		t.Fatalf("失败路径不该留下文件，或至少不能含明文：\n%s", raw)
	}

	// 已存在的明文表：crypt == nil 时读要报错，且文件保持原样（留给用户修好密钥后重试）
	writeLegacyQuickCmds(t, path, legacyEntry("l1", "旧", "echo 秘密", false))
	before := readFileStr(t, path)
	if _, err := loadQuickCmds(path, nil); err == nil {
		t.Fatal("主密钥不可用时读明文表必须报错（不能把明文交回去）")
	}
	if after := readFileStr(t, path); after != before {
		t.Fatalf("失败路径改动了文件：\n前：%s\n后：%s", before, after)
	}
	// 修好密钥（同一把）后照常迁移
	if _, err := loadQuickCmds(path, crypt); err != nil {
		t.Fatalf("密钥恢复后应当能迁移：%v", err)
	}
	if raw := readFileStr(t, path); strings.Contains(raw, "echo 秘密") {
		t.Fatalf("密钥恢复后没有迁移：\n%s", raw)
	}
}

// 「读 → 迁移 → 重写」与「整表保存」之间的丢失更新：迁移手里那份**过期快照**不许把用户
// 刚保存的表整表覆盖（webssh 复审里踩到的坑）。
//
// 判据：每一轮并发跑 { load（会触发迁移）, save（一张全新的表） }，断言**最终磁盘上的表就是
// save 提交的那一张**。两种合法时序都会满足它 —— load 先跑完（迁移旧的、再被 save 盖掉）、
// 或 save 先跑完（load 读到的已是新表、无明文可迁移、根本不写）。只有「load 读到旧表 →
// save 写新表 → load 把旧表的迁移结果写回去」这一种时序会失败；**去掉 quickCmdsMu 就能跑红**。
func TestQuickCmdsMigrationSaveLostUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	crypt := testCryptorAt(t, filepath.Join(dir, masterKeyFileName))

	legacy := make([]map[string]interface{}, 0, 50)
	for i := 0; i < 50; i++ {
		legacy = append(legacy, legacyEntry(fmt.Sprintf("old%02d", i), fmt.Sprintf("旧%d", i),
			"echo "+strings.Repeat("x", 200), true))
	}
	saved := []QuickCmd{{ID: "new01", Name: "新表", Content: "echo NEW", Auto: false}}

	const rounds = 40
	for round := 0; round < rounds; round++ {
		writeLegacyQuickCmds(t, path, legacy...)

		var wg sync.WaitGroup
		var loadErr, saveErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, loadErr = loadQuickCmdsFile(path, crypt)
		}()
		go func() {
			defer wg.Done()
			<-start
			saveErr = saveQuickCmds(path, crypt, saved)
		}()
		close(start)
		wg.Wait()

		if loadErr != nil {
			t.Fatalf("第 %d 轮读表失败：%v", round, loadErr)
		}
		if saveErr != nil {
			t.Fatalf("第 %d 轮保存失败：%v", round, saveErr)
		}

		f := quickCmdsFileAt(t, path)
		if f.Version != 2 || len(f.Commands) != 1 || f.Commands[0].ID != "new01" {
			t.Fatalf("第 %d 轮：save 提交的表被迁移的过期快照覆盖了（磁盘上 %d 条）：\n%s",
				round, len(f.Commands), readFileStr(t, path))
		}
		if got, err := decryptQuickCmdContent(crypt, f.Commands[0]); err != nil || got != "echo NEW" {
			t.Fatalf("第 %d 轮：磁盘上的内容 = %q（err=%v），期望 echo NEW", round, got, err)
		}
	}
}

// 表内 id 重复 / 非法（含冒号）时重发：迁移后 id 互不相同且都合法、**表内第一个出现的 id
// 保留**、名称/内容/auto 逐字节不变（内容必须跟着新 id 重新加密，否则 AAD 对不上、读出来就是
// 「解密失败」），而且此时两条密文互换必须解不开。
func TestQuickCmdsReissuesDupAndInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	keyPath := filepath.Join(dir, masterKeyFileName)
	crypt := testCryptorAt(t, keyPath)

	// 三条：两条 id 相同（手工文件能造），一条 id 含冒号（validQuickCmdID 不认，而且它会让
	// AAD 里 `quickcmd:<id>:<scope>` 的分隔符产生歧义）。
	writeLegacyQuickCmds(t, path,
		legacyEntry("dup1", "甲", "echo 甲", false),
		legacyEntry("dup1", "乙", "echo 乙", true),
		legacyEntry("bad:id", "丙", "echo 丙", false),
	)

	got, err := loadQuickCmds(path, crypt)
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if len(got) != 3 {
		t.Fatalf("三条指令应当都在：%+v", got)
	}
	want := []struct {
		name    string
		content string
		auto    bool
	}{{"甲", "echo 甲", false}, {"乙", "echo 乙", true}, {"丙", "echo 丙", false}}
	seen := map[string]bool{}
	for i, c := range got {
		if !validQuickCmdID(c.ID) {
			t.Errorf("第 %d 条的 id %q 仍然非法", i, c.ID)
		}
		if seen[c.ID] {
			t.Errorf("第 %d 条的 id %q 在表里重复", i, c.ID)
		}
		seen[c.ID] = true
		if c.Name != want[i].name || c.Content != want[i].content || c.Auto != want[i].auto {
			t.Errorf("第 %d 条被改坏了：%+v，期望 %+v", i, c, want[i])
		}
		if i == 0 && c.ID != "dup1" {
			t.Errorf("表里第一个出现的 id 应当保留，实际 %q", c.ID)
		}
		if i > 0 && c.ID == "dup1" {
			t.Errorf("第 %d 条的重复 id 应当被重发", i)
		}
	}

	// 磁盘上是密文，且换一把同路径的 cryptor（重启）后照样读得出来 —— 说明是**用新 AAD
	// 重新加密**，不是把旧密文挂在新 id 下面（后者重启后会变成「解密失败」）。
	raw := readFileStr(t, path)
	for _, leak := range []string{"echo 甲", "echo 乙", "echo 丙"} {
		if strings.Contains(raw, leak) {
			t.Errorf("迁移后磁盘上还有明文命令 %q：\n%s", leak, raw)
		}
	}
	restarted := testCryptorAt(t, keyPath)
	got, err = loadQuickCmds(path, restarted)
	if err != nil {
		t.Fatalf("重启后读取失败：%v", err)
	}
	for i, c := range got {
		if c.Content != want[i].content || c.Auto != want[i].auto {
			t.Errorf("重启后第 %d 条内容变了：%+v", i, c)
		}
	}

	// 互换任意两条的密文，用它们各自的 id 都必须解不开（id 唯一 + AAD 绑定 id 的直接后果）。
	f := quickCmdsFileAt(t, path)
	for i := 0; i < len(f.Commands); i++ {
		for j := i + 1; j < len(f.Commands); j++ {
			left, right := f.Commands[i], f.Commands[j]
			if _, err := decryptQuickCmdContent(restarted, left); err != nil {
				t.Fatalf("第 %d 条自己的密文应当解得开：%v", i, err)
			}
			swapped := left
			swapped.Content = right.Content
			if _, err := decryptQuickCmdContent(restarted, swapped); err == nil {
				t.Errorf("第 %d 条（id=%s）与第 %d 条（id=%s）的密文互换后竟然解得开",
					i, left.ID, j, right.ID)
			}
		}
	}

	// 第二阶段：**已经是密文**的表里出现重复 id（同样只有手工改文件才造得出：接口那侧
	// sanitizeQuickCmds 会去重）。这条路要先把第二条的密文按旧 id 解开、再按新 id 重新加密。
	dup := []QuickCmd{
		{ID: "same01", Name: "丁", Content: "echo 丁", Auto: false},
		{ID: "same01", Name: "戊", Content: "echo 戊", Auto: true},
	}
	if err := saveQuickCmds(path, crypt, dup); err != nil {
		t.Fatalf("准备重复 id 的密文表失败：%v", err)
	}
	got, err = loadQuickCmds(path, crypt)
	if err != nil {
		t.Fatalf("密文表读取失败：%v", err)
	}
	if len(got) != 2 {
		t.Fatalf("密文表去重后指令数 = %d", len(got))
	}
	if got[0].ID != "same01" {
		t.Errorf("密文表里第一个出现的 id 应当保留，实际 %q", got[0].ID)
	}
	if got[1].ID == "same01" || !validQuickCmdID(got[1].ID) {
		t.Errorf("密文表里的重复 id 应当被重发成合法 id，实际 %q", got[1].ID)
	}
	dupWant := []struct {
		name    string
		content string
		auto    bool
	}{{"丁", "echo 丁", false}, {"戊", "echo 戊", true}}
	for i, c := range got {
		if c.Name != dupWant[i].name || c.Content != dupWant[i].content || c.Auto != dupWant[i].auto {
			t.Errorf("密文表去重后第 %d 条被改坏了：%+v，期望 %+v", i, c, dupWant[i])
		}
	}
	// 重发后的 id 必须与重写后的密文一致：重启后照样读得出同样的内容。
	got, err = loadQuickCmds(path, testCryptorAt(t, keyPath))
	if err != nil {
		t.Fatalf("密文表重启后读取失败：%v", err)
	}
	for i, c := range got {
		if c.Content != dupWant[i].content {
			t.Errorf("密文表去重、重启后第 %d 条内容 = %q，期望 %q", i, c.Content, dupWant[i].content)
		}
	}
}

// sanitizeQuickCmds：名称 trim、**命令内容原样保留**、id 缺失/非法补一个、重复 id 去重。
func TestSanitizeQuickCmdsKeepsCommandContent(t *testing.T) {
	got, err := sanitizeQuickCmds([]QuickCmd{
		{ID: "keep", Name: "  多条  ", Content: trickyContent, Auto: true},
		{Name: "无 id", Content: "  ls -l\n", Auto: false},
		{ID: "bad:id", Name: "非法 id", Content: "pwd", Auto: false},
		{ID: "dup", Name: "重复一", Content: "1", Auto: false},
		{ID: "dup", Name: "重复二", Content: "2", Auto: false},
	})
	if err != nil {
		t.Fatalf("sanitizeQuickCmds: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("重复 id 应当被去重：%+v", got)
	}
	if got[0].Name != "多条" || got[0].Content != trickyContent {
		t.Errorf("名称应当 trim、内容应当原样保留：%+v", got[0])
	}
	for i, c := range got {
		if !validQuickCmdID(c.ID) {
			t.Errorf("第 %d 条的 id %q 应当被补齐/纠正", i, c.ID)
		}
	}
	if got[3].ID != "dup" || got[3].Name != "重复一" {
		t.Errorf("重复 id 应当保留先出现的那条：%+v", got[3])
	}
	if got[1].Content != "  ls -l\n" {
		t.Errorf("内容里的前导空白与换行被改掉了：%q", got[1].Content)
	}

	// 名称/内容为空 → 报错（内容只看「全是空白」）
	if _, err := sanitizeQuickCmds([]QuickCmd{{ID: "a", Name: " ", Content: "ls"}}); err == nil {
		t.Error("空名称应当被拒")
	}
	if _, err := sanitizeQuickCmds([]QuickCmd{{ID: "a", Name: "n", Content: " \n\t "}}); err == nil {
		t.Error("全是空白的内容应当被拒")
	}
}

// 读写失败要回「code + 英文诊断原文」：前端才拿得到能随界面语言切换的文案
// （AGENTS 规则 13）。这里把两条失败路径的 HTTP 响应钉住。
func TestQuickCmdsHandlerErrorsCarryCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quickcmds.json")
	enc := testCryptorAt(t, filepath.Join(dir, masterKeyFileName))
	if err := saveQuickCmds(path, enc, []QuickCmd{{ID: "db-01", Name: "disk", Content: "df -h"}}); err != nil {
		t.Fatal(err)
	}

	// ① 解不开（换了一把主密钥）→ 500 + err_quickcmds_load_failed，diagnostic 在 detail 里。
	other := &AdminMux{renv: &RuntimeEnv{QuickCmdsFile: path}, crypt: testCryptor(t)}
	rec := httptest.NewRecorder()
	other.handleGetQuickCmds(rec, httptest.NewRequest(http.MethodGet, "/api/quickcmds", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("解不开时应 500，实得 %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Code   string            `json:"code"`
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应体不是 JSON: %v\n%s", err, rec.Body.String())
	}
	if got.Code != "err_quickcmds_load_failed" {
		t.Fatalf("应带 err_quickcmds_load_failed，实得 %q", got.Code)
	}
	if got.Params["detail"] == "" {
		t.Fatalf("detail 参数不该为空（诊断原文要能看到）: %+v", got.Params)
	}

	// ② 主密钥不可用（crypt == nil）→ 保存 500 + err_quickcmds_save_failed，且盘上一个字节都不动。
	before := readFileStr(t, path)
	noKey := &AdminMux{renv: &RuntimeEnv{QuickCmdsFile: path}}
	rec2 := httptest.NewRecorder()
	noKey.handleSaveQuickCmds(rec2, httptest.NewRequest(http.MethodPost, "/api/quickcmds",
		strings.NewReader(`{"commands":[{"id":"db-01","name":"disk","content":"df -h"}]}`)))
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("主密钥不可用时应 500，实得 %d: %s", rec2.Code, rec2.Body.String())
	}
	if code := errorCodeOf(t, rec2.Body.Bytes()); code != "err_quickcmds_save_failed" {
		t.Fatalf("应带 err_quickcmds_save_failed，实得 %q", code)
	}
	if after := readFileStr(t, path); after != before {
		t.Fatalf("主密钥不可用时不该改写文件：\n%s", after)
	}
}
