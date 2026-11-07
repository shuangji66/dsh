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
