//go:build !noterminal

// 快捷指令属于终端功能：终端被编译剔除时，它一并消失（见 terminal_disabled.go）。

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// quickcmds.go —— 终端快捷指令的持久化（数据目录下的 quickcmds.json）。
//
// 前端每次增删改/排序都把**整张表**发回来，后端只做校验与落盘：排序、去重这类规则于是只在
// 前端实现一遍，后端不会多出一份「部分更新」的语义要对齐。
//
// **命令内容加密落盘**（文件版本 2，见 saveQuickCmds）：`content` 以
// `{"v":1,"ct":"<base64(nonce||ciphertext)>"}` 落盘；`id` / `name` / `auto` 仍是明文。
// 用户往快捷指令里放的常常是 `mysql -uroot -p<密码>`、带 token 的 curl 这类东西，而拿到
// 磁盘快照/备份的人不该顺手读到它们（威胁模型见 secret.go 的包注释）；名称是给人看的标签，
// 加密它反而让文件没法排查。
//
// 加密边界只在**落盘这一层**：接口收发的始终是明文 `content`，前端完全不需要知道这件事。
//
// 旧文件（版本 1 的明文内容，甚至更早的裸数组）照常能读，并且会被**主动迁移**成密文：
// 读一张表时就地加密重写，进程启动时还会再迁一次（见 migrateQuickCmdsAtStartup）——
// 前端只在打开弹窗时才加载列表，光靠读时迁移兜不住「用户几个月没打开过快捷指令弹窗」。
//
// 参考实现：terminal / webssh 两个项目的 `quickcmds.go`（含 webssh 复审的加固：读-改-写
// 全程持锁、非法/重复 id 重发）。

// quickCmdMigrationPlaceholder 是「明文内容加密失败」时替换进去的文案（它自己也是密文落盘）。
//
// 为什么要一个可见的占位而不是清成空串：空内容在界面上看不出来被清过，用户会以为是自己记错了；
// 而这条内容已经拿不回来了（加密都失败的机器上，原明文也不该再留在磁盘上）。它是**落盘的数据**
// 而不是界面文案，所以故意不进 i18n 字典——用户以后把界面切成英文，磁盘上那句话也不该跟着变。
const quickCmdMigrationPlaceholder = "命令加密失败已清空，请重新添加"

// QuickCmd is a user-defined terminal quick command as exchanged with the API
// (`content` 是**明文**；加密只发生在落盘那一层）。
type QuickCmd struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Auto    bool   `json:"auto"`
}

// quickCmdStored 是一条指令在磁盘上的形态。
type quickCmdStored struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Auto bool   `json:"auto"`
	// Content 是加密后的命令内容。nil 表示这条指令没有内容（sanitize 拒绝了空内容，正常数据
	// 里不会出现；手工编辑过的文件可能出现，那时照「没有内容」处理）。
	Content *secretEnvelope `json:"content"`
	// plain / legacy 只在读到**版本 1**（明文）文件时有意义，见 UnmarshalJSON。
	// 两个字段都未导出 → 不参与 json.Marshal，也就不会把明文写回磁盘。
	plain  string
	legacy bool
}

// UnmarshalJSON 兼容旧格式的明文内容（`"content": "df -h"`）。
//
// 按 JSON 类型分流：字符串 = 版本 1 的明文，对象 = 版本 2 的密文信封——两种形态不会歧义。
// 读到明文会打上 `legacy` 标记，由 migrateQuickCmds 加密后重写（**不做**「读到就一直用明文、
// 等下次保存再说」那种懒迁移：用户可能几个月都不点一次保存）。
func (c *quickCmdStored) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Auto    bool            `json:"auto"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.ID, c.Name, c.Auto, c.Content, c.plain, c.legacy = raw.ID, raw.Name, raw.Auto, nil, "", false
	v := bytes.TrimSpace(raw.Content)
	switch {
	case len(v) == 0 || bytes.Equal(v, []byte("null")):
		return nil
	case v[0] == '"':
		c.legacy = true
		return json.Unmarshal(v, &c.plain)
	default:
		// 交给 json 解信封（v / ct 两个字段）；内容不是字符串也不是对象（数字、数组……）
		// 时在这里报错，而不是把命令静默变成空串。
		return json.Unmarshal(v, &c.Content)
	}
}

// quickCmdsFile 是磁盘上的整表信封（Version 2 = 命令内容加密）。
type quickCmdsFile struct {
	Version int `json:"version"`
	// Commands 用指针切片没意义（null 与 [] 在前端都当空处理），统一在读取时归一成 []。
	Commands []quickCmdStored `json:"commands"`
}

// maxQuickCmdsBody 是保存请求体的上限（整表提交，正常只有几 KB；没有上限时一个客户端
// 可以用超大 body 把进程内存打爆 —— 该进程守护并管理着 dsh 与全部终端会话）。
const maxQuickCmdsBody = 1 << 20 // 1MB

// maxQuickCmdIDLen 是指令 id 的长度上限。id 会进密文的 AAD（见 quickCmdAADField），
// 也当前端的列表 key 用，长到几百字节没有意义。
const maxQuickCmdIDLen = 64

// validQuickCmdID 校验指令 id：只允许 [0-9a-zA-Z_-]，长度 1..maxQuickCmdIDLen。
//
// 为什么要卡字符集：id 是密文 AAD 的一部分，而 AAD 是 `harness:v1:quickcmd:<id>:<scope>`
// 这种**只靠冒号分隔**的拼接，没有长度前缀。含冒号的 id 会让 (id="x", scope="a:b") 与
// (id="x:a", scope="b") 拼出同一个 AAD；含空格/换行的 id 还会把日志与界面排版弄乱。
// 接口那侧会把非法 id 换成新的（见 sanitizeQuickCmds），迁移那侧同样处理（见 reassignQuickCmdID）。
//
// 允许 '-' 是必须的：前端 `utils/randomId.ts` 发的是带连字符的 UUID v4。
func validQuickCmdID(id string) bool {
	if id == "" || len(id) > maxQuickCmdIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// quickCmdSeq 是随机源不可用时的兜底序号（只在极端故障路径上用，因此不必太讲究）。
var (
	quickCmdSeqMu sync.Mutex
	quickCmdSeqN  int
)

// newQuickCmdID 生成一个 32 位 hex 的指令 id；随机源不可用时退化为「PID-序号」，
// 仍满足 validQuickCmdID。
func newQuickCmdID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		logWarn("[quickcmds] failed to read random bytes for a command id: %v", err)
		quickCmdSeqMu.Lock()
		quickCmdSeqN++
		n := quickCmdSeqN
		quickCmdSeqMu.Unlock()
		return fmt.Sprintf("%d-%d", os.Getpid(), n)
	}
	return hex.EncodeToString(b[:])
}

// quickCmdsMu serializes concurrent reads/writes of the persisted file.
//
// 它必须覆盖**整段「读 → 迁移 → 重写」**，而不只是单次写：loadQuickCmdsFile 遇到明文时会
// 就地加密重写，而 saveQuickCmds 是整表覆盖写；两者交错时，读那一侧手里那份**过期快照**
// （迁移结果）会把用户刚保存的表整表覆盖掉，双方都不报错——用户看到的是「命令莫名消失」
// （前端已乐观更新，直到下次刷新才发现本地与服务端分叉）。这条是 webssh 复审里踩到的坑，
// 回归测试见 quickcmds_crypt_test.go 的 TestQuickCmdsMigrationSaveLostUpdate。
//
// 纪律：**不许重入** —— 持锁的函数里不能再调另一个持锁的函数。持锁的只有 loadQuickCmdsFile
// 与 saveQuickCmds，二者互不调用（migrateQuickCmdsAtStartup 只调前者、自己并不持锁，
// writeQuickCmdsFile 是纯落盘、不持锁）。
var quickCmdsMu sync.Mutex

// quickCmdsPath resolves the persistence file path: 已解析的运行时路径优先，
// 否则退回统一数据目录下的 quickcmds.json。
func quickCmdsPath(renv *RuntimeEnv) string {
	if renv != nil && renv.QuickCmdsFile != "" {
		return renv.QuickCmdsFile
	}
	return dataPath("quickcmds.json")
}

// handleGetQuickCmds returns the saved quick commands (or an empty list when
// the file does not exist yet). Command contents are decrypted here — the API
// always speaks plaintext.
func (m *AdminMux) handleGetQuickCmds(w http.ResponseWriter, r *http.Request) {
	path := quickCmdsPath(m.renv)
	cmds, err := loadQuickCmds(path, m.crypt)
	if err != nil {
		// 失败原因必须让用户看到（诊断信息是英文原文，见 AGENTS 规则 13）：解不开的原因是
		// 「换了主密钥」「密文被搬过」这类只有用户能动手的事，前端把它带在 toast 与弹窗里。
		writeErrU(w, http.StatusInternalServerError, "err_quickcmds_load_failed", "加载快捷指令失败：%s", "detail", err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "commands": cmds})
}

// handleSaveQuickCmds validates and persists the whole quick command list.
func (m *AdminMux) handleSaveQuickCmds(w http.ResponseWriter, r *http.Request) {
	// 整表提交正常只有几 KB：给个上限，免得一个客户端用超大 body 把进程内存打爆。
	r.Body = http.MaxBytesReader(w, r.Body, maxQuickCmdsBody)
	var req struct {
		Commands []QuickCmd `json:"commands"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		writeErr(w, "invalid request body", http.StatusBadRequest)
		return
	}
	cmds, err := sanitizeQuickCmds(req.Commands)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := quickCmdsPath(m.renv)
	if err := saveQuickCmds(path, m.crypt, cmds); err != nil {
		// 主密钥不可用（master key unavailable）也会走到这里：文案带 code，前端能跟着界面
		// 语言切换，而 detail 里的英文诊断串说明是哪一步失败（见 AGENTS 规则 13）。
		writeErrU(w, http.StatusInternalServerError, "err_quickcmds_save_failed", "保存快捷指令失败：%s", "detail", err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "commands": cmds})
}

// sanitizeQuickCmds 校验并规整整张表：补齐/纠正 id、去重、trim 名称。
//
// 名称去空白，**命令内容原样保留**（只判「全是空白 = 空」）：内容里的前导/行尾空格与换行都是
// 命令的一部分（heredoc、多行脚本、`printf '  x'`），顺手 trim 就是把用户写下的东西改掉——
// 前端的 QuickCmdEditDialog 现在也只 trim 名称。两套口径不一致的症状是「保存时看到的和重新
// 打开后看到的不一样」，也会让「加密前后内容逐字节不变」这条承诺失去意义。
//
// id 缺失/非法（见 validQuickCmdID）时**补一个新的**，而不是回 400：新增一条指令时前端本就
// 带上 id，这里的兜底只服务手写的请求，为它回一句没头没脑的报错没有意义。前端会用响应里的
// commands 覆盖本地列表，因此服务端补的 id 立即生效，列表 key 与服务端始终一致。
func sanitizeQuickCmds(in []QuickCmd) ([]QuickCmd, error) {
	out := make([]QuickCmd, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, c := range in {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || strings.TrimSpace(c.Content) == "" {
			return nil, errors.New("quick command requires name and content")
		}
		if !validQuickCmdID(c.ID) {
			c.ID = newQuickCmdID()
		}
		if seen[c.ID] {
			// 同一 id 出现两次：保留先出现的那条（前端按 id 做列表 key，重复 id 会让渲染错乱，
			// 密文的 AAD 也认不出两条同 id 的指令）。
			continue
		}
		seen[c.ID] = true
		out = append(out, c)
	}
	return out, nil
}

// loadQuickCmds 读一张表并把命令内容解成明文（`GET /api/quickcmds` 用）。
func loadQuickCmds(path string, crypt *cryptor) ([]QuickCmd, error) {
	f, err := loadQuickCmdsFile(path, crypt)
	if err != nil {
		return nil, err
	}
	out := make([]QuickCmd, 0, len(f.Commands))
	for _, e := range f.Commands {
		content, err := decryptQuickCmdContent(crypt, e)
		if err != nil {
			// 解密失败必须报错：静默给一个空内容，用户看到的是「我的指令内容没了」，而真正的
			// 原因（换了 master.key / 密文被搬到别的指令上）永远查不出来，而且他一点保存就会
			// 把服务端那份覆盖成空的。
			return nil, fmt.Errorf("decrypt command content of %q (%s) failed: %w", e.Name, e.ID, err)
		}
		out = append(out, QuickCmd{ID: e.ID, Name: e.Name, Content: content, Auto: e.Auto})
	}
	return out, nil
}

// decryptQuickCmdContent 解出一条指令的命令内容。
//
// 走到这里的内容**一定是密文**：明文条目在 migrateQuickCmds 里已经全部被加密重写（迁移不掉时
// loadQuickCmdsFile 直接报错，根本到不了这里）。`content` 是 null / 缺字段时按「没有内容」处理
// ——那是手工编辑过的文件，不是明文。
func decryptQuickCmdContent(crypt *cryptor, e quickCmdStored) (string, error) {
	if e.Content == nil {
		return "", nil
	}
	return crypt.decrypt(quickCmdAADField(e.ID), quickCmdScopeGlobal, e.Content)
}

// loadQuickCmdsFile 读一张表的磁盘形态（调用方持锁），并**顺手做迁移**：文件里只要有旧格式的
// 明文内容，就地加密重写（原子替换）。
//
// 迁移失败（加密不了、或重写不进去）时**报错**，而不是「先把明文内容返回给用户、下次再说」：
// 报告出来的失败是可查的，静默保留明文则违背这条改动的全部意义。文件读不到（还没有指令）
// 按空表处理——那是正常状态，不是错误。
func loadQuickCmdsFile(path string, crypt *cryptor) (quickCmdsFile, error) {
	// 「读 → 迁移 → 重写」整段持锁：迁出来的那份是**读那一刻**的快照，落盘前若别人整表保存过，
	// 写回去就是覆盖（见 quickCmdsMu 的注释）。
	quickCmdsMu.Lock()
	defer quickCmdsMu.Unlock()
	var f quickCmdsFile
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return quickCmdsFile{}, nil
		}
		return f, err
	}
	// 版本 2 是整表信封；更早的版本还允许**裸数组**（`[{...}]`），一并兼容。
	if err := json.Unmarshal(data, &f); err != nil {
		var bare []quickCmdStored
		if err2 := json.Unmarshal(data, &bare); err2 != nil {
			return f, err
		}
		f = quickCmdsFile{Commands: bare}
	}
	if f.Commands == nil {
		f.Commands = []quickCmdStored{}
	}
	migrated, err := migrateQuickCmds(crypt, &f)
	if err != nil {
		return f, err
	}
	if !migrated {
		return f, nil
	}
	if err := writeQuickCmdsFile(path, f); err != nil {
		return f, fmt.Errorf("rewrite %s with encrypted command contents failed: %w", path, err)
	}
	return f, nil
}

// migrateQuickCmds 把表里**旧格式的明文内容**加密成版本 2 的信封，返回是否发生了迁移。
//
// 规则：
//   - id 非法（validQuickCmdID）或在本表里重复 → 重发一个 id，内容跟着重发后的 id 重新加密
//     （见 reassignQuickCmdID），表内第一个出现的 id 保留；
//   - 已经是信封的条目**原样保留**（不重新加密：没必要换密文，也就少一次「解不开」的机会）；
//   - 明文内容加密成功 → 换成信封，内容与格式逐字节不变；
//   - 明文内容为空（空内容存不下来，encrypt 返回 nil）或加密失败 → 换成**加密后的占位文案**
//     （quickCmdMigrationPlaceholder），并把 `auto` 关掉——一句中文提示不该被「点一下就自动
//     回车」当成命令跑到远端 shell 上；
//   - 连占位文案都加密不了（系统随机数不可用这种级别）→ 返回错误、**一个字节都不写**：
//     写不出密文，也不能为了「看起来迁移成功」而留下明文；
//   - 主密钥不可用（crypt == nil）→ 报错、**不返回那份明文**（见 decryptQuickCmdContent 的
//     说明）；磁盘上的文件一个字节都不动，修好主密钥后重试即可。
func migrateQuickCmds(crypt *cryptor, f *quickCmdsFile) (bool, error) {
	if crypt == nil {
		for i := range f.Commands {
			if f.Commands[i].legacy {
				return false, errors.New("master key unavailable, refusing to hand back the plaintext command contents on disk (they stay plaintext until the master key is usable again)")
			}
		}
		return false, nil
	}
	changed := false
	// seen 记录本表里已经出现过的 id：**第一个出现的保留**，后面的重发（与 sanitizeQuickCmds
	// 对请求体的口径一致）。
	seen := make(map[string]bool, len(f.Commands))
	for i := range f.Commands {
		e := &f.Commands[i]
		reissued, err := reassignQuickCmdID(crypt, e, seen)
		if err != nil {
			return false, err
		}
		if reissued {
			changed = true
		}
		seen[e.ID] = true
		if !e.legacy {
			continue
		}
		changed = true
		sec, err := crypt.encrypt(quickCmdAADField(e.ID), quickCmdScopeGlobal, e.plain)
		if err != nil || sec == nil {
			// 这里是**唯一**会让用户丢内容的路径，所以原因必须可查（用户只看到占位文案）。
			if err == nil {
				// encrypt("") 返回 nil：空内容存不下来（正常也不会有——sanitize 就拒绝了空内容，
				// 这里只可能是手工编辑过的文件）。
				logWarn("[quickcmds] plaintext content of %q (%s) is empty, replaced with the placeholder text", e.Name, e.ID)
			} else {
				logWarn("[quickcmds] failed to encrypt plaintext content of %q (%s), replaced with the placeholder text: %v", e.Name, e.ID, err)
			}
			placeholder, perr := crypt.encrypt(quickCmdAADField(e.ID), quickCmdScopeGlobal, quickCmdMigrationPlaceholder)
			if perr != nil {
				return false, fmt.Errorf("encrypt placeholder content failed: %w", perr)
			}
			if placeholder == nil {
				return false, errors.New("encrypt placeholder content returned an empty envelope")
			}
			sec = placeholder
			e.Auto = false
		}
		e.Content, e.plain, e.legacy = sec, "", false
	}
	if changed {
		// 发生过迁移就一定按版本 2 落盘（哪怕这张表原本没写 version）。
		f.Version = 2
	}
	return changed, nil
}

// reassignQuickCmdID 在指令 id 非法（validQuickCmdID）或在本表里已经出现过时，给它换一个
// 全新且本表未用过的 id，返回是否改动了 id。
//
// 为什么必须重发：AAD 是 `harness:v1:quickcmd:<指令 id>:<作用域>`，id 与作用域之间那个冒号
// 没有长度前缀，所以含冒号的 id 会让 (id="x", scope="a:b") 与 (id="x:a", scope="b") 拼出**同一个**
// AAD；而同一张表里两条指令 id 相同时，两条密文互换位置后照样解得开——界面上就是「按钮上写着
// 乙的名字，点下去跑的是甲的命令」。这两种文件只有手工编辑数据目录才造得出来（接口那侧
// sanitizeQuickCmds 会补 id / 去重），但代价只是重发一个 id，没有理由留着。
//
// 已经在信封形态（密文）的条目要**先解开、再用新 id 重新加密**：只换 id 不重新加密，AAD 就
// 对不上了（那条指令立刻变成「解密失败」）。解不开（换了 master.key 等）时原样保留并记日志
// ——那条内容本来就取不回来，此时改 id 只会把「读这张表报错」变成「内容悄悄错位」，反而更糟。
func reassignQuickCmdID(crypt *cryptor, e *quickCmdStored, seen map[string]bool) (bool, error) {
	if validQuickCmdID(e.ID) && !seen[e.ID] {
		return false, nil
	}
	oldID := e.ID
	newID := newQuickCmdID()
	for seen[newID] {
		newID = newQuickCmdID()
	}
	if e.Content != nil {
		plain, err := crypt.decrypt(quickCmdAADField(oldID), quickCmdScopeGlobal, e.Content)
		if err != nil {
			logWarn("[quickcmds] %q has an invalid or duplicate id (%s) but its ciphertext cannot be decrypted, keeping it as is (reading this file will fail): %v", e.Name, oldID, err)
			return false, nil
		}
		sec, err := crypt.encrypt(quickCmdAADField(newID), quickCmdScopeGlobal, plain)
		if err != nil {
			return false, fmt.Errorf("re-encrypt the content of %q under its new id failed: %w", e.Name, err)
		}
		if sec == nil {
			return false, fmt.Errorf("re-encrypt the content of %q under its new id failed: content is empty", e.Name)
		}
		e.Content = sec
	}
	e.ID = newID
	// 只写名称 / 旧 id / 新 id，**不写命令内容**（日志会进系统日志与用户贴出来的诊断信息）。
	logWarn("[quickcmds] %q had an invalid or duplicate id (%s), reissued as %s", e.Name, oldID, newID)
	return true, nil
}

// migrateQuickCmdsAtStartup 在启动时把磁盘上的旧明文内容加密一次。
//
// 为什么不能只在读取时迁移：前端只在**打开快捷指令弹窗**时才请求 `/api/quickcmds`，
// 用户几个月不点它，明文就一直躺在磁盘上。启动时主动迁一次才能兜住「数据目录里不再有明文」。
//
// 失败不拦住启动（返回错误，调用方只记日志）：控制台就是这个 NAS 的带外访问入口，起不来
// 比一条指令还是明文更糟；而用户真的打开弹窗时（或下次启动）读路径会再迁移一次并报出原因。
func migrateQuickCmdsAtStartup(renv *RuntimeEnv, crypt *cryptor) error {
	// 和读取走同一条路（读 → 迁移 → 需要时原子重写），迁移规则只有一处实现。
	// 这条路**不自己持锁**：锁在 loadQuickCmdsFile 里取，启动迁移与请求路径才不会互相死锁。
	_, err := loadQuickCmdsFile(quickCmdsPath(renv), crypt)
	return err
}

// saveQuickCmds 加密整张表并落盘（版本 2）。
//
// 每次保存都重新加密（GCM 每次的 nonce 都是新的）：整表重写本来就会换掉整个文件，没有
// 「保留原密文」的意义，而解密再加密的往返反而多一次失败机会。
//
// 与 loadQuickCmdsFile 共用同一把锁：整表覆盖写必须和「读 → 迁移 → 重写」互相排斥，
// 否则迁移那份过期快照会把刚保存的表整表盖回去（见 quickCmdsMu 的注释）。
func saveQuickCmds(path string, crypt *cryptor, cmds []QuickCmd) error {
	quickCmdsMu.Lock()
	defer quickCmdsMu.Unlock()
	stored := make([]quickCmdStored, 0, len(cmds))
	for _, c := range cmds {
		sec, err := crypt.encrypt(quickCmdAADField(c.ID), quickCmdScopeGlobal, c.Content)
		if err != nil {
			// crypt == nil（主密钥不可用）也会走到这里：**绝不**退化成写明文。
			return fmt.Errorf("encrypt the content of %q failed: %w", c.Name, err)
		}
		if sec == nil {
			// encrypt 对空明文返回 nil，而空内容已经被 sanitize 拒掉；走到这里说明调用顺序被改坏了。
			return fmt.Errorf("the content of %q is empty, refusing to persist", c.Name)
		}
		stored = append(stored, quickCmdStored{ID: c.ID, Name: c.Name, Auto: c.Auto, Content: sec})
	}
	return writeQuickCmdsFile(path, quickCmdsFile{Version: 2, Commands: stored})
}

// writeQuickCmdsFile 原子写整表（同目录唯一临时文件 + chmod 0600 + fsync + rename）。
//
// 权限 0600：文件里虽然是密文，但名称/是否自动回车仍是明文，而且它就在主密钥旁边——
// 没有理由让同机其他用户读它。飞牛存储卷上 O_CREAT 的模式位不可靠，必须显式 chmod 一次。
//
// 临时文件名必须唯一（os.CreateTemp），不能共用一个 <path>.tmp —— 与 config.go 的
// SaveConfig 同一类竞态（整表保存完全可能交错）：后一次 rename 会把前一次写到一半的临时
// 文件改名成正式文件，于是盘上留下一份被截断的 JSON。失败路径负责清掉临时文件。
func writeQuickCmdsFile(path string, f quickCmdsFile) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 失败路径上清掉临时文件（成功时它已被 rename 走）。
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// 先刷盘再 rename：否则断电/崩溃后可能拿到「文件在但内容为空」的表
	// （读路径会解析失败并把用户的快捷指令当成空列表）。
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
