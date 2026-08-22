package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// AppConfig from env: TRIM_APPNAME, TRIM_API_TOKEN

// TrimGatewayConfig locates the fnOS open-gateway unix socket.
const trimSocket = "/var/run/trim_open_gateway_apiscope.socket"
const trimEndpoint = "http://localhost/api/v1/trimapp"

type trimRequest struct {
	ReqID   string      `json:"reqId"`
	Req     string      `json:"req"`
	AppName string      `json:"appName"`
	Data    interface{} `json:"data"`
}

type trimEnvelopeOut struct {
	ReqID string      `json:"reqId"`
	Code  int         `json:"code"`
	Msg   string      `json:"msg"`
	Data  interface{} `json:"data"`
}

// FnosClient is a thin client over the fnOS open-gateway API.
type FnosClient struct {
	AppName string
	Token   string
}

func NewFnosClient(renv *RuntimeEnv) *FnosClient {
	return &FnosClient{AppName: renv.TRIMAppName, Token: renv.TRIMApiToken}
}

func (c *FnosClient) call(req string, data interface{}) (int, string, interface{}, error) {
	if c.Token == "" {
		return 1, "TRIM_API_TOKEN 不可用", nil, fmt.Errorf("TRIM_API_TOKEN not set")
	}
	body, _ := json.Marshal(trimRequest{
		ReqID:   fmt.Sprintf("h%d", time.Now().UnixNano()),
		Req:     req,
		AppName: c.AppName,
		Data:    data,
	})
	httpc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", trimSocket)
			},
		},
		Timeout: 10 * time.Second,
	}
	hreq, err := http.NewRequest("POST", trimEndpoint, bytes.NewReader(body))
	if err != nil {
		return 1, "构造 fnOS 请求失败", nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := httpc.Do(hreq)
	if err != nil {
		return 1, "调用 fnOS 网关失败: " + err.Error(), nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env trimEnvelopeOut
	if err := json.Unmarshal(raw, &env); err != nil {
		return 1, "解析 fnOS 响应失败", nil, err
	}
	return env.Code, env.Msg, env.Data, nil
}

// GetPlatformConfig reads the fnOS system language and version.
func (c *FnosClient) GetPlatformConfig() (map[string]interface{}, error) {
	code, msg, data, err := c.call("trim.system.getPlatformConfig", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("fnOS code=%d msg=%s", code, msg)
	}
	if m, ok := data.(map[string]interface{}); ok {
		return m, nil
	}
	return map[string]interface{}{}, nil
}


// GetUserAccessibleFolders 查询当前用户的已授权目录
// 需要传入 uid（从统一网关获取的当前用户ID）
func (c *FnosClient) GetUserAccessibleFolders(uid int) ([]string, string, error) {
    code, msg, data, err := c.call("trim.file.getUserAccessibleFolders", map[string]interface{}{
        "uid": uid,
    })
    if err != nil {
        return nil, "", err
    }
    if code != 0 {
        return nil, msg, fmt.Errorf("fnOS code=%d msg=%s", code, msg)
    }
    var res struct {
        Paths []string `json:"paths"`
    }
    b, _ := json.Marshal(data)
    _ = json.Unmarshal(b, &res)
    return res.Paths, msg, nil
}

// DelUserAccessibleFolder 删除用户的授权目录
func (c *FnosClient) DelUserAccessibleFolder(uid int, path string) (bool, string, error) {
    code, msg, _, err := c.call("trim.file.delUserAccessibleFolder", map[string]interface{}{
        "uid":  uid,
        "path": path,
    })
    if err != nil {
        return false, "", err
    }
    return code == 0, msg, nil
}