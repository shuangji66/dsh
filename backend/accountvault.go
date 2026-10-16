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
// 「当前生效的账号」不额外记状态，而是**推导**出来的：拿 dsh 凭据里的 token 与库里各条目
// 比对，命中的那条就是在用的。token 每次登录都会换新，所以条目按平台的 user.id 归并
// （见 accountLoginComplete 的 upsert）；id 缺失时退回按 token 摘要生成的本地 id。

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// accountVaultFile 是账号库文件名（拼在控制台数据目录下，见 config.go 的 dataPath）。
const accountVaultFile = "dsh-accounts.json"

// accountVaultEntry 是一个已保存的账号（含 token —— 库文件权限 0600，见 saveAccountVault）。
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

// loadAccountVault 读账号库；文件不存在时返回空库（不报错）。
func loadAccountVault() (*accountVault, error) {
	text, err := accountReadFileText(dataPath(accountVaultFile))
	if err != nil {
		return nil, err
	}
	v := &accountVault{Version: 1}
	if strings.TrimSpace(text) == "" {
		return v, nil
	}
	if err := json.Unmarshal([]byte(text), v); err != nil {
		return nil, err
	}
	if v.Version == 0 {
		v.Version = 1
	}
	return v, nil
}

// saveAccountVault 原子落盘（临时文件 + rename + 0600，复用凭据文件的写实现）。
func saveAccountVault(v *accountVault) error {
	v.Version = 1
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return accountWriteFileText(dataPath(accountVaultFile), string(raw)+"\n")
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
func accountVaultError(err error) error {
	return uiErr("err_account_store_failed", "账号库读写失败：%s", "detail", err.Error())
}

// accountVaultDebug 只给日志用：库里有几条账号（不泄露 token）。
func accountVaultDebug(v *accountVault) string {
	if v == nil {
		return "0 account(s)"
	}
	return fmt.Sprintf("%d account(s)", len(v.Accounts))
}
