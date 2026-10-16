package main

// accountvault.go —— 控制台自己的「DeepSeek 账号库」（多账号）。
//
// dsh 只认**一条**账号凭据（固定键 `deepseek-account-platform/default`，见 account.go），
// 它自己没有「多账号」概念 —— 所以多账号只能是：控制台这边存多份 token，切换时把对应的
// 那份重新写进那条记录（credentials-local 监听凭据文件，写入即生效；账号插件在
// `credentials/record-updated` 上清了资料/余额缓存，所以换完立刻就是新账号的数据）。
//
// 库文件落在**控制台自己的数据目录**（`$HARNESS_DATA_DIR/dsh-accounts.json`，0600）：
// 刻意不往 dsh 的凭据文件里塞额外记录 —— 那个文件里装着用户其它密钥、格式也属于 dsh，
// 我们只写它认识的那两条（default / device）。
//
// **整份库是密文落盘**（AES-256-GCM，主密钥与快捷指令同一把，见 secret.go 的威胁模型）：
// dsh 只认一条凭据记录，所以「多账号」意味着库里同时躺着若干个**没在生效**的 token ——
// 拿到磁盘快照/备份的人不该因此一次拿走全部账号。这与 dsh 那份 `.credentials.yaml` 是两回事：
// 那个文件**必须**明文（dsh 逐字读 payload 里的 token 当 Bearer 用，塞密文进去等于把密文发给
// 平台），而这份文件完全由控制台拥有，格式也由控制台定义。
//
// 「当前生效的账号」不额外记状态，而是**推导**出来的：拿 dsh 凭据里的 token 与库里各条目
// 比对，命中的那条就是在用的。token 每次登录都会换新，所以条目按平台的 user.id 归并
// （见 accountLoginComplete 的 upsert）；id 缺失时退回按 token 摘要生成的本地 id。

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// accountVaultFile 是账号库文件名（默认拼在控制台数据目录下，见 config.go 的 dataPath）。
const accountVaultFile = "dsh-accounts.json"

// accountVaultVersion 是账号库**明文文档**的版本（信封里面的那份）。
const accountVaultVersion = 1

// accountVaultDocVersion 是磁盘文档的版本：2 = 整份列表是一个密文信封。
//
// 版本 1（明文 JSON）**没有兼容路径**：这一版还没对外发布过，旧文件直接删掉、重新登录即可。
// 因此读到 version 1 会明确报错，而不是当空库读 —— 那会让「旧文件还在」被当成「库里没有
// 账号」，紧接着任何一次保存都会把明文库覆盖掉（用户重新登录前就先丢一次数据）。
const accountVaultDocVersion = 2

// accountVaultAADField / accountVaultAADScope 是库密文的 AAD 组成（形态见 secret.go 的 aad）。
//
// 整份文档只用一个信封，所以 field 只标「这是账号库」：把快捷指令那条密文剪到这里（或反过来）
// 会直接解密失败，而不是把一段无关密文当成账号 token 用。
const (
	accountVaultAADField = "accountvault"
	accountVaultAADScope = "global"
)

// accountVaultStored 是账号库在磁盘上的形态：`{"version":2,"vault":{"v":1,"ct":"…"}}`。
// Vault 为 nil = 文档里没有信封（旧格式 / 被截断），由 loadAccountVault 分流报错。
type accountVaultStored struct {
	Version int             `json:"version"`
	Vault   *secretEnvelope `json:"vault"`
}

// accountVaultEntry 是一个已保存的账号（含 token —— 磁盘上是密文，见 loadAccountVault /
// saveAccountVault；内存里与接口收发的始终是明文）。
type accountVaultEntry struct {
	ID         string `json:"id"`
	Contact    string `json:"contact,omitempty"`
	Name       string `json:"name,omitempty"`
	AvatarURL  string `json:"avatarUrl,omitempty"`
	Token      string `json:"token"`
	Issuer     string `json:"issuer,omitempty"`
	AddedAt    string `json:"addedAt,omitempty"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
}

type accountVault struct {
	Version  int                 `json:"version"`
	Accounts []accountVaultEntry `json:"accounts"`
}

// accountVaultView 是给前端的账号条目（**不带 token**）。
type accountVaultView struct {
	ID         string `json:"id"`
	Contact    string `json:"contact,omitempty"`
	Name       string `json:"name,omitempty"`
	AvatarURL  string `json:"avatarUrl,omitempty"`
	Active     bool   `json:"active"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
}

// accountVaultPath 解析账号库路径：已解析的运行时路径优先，否则退回统一数据目录下的
// dsh-accounts.json（与 quickCmdsPath / masterKeyPath 同一套兜底写法，单测会构造空的
// RuntimeEnv）。控制台自产文件一律跟着 DataDir 走（AGENTS.md 规则 2）。
func accountVaultPath(renv *RuntimeEnv) string {
	if renv != nil && renv.DataDir != "" {
		return filepath.Join(renv.DataDir, accountVaultFile)
	}
	return dataPath(accountVaultFile)
}

// accountVaultMu 串行化账号库的**整段「读 → 改 → 写」**（accountVaultMutate）。
//
// 为什么必须覆盖整段、而不是只锁单次写：调用方拿到库之后往往还要做几秒钟的事
// （切换账号要等 dsh 认账最多 5 秒、移除要先请平台作废 token），这期间别的入口
// （另一个标签页的「添加账号」、状态查询的自愈导入）完全可能已经改过库；最后那次
// save 把**几秒前的快照**整份写回去，就是静默的数据丢失 —— 被移除的账号复活、
// 新加的账号消失，双方都不报错（快捷指令那边踩过同一个坑，见 quickcmds.go 的
// quickCmdsMu；回归测试见 accountvault_crypt_test.go 的 TestAccountVaultMutateNoLostUpdate）。
//
// 纪律：**不许重入** —— 锁内只允许做纯内存操作（含读文件），不许再调 accountVaultMutate，
// 也不许做平台请求 / 等 dsh 认账这类慢操作（那些必须在取锁之前做完）。
var accountVaultMu sync.Mutex

// accountVaultMutate 是账号库**唯一**的写入入口：在锁内重读最新库、应用 fn、落盘。
//
// 这样调用方就不用（也不该）自己「先 load 一个快照、改完再 save」：每次变更都基于锁内
// 重读的那一份，谁都不会覆盖谁的改动。fn 返回错误或 load 失败时**一个字节都不写**。
func accountVaultMutate(path string, crypt *cryptor, fn func(*accountVault) error) error {
	accountVaultMu.Lock()
	defer accountVaultMu.Unlock()
	if fn == nil {
		return errors.New("accountVaultMutate requires a mutation function")
	}
	v, err := loadAccountVault(path, crypt)
	if err != nil {
		return err
	}
	if err := fn(v); err != nil {
		return err
	}
	return saveAccountVault(path, crypt, v)
}

// loadAccountVault 读账号库；文件不存在时返回空库（不报错）。
//
// 解不开**必须**报错，绝不返回空库：调用方（状态查询的自愈导入、登录成功后的 upsert）拿到
// 空库就会把原始密文覆盖掉，而真正的原因（主密钥换了 / 密文被搬了 / 文件被截断）再也查不出来。
// 主密钥不可用（crypt == nil）同样在这里报错，语义见 secret.go。
//
// 热路径上的「读 → 改 → 写」请走 accountVaultMutate（它自己会读）；本函数只给只读用途。
func loadAccountVault(path string, crypt *cryptor) (*accountVault, error) {
	text, err := accountReadFileText(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return &accountVault{Version: accountVaultVersion}, nil
	}
	var stored accountVaultStored
	if err := json.Unmarshal([]byte(text), &stored); err != nil {
		return nil, fmt.Errorf("account vault is not a readable JSON document: %w", err)
	}
	if stored.Vault == nil {
		if accountVaultLooksLegacy(text) {
			return nil, fmt.Errorf("account vault is still in the legacy plaintext format (version 1); delete %s and sign in again", accountVaultFile)
		}
		return nil, fmt.Errorf("account vault has no encrypted envelope (version %d, probably truncated)", stored.Version)
	}
	plain, err := crypt.decrypt(accountVaultAADField, accountVaultAADScope, stored.Vault)
	if err != nil {
		return nil, fmt.Errorf("account vault decryption failed: %w", err)
	}
	v := &accountVault{Version: accountVaultVersion}
	if strings.TrimSpace(plain) == "" {
		return v, nil
	}
	if err := json.Unmarshal([]byte(plain), v); err != nil {
		return nil, fmt.Errorf("account vault content is not a readable JSON document: %w", err)
	}
	if v.Version == 0 {
		v.Version = accountVaultVersion
	}
	return v, nil
}

// accountVaultLooksLegacy 判断这份文档是不是旧版（明文）账号库：按旧结构解得出 accounts 键。
// 只用来把错误说得更明白（普通 JSON 坏文件走另一条分支）。
func accountVaultLooksLegacy(text string) bool {
	var legacy struct {
		Accounts json.RawMessage `json:"accounts"`
	}
	return json.Unmarshal([]byte(text), &legacy) == nil && legacy.Accounts != nil
}

// saveAccountVault 原子落盘：整份列表加密成一个信封，临时文件 → 0600 → rename
// （复用凭据文件的写实现）。
//
// 生产路径上**只应由 accountVaultMutate 在锁内调用**（它已经握着最新那份库）——直接调用
// 就是「拿一份可能过期的快照整份覆盖」，见 accountVaultMu 的说明。
//
// 主密钥不可用时 encrypt 直接报错，**绝不退化成写明文**（见 secret.go / main.go 的语义）。
func saveAccountVault(path string, crypt *cryptor, v *accountVault) error {
	v.Version = accountVaultVersion
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	env, err := crypt.encrypt(accountVaultAADField, accountVaultAADScope, string(raw))
	if err != nil {
		return err
	}
	if env == nil {
		// encrypt("") 返回 nil；账号库永远不是空的（至少是 `{"version":1}`）。
		return errors.New("encrypting the account vault returned an empty envelope")
	}
	out, err := json.MarshalIndent(accountVaultStored{Version: accountVaultDocVersion, Vault: env}, "", "  ")
	if err != nil {
		return err
	}
	return accountWriteFileText(path, string(out)+"\n")
}

// find 按 id 取条目（返回指针，改它等于改库里的条目）。
func (v *accountVault) find(id string) *accountVaultEntry {
	for i := range v.Accounts {
		if v.Accounts[i].ID == id {
			return &v.Accounts[i]
		}
	}
	return nil
}

// upsert 写入/更新一条账号（按 id 归并；重复登录同一账号只换 token）。
func (v *accountVault) upsert(e accountVaultEntry) {
	if cur := v.find(e.ID); cur != nil {
		if e.AddedAt == "" {
			e.AddedAt = cur.AddedAt
		}
		*cur = e
		return
	}
	v.Accounts = append(v.Accounts, e)
}

// remove 删掉一条账号，返回是否删到了。
func (v *accountVault) remove(id string) bool {
	for i := range v.Accounts {
		if v.Accounts[i].ID == id {
			v.Accounts = append(v.Accounts[:i], v.Accounts[i+1:]...)
			return true
		}
	}
	return false
}

// activeID 返回「凭据文件里那个 token」所属的账号 id（空串 = 库里没有它）。
func (v *accountVault) activeID(activeToken string) string {
	if activeToken == "" {
		return ""
	}
	for i := range v.Accounts {
		if v.Accounts[i].Token == activeToken {
			return v.Accounts[i].ID
		}
	}
	return ""
}

// accountVaultViews 转成前端要的列表（标出正在生效的那条；token 不外传）。
func accountVaultViews(v *accountVault, activeToken string) []accountVaultView {
	if v == nil || len(v.Accounts) == 0 {
		return nil
	}
	active := v.activeID(activeToken)
	out := make([]accountVaultView, 0, len(v.Accounts))
	for _, e := range v.Accounts {
		out = append(out, accountVaultView{
			ID:         e.ID,
			Contact:    e.Contact,
			Name:       e.Name,
			AvatarURL:  e.AvatarURL,
			Active:     active != "" && e.ID == active,
			LastUsedAt: e.LastUsedAt,
		})
	}
	return out
}

// accountVaultID 给一个账号算稳定的本地 id：优选用平台 userId，缺失时退回
// 「token 摘要」——足以在同一台设备内区分不同 token，且不泄露 token 本身。
func accountVaultID(userID, token string) string {
	if strings.TrimSpace(userID) != "" {
		return userID
	}
	sum := sha256.Sum256([]byte(token))
	return "local-" + base64.RawURLEncoding.EncodeToString(sum[:6])
}

// accountUserFields 从 auth_exchange 的 user 字段里取展示用的资料（字段名宽容些：
// 平台与不同版本用过 mobile_number/email/avatar_url 等写法）。
func accountUserFields(user interface{}) (id, contact, name, avatar string) {
	m, ok := user.(map[string]interface{})
	if !ok {
		return "", "", "", ""
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	return str("id", "user_id"), str("mobile_number", "email", "contact"),
		str("name", "nickname"), str("avatar_url", "avatarUrl", "avatar")
}

// accountVaultError 把库的读写错误包成面向用户的文案（错误原文进 detail 参数，便于排查）。
//
// 刻意不复用 err_account_store_failed（那句文案是「写入账号凭据失败」，说的是 dsh 的凭据
// 文件）：库读不出来时说「写入凭据失败」会让用户往错误的方向查（去翻 credentials.yaml）。
func accountVaultError(err error) error {
	return uiErr("err_account_vault_failed", "账号库读写失败：%s", "detail", err.Error())
}

// accountVaultDebug 只给日志用：库里有几条账号（不泄露 token）。
func accountVaultDebug(v *accountVault) string {
	if v == nil {
		return "0 account(s)"
	}
	return fmt.Sprintf("%d account(s)", len(v.Accounts))
}
