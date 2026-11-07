package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTestConfig 临时替换全局配置（saveConfig / GetConfig 是包级状态）。
func withTestConfig(t *testing.T, c AppConfig) {
	t.Helper()
	prev := GetConfig()
	initConfig(&c)
	t.Cleanup(func() { initConfig(&prev) })
}

func dshEnvTestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), dshEnvFileName)
}

// 变量名明文落盘（排查用），变量值必须是密文。
func TestDshEnvNamesPlaintextValuesEncrypted(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	vars := []DshEnvVar{
		{Name: "OPENAI_API_KEY", Value: "sk-super-secret"},
		{Name: "TZ", Value: "Asia/Shanghai"},
	}
	if err := saveDshEnv(path, crypt, vars); err != nil {
		t.Fatalf("saveDshEnv: %v", err)
	}
	raw := readFileStr(t, path)
	if !strings.Contains(raw, `"version": 1`) {
		t.Fatalf("落盘版本不是 1：%s", raw)
	}
	for _, name := range []string{"OPENAI_API_KEY", "TZ"} {
		if !strings.Contains(raw, name) {
			t.Fatalf("磁盘上应能看到变量名 %q：%s", name, raw)
		}
	}
	for _, secret := range []string{"sk-super-secret", "Asia/Shanghai"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("磁盘上出现了明文变量值 %q：%s", secret, raw)
		}
	}
	got, err := loadDshEnv(path, crypt)
	if err != nil {
		t.Fatalf("loadDshEnv: %v", err)
	}
	if len(got) != len(vars) || got[0] != vars[0] || got[1] != vars[1] {
		t.Fatalf("loadDshEnv = %#v, want %#v", got, vars)
	}
	// 换一把密钥必须解不开，而不是返回空表。
	if _, err := loadDshEnv(path, testCryptor(t)); err == nil {
		t.Fatalf("换主密钥后仍能解密，密文没有绑定密钥")
	}
}

// 空值不加密（留 null），但变量本身要保留。
func TestDshEnvEmptyValueRoundTrip(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	if err := saveDshEnv(path, crypt, []DshEnvVar{{Name: "EMPTY", Value: ""}}); err != nil {
		t.Fatal(err)
	}
	raw := readFileStr(t, path)
	if !strings.Contains(raw, `"name": "EMPTY"`) || !strings.Contains(raw, `"value": null`) {
		t.Fatalf("空值的落盘形态不对：%s", raw)
	}
	got, err := loadDshEnv(path, crypt)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (DshEnvVar{Name: "EMPTY", Value: ""}) {
		t.Fatalf("round trip = %#v", got)
	}
}

// 未知版本必须报错，而不是当空表覆盖掉。
func TestDshEnvRejectsUnknownVersion(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	data, err := json.MarshalIndent(dshEnvFile{Version: 99, Variables: []dshEnvStored{{Name: "A"}}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDshEnv(path, crypt); err == nil {
		t.Fatalf("版本 99 应报错")
	}
}

// AAD 绑定变量名：同表内把两条密文互换位置必须解密失败。
func TestDshEnvCiphertextBoundToName(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	if err := saveDshEnv(path, crypt, []DshEnvVar{
		{Name: "A", Value: "one"},
		{Name: "B", Value: "two"},
	}); err != nil {
		t.Fatal(err)
	}
	var f dshEnvFile
	if err := json.Unmarshal([]byte(readFileStr(t, path)), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Variables) != 2 {
		t.Fatalf("variables = %#v", f.Variables)
	}
	f.Variables[0].Value, f.Variables[1].Value = f.Variables[1].Value, f.Variables[0].Value
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDshEnv(path, crypt); err == nil {
		t.Fatalf("交换密文后仍能解密，AAD 没有绑定变量名")
	}
}

func TestDshEnvSaveEmptyRemovesFile(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	if err := saveDshEnv(path, crypt, []DshEnvVar{{Name: "FOO", Value: "bar"}}); err != nil {
		t.Fatal(err)
	}
	if err := saveDshEnv(path, crypt, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("空列表应删除 %s，stat err=%v", path, err)
	}
	got, err := loadDshEnv(path, crypt)
	if err != nil || len(got) != 0 {
		t.Fatalf("文件删除后应读成空列表：%#v, %v", got, err)
	}
}

func TestNormalizeDshEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		in      []DshEnvVar
		want    []DshEnvVar
		wantErr string
	}{
		{name: "valid", in: []DshEnvVar{{Name: " FOO ", Value: " bar "}}, want: []DshEnvVar{{Name: "FOO", Value: " bar "}}},
		{name: "empty name", in: []DshEnvVar{{Name: "  ", Value: "x"}}, wantErr: "err_dsh_env_name_required"},
		{name: "bad name", in: []DshEnvVar{{Name: "1FOO", Value: "x"}}, wantErr: "err_dsh_env_name_invalid"},
		{name: "bad char", in: []DshEnvVar{{Name: "FOO-BAR", Value: "x"}}, wantErr: "err_dsh_env_name_invalid"},
		{name: "reserved", in: []DshEnvVar{{Name: "PATH", Value: "x"}}, wantErr: "err_dsh_env_name_reserved"},
		{name: "reserved case", in: []DshEnvVar{{Name: "dsh_home", Value: "x"}}, wantErr: "err_dsh_env_name_reserved"},
		// 前缀保留：DSH_SUBPROCESS_* 是 dsh 自己下发给工具 shell 的通道。
		{name: "reserved prefix", in: []DshEnvVar{{Name: "DSH_SUBPROCESS_FOO", Value: "x"}}, wantErr: "err_dsh_env_name_reserved"},
		{name: "reserved prefix case", in: []DshEnvVar{{Name: "dsh_subprocess_foo", Value: "x"}}, wantErr: "err_dsh_env_name_reserved"},
		// 平台/控制台的部署事实：改它会让插件与控制台看到两套目录/应用名。
		{name: "reserved trim", in: []DshEnvVar{{Name: "TRIM_APPNAME", Value: "x"}}, wantErr: "err_dsh_env_name_reserved"},
		{name: "reserved trim case", in: []DshEnvVar{{Name: "trim_pkgvar", Value: "x"}}, wantErr: "err_dsh_env_name_reserved"},
		{name: "reserved data dir", in: []DshEnvVar{{Name: "HARNESS_DATA_DIR", Value: "x"}}, wantErr: "err_dsh_env_name_reserved"},
		{name: "duplicate", in: []DshEnvVar{{Name: "FOO", Value: "x"}, {Name: "FOO", Value: "y"}}, wantErr: "err_dsh_env_duplicate"},
		{name: "nul", in: []DshEnvVar{{Name: "FOO", Value: "a\x00b"}}, wantErr: "err_dsh_env_value_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeDshEnvVars(tt.in)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error %s, got %#v", tt.wantErr, got)
				}
				if m, ok := uiMsgOf(err); !ok || m.Code != tt.wantErr {
					t.Fatalf("error code = %#v, want %s (err=%v)", m, tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeDshEnvVars: %v", err)
			}
			if len(got) != len(tt.want) || got[0] != tt.want[0] {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}

	if _, err := normalizeDshEnvVars(make([]DshEnvVar, maxDshEnvVars+1)); err == nil {
		t.Fatalf("超过条数上限应报错")
	}
	long := strings.Repeat("x", maxDshEnvValueLen+1)
	if _, err := normalizeDshEnvVars([]DshEnvVar{{Name: "FOO", Value: long}}); err == nil {
		t.Fatalf("超长值应报错")
	}
}

// 开关打开时注入；关闭时不注入（列表仍可保存，只是不生效）。
func TestBuildEnvInjectsCustomDshEnv(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	if err := saveDshEnv(path, crypt, []DshEnvVar{
		{Name: "FOO", Value: "bar"},
		{Name: "TZ", Value: "UTC"},
	}); err != nil {
		t.Fatal(err)
	}

	m := newTestDshManager(t.TempDir(), "")
	m.renv.DshEnvFile = path
	m.crypt = crypt

	withTestConfig(t, AppConfig{DshEnvEnabled: true, DshMemAuto: true})
	env := m.buildEnv()
	if got, ok := envValue(env, "FOO"); !ok || got != "bar" {
		t.Fatalf("FOO = %q (存在=%v), want bar", got, ok)
	}
	if got, ok := envValue(env, "TZ"); !ok || got != "UTC" {
		t.Fatalf("TZ = %q (存在=%v), want UTC", got, ok)
	}
	// 托管变量仍然最后生效。
	if got, ok := envValue(env, "SHELL"); !ok || got != "/bin/bash" {
		t.Fatalf("SHELL = %q (存在=%v), want /bin/bash", got, ok)
	}

	withTestConfig(t, AppConfig{DshEnvEnabled: false, DshMemAuto: true})
	if got, ok := envValue(m.buildEnv(), "FOO"); ok {
		t.Fatalf("开关关闭时不应注入 FOO（得到 %q）", got)
	}
}

// 手工改过的文件即便塞了保留名，buildEnv 也必须忽略它，不能覆盖控制台管理的值。
func TestBuildEnvIgnoresReservedDshEnvName(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	if err := saveDshEnv(path, crypt, []DshEnvVar{
		{Name: "DSH_HOME", Value: "/tmp/evil"},
		{Name: "FOO", Value: "bar"},
	}); err != nil {
		t.Fatal(err)
	}
	m := newTestDshManager(t.TempDir(), "")
	m.renv.DshEnvFile = path
	m.crypt = crypt
	withTestConfig(t, AppConfig{DshEnvEnabled: true, DshMemAuto: true})

	env := m.buildEnv()
	if got, ok := envValue(env, "DSH_HOME"); ok && got == "/tmp/evil" {
		t.Fatalf("保留变量 DSH_HOME 被文件覆盖了")
	}
	if got, ok := envValue(env, "FOO"); !ok || got != "bar" {
		t.Fatalf("普通变量 FOO 应正常注入，得到 %q (存在=%v)", got, ok)
	}
}

// 文件损坏时 buildEnv 不能崩、不能注入半个表，只记一条 WARN。
func TestBuildEnvSkipsCorruptDshEnvFile(t *testing.T) {
	path := dshEnvTestPath(t)
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTestDshManager(t.TempDir(), "")
	m.renv.DshEnvFile = path
	m.crypt = testCryptor(t)
	withTestConfig(t, AppConfig{DshEnvEnabled: true, DshMemAuto: true})

	env := m.buildEnv()
	if _, ok := envValue(env, "FOO"); ok {
		t.Fatalf("损坏文件不应注入任何变量")
	}
}

func TestDshEnvHandlers(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	m := &AdminMux{renv: &RuntimeEnv{DshEnvFile: path}, crypt: crypt}

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/dsh-env", strings.NewReader(body))
		rec := httptest.NewRecorder()
		m.handleSaveDshEnv(rec, req)
		return rec
	}

	rec := post(`{"variables":[{"name":"FOO","value":"bar"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var saved struct {
		OK        bool        `json:"ok"`
		Variables []DshEnvVar `json:"variables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.OK || len(saved.Variables) != 1 || saved.Variables[0].Name != "FOO" {
		t.Fatalf("POST response = %s", rec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/dsh-env", nil)
	getRec := httptest.NewRecorder()
	m.handleGetDshEnv(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", getRec.Code, getRec.Body.String())
	}
	var payload struct {
		Variables        []DshEnvVar `json:"variables"`
		Reserved         []string    `json:"reserved"`
		ReservedPrefixes []string    `json:"reservedPrefixes"`
		Max              int         `json:"max"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Variables) != 1 || payload.Variables[0].Value != "bar" {
		t.Fatalf("GET variables = %#v", payload.Variables)
	}
	if len(payload.Reserved) == 0 || payload.Max != maxDshEnvVars {
		t.Fatalf("GET 缺少保留名单/上限：%s", getRec.Body.String())
	}

	rec = post(`{"variables":[{"name":"PATH","value":"/evil"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("保留名应 400，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Code != "err_dsh_env_name_reserved" {
		t.Fatalf("code = %q, want err_dsh_env_name_reserved", errBody.Code)
	}

	// 损坏文件：GET 必须报错（而不是返回空表让用户覆盖掉服务端那份）。
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	getRec = httptest.NewRecorder()
	m.handleGetDshEnv(getRec, getReq)
	if getRec.Code != http.StatusInternalServerError {
		t.Fatalf("损坏文件 GET status = %d, want 500", getRec.Code)
	}
}

// 保存接口对「没带 variables 字段」必须报错：只有**显式**传空数组才是「清空整表」，
// 否则一个 `{}` 就能把用户的变量表删掉（saveDshEnv 对空表会直接删文件）。
func TestSaveDshEnvRequiresVariablesField(t *testing.T) {
	crypt := testCryptor(t)
	path := dshEnvTestPath(t)
	m := &AdminMux{renv: &RuntimeEnv{DshEnvFile: path}, crypt: crypt}

	// 先放一份真实数据，确认它不会被下面的畸形请求删掉。
	if err := saveDshEnv(path, crypt, []DshEnvVar{{Name: "FOO", Value: "bar"}}); err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{`{}`, `{"variables":null}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/dsh-env", strings.NewReader(body))
		m.handleSaveDshEnv(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400", body, rec.Code)
		}
		var errBody struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
			t.Fatal(err)
		}
		if errBody.Code != "err_dsh_env_variables_missing" {
			t.Fatalf("body %s code = %q, want err_dsh_env_variables_missing", body, errBody.Code)
		}
	}
	if vars, err := loadDshEnv(path, crypt); err != nil || len(vars) != 1 {
		t.Fatalf("畸形请求后变量表被改动：%#v, %v", vars, err)
	}

	// 空数组才是「清空」。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/dsh-env", strings.NewReader(`{"variables":[]}`))
	m.handleSaveDshEnv(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("空数组 status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("空数组应删掉变量文件，stat err=%v", err)
	}
}

// 启动时的临时文件清扫：只删 <文件名>.*.tmp，不碰最终文件，也不碰别的临时文件。
func TestSweepDshEnvTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, dshEnvFileName)
	stale := []string{
		filepath.Join(dir, dshEnvFileName+".123.tmp"),
		filepath.Join(dir, dshEnvFileName+".456.tmp"),
	}
	keep := filepath.Join(dir, "quickcmds.json.789.tmp")
	for _, p := range append([]string{path, keep}, stale...) {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sweepDshEnvTemps(path)

	for _, p := range stale {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("残留临时文件未被清掉：%s", p)
		}
	}
	for _, p := range []string{path, keep} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("不该删的文件被删了：%s (%v)", p, err)
		}
	}

	// 目录不存在 / 没有残留时不能报错（启动路径上无条件调用）。
	sweepDshEnvTemps(filepath.Join(t.TempDir(), "missing", dshEnvFileName))
}

// 设置页把「自定义环境变量」从关切到开时要先读一遍变量文件：读不出来就整次拒绝保存，
// 否则开关会显示为「已开启」，而 dsh 启动时静默跳过全部变量。
func TestSaveSettingsRejectsEnableWhenEnvFileUnreadable(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })

	cfg := defaultConfig()
	cfg.DshMemLimit = 2048
	cfg.DshMemAuto = true
	cfg.DshEnvEnabled = false
	initConfig(&cfg)

	envPath := dshEnvTestPath(t)
	renv := &RuntimeEnv{
		ConfigFile: filepath.Join(t.TempDir(), "config.json"),
		DshEnvFile: envPath,
	}
	m := &AdminMux{renv: renv, dsh: newTestDshManager(t.TempDir(), ""), boot: newBootState(), crypt: testCryptor(t)}

	save := func(mutate func(*AppConfig)) (int, string) {
		t.Helper()
		next := GetConfig()
		mutate(&next)
		body, err := json.Marshal(map[string]interface{}{"config": next})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		m.handleSaveSettings(rec, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(string(body))))
		var out struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out.Code
	}

	// 文件损坏：拒绝保存，开关保持关闭。
	if err := os.WriteFile(envPath, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, errCode := save(func(c *AppConfig) { c.DshEnvEnabled = true })
	if code != http.StatusInternalServerError || errCode != "err_dsh_env_load_failed" {
		t.Fatalf("损坏文件的保存 = %d/%q, want 500/err_dsh_env_load_failed", code, errCode)
	}
	if GetConfig().DshEnvEnabled {
		t.Fatalf("被拒绝后开关不应变成开启")
	}

	// 文件可读：允许打开开关，并落盘。
	if err := os.Remove(envPath); err != nil {
		t.Fatal(err)
	}
	if code, _ := save(func(c *AppConfig) { c.DshEnvEnabled = true }); code != http.StatusOK {
		t.Fatalf("文件缺失（= 空表）时保存状态 = %d, want 200", code)
	}
	if !GetConfig().DshEnvEnabled {
		t.Fatalf("保存成功后开关应为开启")
	}
}
