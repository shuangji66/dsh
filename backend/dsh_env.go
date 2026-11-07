package main

// dsh_env.go —— 设置页「运行环境 → 自定义环境变量」的持久化与校验。
//
// 磁盘形态是统一数据目录下的 dsh-env.json：**变量名明文、变量值逐条一个
// AES-256-GCM 信封**（见 secret.go）。环境变量里常放 API key / token，拿到磁盘快照
// 或备份的人不该顺手读到值；但名字是给人排查用的标签，加密它只会让文件没法看。
// 接口收发的始终是明文（与 quickcmds 一致），加密只发生在落盘这一层。
//
// 生效路径：AppConfig.DshEnvEnabled 打开时，DshManager.buildEnv 在 dsh 启动时
// 读这个文件并注入；保存配置后仍需重启 dsh 才生效。变量名有一份保留名单
// （dshEnvReservedNames），保存接口拒绝它们 —— 否则用户可以覆盖 PATH / DSH_HOME
// 这类控制台自己管理的变量。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// dshEnvFileName 是数据目录下的文件名（与 config.json / quickcmds.json 同级）。
	dshEnvFileName = "dsh-env.json"
	// dshEnvFileVersion 是磁盘格式版本。信封本身另有 secretVersion。
	// 变量名明文、变量值逐条加密。
	dshEnvFileVersion = 1
	// dshEnvScope 绑定密文用途：把别处的密文剪到这里会解密失败。
	dshEnvScope = "global"
	// maxDshEnvVars 是变量条数上限（整表提交，正常只有几条）。
	maxDshEnvVars = 64
	// maxDshEnvNameLen / maxDshEnvValueLen 是单条变量的长度上限。
	maxDshEnvNameLen  = 128
	maxDshEnvValueLen = 8 << 10 // 8 KiB
	// maxDshEnvBody 是保存请求体的上限（64 条 × 8 KiB 留足 JSON 余量）。
	maxDshEnvBody = 2 << 20 // 2 MB
)

// dshEnvNameRe 与 dsh 的凭据引用同一套 POSIX 标识符规则
// （见 @deepseek-ai/dsh-credentials 的 REF_PATTERN）。
var dshEnvNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// DshEnvVar 是一条自定义环境变量（接口收发明文；落盘时只有 Value 进密文信封）。
type DshEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// dshEnvStored 是一条变量在磁盘上的形态：名字明文，值是一个密文信封。
// 空值不加密（encrypt 对空串返回 nil），读回时按空串处理。
type dshEnvStored struct {
	Name  string          `json:"name"`
	Value *secretEnvelope `json:"value"`
}

// dshEnvFile 是磁盘上的整表：变量名明文，变量值逐条一个信封。
type dshEnvFile struct {
	Version   int            `json:"version"`
	Variables []dshEnvStored `json:"variables,omitempty"`
}

// dshEnvAADField 把变量名绑进密文的 AAD：同表内两条变量的密文互换位置会直接解密失败，
// 改名而不重新加密也会失败（保存时整表重新加密，所以正常流程不会出现）。
func dshEnvAADField(name string) string { return "dsh-env:" + name }

// dshEnvReservedNames 是控制台自己管理、或会改变 dsh 启动/安全边界的变量名。
//
// 比较时统一 ToUpper（Windows 环境名大小写不敏感；POSIX 上也不该让 `Path` 绕过）。
// 名单刻意包含 dsh 自己的 bootstrap 名（DSH_HOME、代理、NODE_OPTIONS 等）：
// 这些值由控制台按部署事实计算，用户改它只会让 dsh 与控制台看到两套目录/端口。
var dshEnvReservedNames = map[string]struct{}{
	// 控制台按启动事实计算
	"PATH": {}, "HOME": {}, "SHELL": {}, "PNPM_HOME": {},
	// 平台 / 控制台的部署事实（TRIM_*、数据目录、主密钥）。buildEnv 从 os.Environ() 起手，
	// 这些值本来就在 dsh 环境里；允许用户改写只会让「控制台看到的」和「插件看到的」变成
	// 两套 —— 例如增强插件的宿主侧按 process.env.TRIM_APPNAME 认应用名
	// （见 dsh-fnos-advanced 的 host/routes.ts），填错的表现是目录授权/工作区请求全线失败，
	// 而设置页没有任何提示。
	"TRIM_APPNAME": {}, "TRIM_PKGVAR": {}, "TRIM_APPDEST": {},
	"HARNESS_DATA_DIR": {}, "HARNESS_MASTER_KEY": {},
	// node 运行时（乱填会让 dsh 直接起不来）
	"NODE_OPTIONS": {}, "NODE_PATH": {}, "NODE_TLS_REJECT_UNAUTHORIZED": {},
	// dsh 数据根 / 身份
	"DSH_HOME": {}, "DSH_AGENTS_HOME": {}, "DSH_WEB_URL": {},
	// dsh 内部事实（由 dsh 自己下发给工具 shell，不是输入）
	"DSH_PROFILE": {}, "DSH_PROFILE_DIR": {}, "DSH_SESSION_ID": {}, "DSH_SHELL": {},
	"DSH_SUBPROCESS_RUNNER": {}, "DSH_SUBPROCESS_CONTROL": {},
	"DSH_PTC_RUNTIME_NODE": {}, "DSH_SPEECH_TOKEN": {}, "DSH_SNAPSHOT": {},
	// 代理由「代理dsh」开关独占（关闭时 buildEnv 会剥掉这些名字）
	"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {}, "NO_PROXY": {},
	// 进程/语言运行时的加载钩子
	"LD_PRELOAD": {}, "LD_LIBRARY_PATH": {}, "LD_AUDIT": {},
	"BASH_ENV": {}, "ENV": {}, "SHELLOPTS": {}, "BASHOPTS": {},
	"PYTHONPATH": {}, "PYTHONSTARTUP": {}, "PYTHONHOME": {},
	"PERL5OPT": {}, "PERL5LIB": {}, "RUBYOPT": {}, "RUBYLIB": {},
	"JAVA_TOOL_OPTIONS": {}, "_JAVA_OPTIONS": {}, "JDK_JAVA_OPTIONS": {},
}

// dshEnvReservedPrefixes 是保留前缀：成组的内部变量按前缀挡，免得 dsh 每加一个新名字
// 就要回来补一条。
var dshEnvReservedPrefixes = []string{
	// dsh 自己下发给工具 shell 的子进程通道（RUNNER / CONTROL 已逐条列出，这里覆盖将来
	// 可能出现的同族名字）。AGENTS.md 一直写着「DSH_SUBPROCESS_*」，代码此前是逐条挡的。
	"DSH_SUBPROCESS_",
}

// dshEnvPath resolves the persistence file path: 已解析的运行时路径优先，
// 否则退回统一数据目录下的 dsh-env.json（与 quickCmdsPath 同一套兜底写法）。
func dshEnvPath(renv *RuntimeEnv) string {
	if renv != nil && renv.DshEnvFile != "" {
		return renv.DshEnvFile
	}
	return dataPath(dshEnvFileName)
}

// isDshEnvReserved 报告变量名是否属于保留名单/前缀。
func isDshEnvReserved(name string) bool {
	upper := strings.ToUpper(name)
	if _, ok := dshEnvReservedNames[upper]; ok {
		return true
	}
	for _, prefix := range dshEnvReservedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// dshEnvReservedSnapshot 返回给前端的保留名单（排序后，便于校验与稳定展示）。
func dshEnvReservedSnapshot() (names []string, prefixes []string) {
	names = make([]string, 0, len(dshEnvReservedNames))
	for n := range dshEnvReservedNames {
		names = append(names, n)
	}
	sort.Strings(names)
	prefixes = make([]string, 0, len(dshEnvReservedPrefixes))
	prefixes = append(prefixes, dshEnvReservedPrefixes...)
	return names, prefixes
}

// normalizeDshEnvVars 校验并规整整张表：trim 变量名、拒绝非法/保留/重复名与超长值。
//
// 变量值**原样保留**（不 trim）：前导/行尾空格可能就是值的一部分（token、带空格的
// 路径），与 quickcmds「内容原样提交」同一口径。
func normalizeDshEnvVars(in []DshEnvVar) ([]DshEnvVar, error) {
	if len(in) > maxDshEnvVars {
		return nil, uiErr("err_dsh_env_too_many", "自定义环境变量最多 %s 个", "max", strconv.Itoa(maxDshEnvVars))
	}
	out := make([]DshEnvVar, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, uiErr("err_dsh_env_name_required", "变量名不能为空")
		}
		if len(name) > maxDshEnvNameLen || !dshEnvNameRe.MatchString(name) {
			return nil, uiErr("err_dsh_env_name_invalid",
				"变量名 %s 不合法：只能包含字母、数字、下划线，且不能以数字开头", "name", name)
		}
		if isDshEnvReserved(name) {
			return nil, uiErr("err_dsh_env_name_reserved",
				"变量名 %s 为保留变量，不能自定义", "name", name)
		}
		if _, ok := seen[name]; ok {
			return nil, uiErr("err_dsh_env_duplicate", "变量名 %s 重复", "name", name)
		}
		seen[name] = struct{}{}
		if strings.IndexByte(item.Value, 0) >= 0 {
			return nil, uiErr("err_dsh_env_value_invalid", "变量 %s 的值包含非法字符", "name", name)
		}
		if len(item.Value) > maxDshEnvValueLen {
			return nil, uiErr("err_dsh_env_value_too_long",
				"变量 %s 的值过长（最多 %s 字节）", "name", name, "max", strconv.Itoa(maxDshEnvValueLen))
		}
		out = append(out, DshEnvVar{Name: name, Value: item.Value})
	}
	return out, nil
}

// loadDshEnv 读取并解密整张表。文件不存在按「还没有变量」处理（空列表，不是错误）；
// 存在但解不开必须报错 —— 静默返回空表会让用户以为变量被删了，一点保存就把服务端
// 那份覆盖掉。
func loadDshEnv(path string, crypt *cryptor) ([]DshEnvVar, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []DshEnvVar{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f dshEnvFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s failed: %w", path, err)
	}
	if f.Version != 0 && f.Version != dshEnvFileVersion {
		return nil, fmt.Errorf("%s has unsupported version %d", path, f.Version)
	}
	if crypt == nil {
		return nil, errors.New("master key unavailable")
	}
	out := make([]DshEnvVar, 0, len(f.Variables))
	for _, item := range f.Variables {
		value := ""
		if item.Value != nil {
			value, err = crypt.decrypt(dshEnvAADField(item.Name), dshEnvScope, item.Value)
			if err != nil {
				return nil, fmt.Errorf("decrypt variable %q in %s failed: %w", item.Name, path, err)
			}
		}
		out = append(out, DshEnvVar{Name: item.Name, Value: value})
	}
	return out, nil
}

// saveDshEnv 加密整张表并原子落盘。空表直接删除文件（没有变量就不该留下旧密文）。
func saveDshEnv(path string, crypt *cryptor, vars []DshEnvVar) error {
	if len(vars) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if crypt == nil {
		return errors.New("master key unavailable")
	}
	stored := make([]dshEnvStored, 0, len(vars))
	for _, v := range vars {
		// 变量名明文落盘；只有值进信封。空值没有要保护的内容，留 null。
		value, err := crypt.encrypt(dshEnvAADField(v.Name), dshEnvScope, v.Value)
		if err != nil {
			return err
		}
		stored = append(stored, dshEnvStored{Name: v.Name, Value: value})
	}
	data, err := json.MarshalIndent(dshEnvFile{Version: dshEnvFileVersion, Variables: stored}, "", "  ")
	if err != nil {
		return err
	}
	return writeDshEnvFile(path, data)
}

// writeDshEnvFile 先写唯一临时文件再 rename，避免断电/崩溃留下半个文件。
// 权限 0600：文件内容是密文，但也没有理由让同组用户读到。
func writeDshEnvFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	renamed = true
	return nil
}

// sweepDshEnvTemps 删除「写一半就断电」留下的临时文件（<文件名>.<随机串>.tmp）。
//
// writeDshEnvFile 用唯一临时文件 + rename 落盘（两个标签页同时保存不会互相踩），代价是
// 进程被杀/掉电时会留下临时文件。它不会被任何读取路径碰到（loadDshEnv 只认最终文件名），
// 但会在数据目录里长期堆积，内容还是同一条变量的密文。由 main 在启动时清一次 ——
// **只在启动时做**：运行期清扫可能删掉另一个并发保存正在写的那个临时文件。
func sweepDshEnvTemps(path string) {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	// 用前缀+后缀匹配而不是 filepath.Glob：数据目录的实际路径里可能带 `[`、`*` 这类
	// glob 元字符（如某些挂载点），拼进模式会变成另一个模式或直接报错。
	prefix := filepath.Base(path) + "."
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) || !strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
			removed++
		}
	}
	// 只在真删了东西时记一行：这是「上次没写干净」的状态变化，不是每次启动的例行日志。
	if removed > 0 {
		logInfo("[dsh-env] removed %d stale temp files", removed)
	}
}

// handleGetDshEnv 返回明文列表 + 保留名单（前端据此做即时校验）。
func (m *AdminMux) handleGetDshEnv(w http.ResponseWriter, r *http.Request) {
	path := dshEnvPath(m.renv)
	vars, err := loadDshEnv(path, m.crypt)
	if err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_dsh_env_load_failed",
			"加载自定义环境变量失败：%s", "detail", err.Error())
		return
	}
	reserved, prefixes := dshEnvReservedSnapshot()
	writeJSON(w, map[string]interface{}{
		"ok":               true,
		"path":             path,
		"variables":        vars,
		"reserved":         reserved,
		"reservedPrefixes": prefixes,
		"max":              maxDshEnvVars,
	})
}

// handleSaveDshEnv 校验并整表保存自定义环境变量。
func (m *AdminMux) handleSaveDshEnv(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDshEnvBody)
	// 指针：**「没带 variables 字段」与「传了空数组」是两回事**。前者是调用方写错了，
	// 后者才是「清空整张表」。都当成 nil 的话，一个 `{}` 就能把用户的变量表删掉
	// （saveDshEnv 对空表会直接删文件），没有任何回滚余地。
	var req struct {
		Variables *[]DshEnvVar `json:"variables"`
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
	if req.Variables == nil {
		writeErrU(w, http.StatusBadRequest, "err_dsh_env_variables_missing",
			"请求缺少 variables 字段（清空变量表请传空数组）")
		return
	}
	vars, err := normalizeDshEnvVars(*req.Variables)
	if err != nil {
		writeErrFrom(w, err, http.StatusBadRequest)
		return
	}
	path := dshEnvPath(m.renv)
	if err := saveDshEnv(path, m.crypt, vars); err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_dsh_env_save_failed",
			"保存自定义环境变量失败：%s", "detail", err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "variables": vars})
}
