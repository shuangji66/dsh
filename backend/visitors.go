package main

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// Visitor 的两种来源：
//   - port：端口访问。请求带 harness 会话 cookie，`ID` 就是该 cookie 值（因此
//     同一 IP 上的多个客户端各占一条），`ExpiresAt` 是登录有效期，可在列表里注销。
//   - gateway：飞牛网关访问（平台网关注入身份头，见 proxy.go 的 gatewayLine）。
//     网关请求没有 harness 会话 cookie，`ID` 按「飞牛用户 + 客户端 IP」构造：同一个人
//     从不同环境（网络）访问飞牛时 IP 不同，各占一条记录，不会被合并刷新成同一条；
//     没有登录有效期，也不支持注销。
const (
	visitorSourcePort    = "port"
	visitorSourceGateway = "gateway"
	// gatewayVisitorIDPrefix 是网关访客 ID 的前缀：登录列表要据此把这些条目与
	// 会话令牌区分开（注销、过期清理的语义都不同）。
	gatewayVisitorIDPrefix = "gateway:uid:"
)

// gatewayVisitorIdleTTL 是网关访问条目的闲置清除时长。网关请求没有 harness 会话，
// 也就没有「登录有效期」可以清理（ExpiresAt 恒为零值），若不设闲置上限，登录列表
// 会长期堆积早已离开的用户。超时只删记录、不涉及任何凭据（网关访问本就没有凭据）。
const gatewayVisitorIdleTTL = 24 * time.Hour

// Visitor represents one identity against the reverse proxy. It is keyed by the
// session token (port access) or the fnOS user + client IP (gateway access) so
// that multiple clients each appear as their own card. The `ID` is that stable
// unique key; `IP` is the (possibly shared) source address.
type Visitor struct {
	ID         string    `json:"id"`
	IP         string    `json:"ip"`
	Source     string    `json:"source"`             // port | gateway
	Username   string    `json:"username,omitempty"` // 仅网关访问：飞牛 OS 用户名
	Admin      bool      `json:"admin,omitempty"`    // 仅网关访问：X-Trim-Isadmin
	LastAccess time.Time `json:"lastAccess"`
	ExpiresAt  time.Time `json:"expiresAt"` // 登录有效期至（网关访问为零值）
}

// isGatewayVisitorID reports whether a visitor id belongs to gateway access.
func isGatewayVisitorID(id string) bool {
	return strings.HasPrefix(id, gatewayVisitorIDPrefix)
}

// gatewayVisitorID builds the stable visitor id for a fnOS user reached from a
// given client address. ip 为空（网关既没带转发头、来源也不是 TCP）时退化为只按
// 用户标识，此时同一用户的不同环境无法区分、会合并为一条。
func gatewayVisitorID(uid int, ip string) string {
	id := gatewayVisitorIDPrefix + strconv.Itoa(uid)
	if ip != "" {
		id += ":" + ip
	}
	return id
}

// VisitorTracker records reverse-proxy sessions and supports logging out a
// visitor: the record is removed and the session token is revoked so the
// client must log in again. It also notifies SSE subscribers whenever the
// visitor set changes (a new login, or a logout), so clients can be pushed
// updates instead of polling.
type VisitorTracker struct {
	mu      sync.Mutex
	byToken map[string]*Visitor
	revoked map[string]bool // session cookie token -> revoked
	subs    map[chan struct{}]struct{}
}

// NewVisitorTracker creates an empty visitor tracker.
func NewVisitorTracker() *VisitorTracker {
	return &VisitorTracker{
		byToken: map[string]*Visitor{},
		revoked: map[string]bool{},
		subs:    map[chan struct{}]struct{}{},
	}
}

// subscribe registers a notification channel. The returned unsubscribe func
// removes it once the SSE client disconnects.
func (t *VisitorTracker) subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	t.mu.Lock()
	t.subs[ch] = struct{}{}
	t.mu.Unlock()
	unsub := func() {
		t.mu.Lock()
		delete(t.subs, ch)
		t.mu.Unlock()
	}
	return ch, unsub
}

func (t *VisitorTracker) notify() {
	t.mu.Lock()
	for ch := range t.subs {
		select {
		case ch <- struct{}{}:
		default: // non-blocking; coalesce bursts
		}
	}
	t.mu.Unlock()
}

// record creates (or refreshes) the session entry for token. The token is the
// unique identity; IP/expiry are refreshed on every authenticated request.
func (t *VisitorTracker) record(token, ip string, expireTs int64) {
	if token == "" {
		return
	}
	t.mu.Lock()
	// 已被注销的令牌不应再出现在列表中
	if t.revoked[token] {
		t.mu.Unlock()
		return
	}
	v, existed := t.byToken[token]
	changed := false
	if !existed {
		v = &Visitor{ID: token, Source: visitorSourcePort}
		t.byToken[token] = v
		changed = true
	}
	v.LastAccess = time.Now()
	if ip != "" && ip != v.IP {
		v.IP = ip
		changed = true // 切换 IP 访问时刷新列表中的 IP
	}
	if expireTs > 0 {
		v.ExpiresAt = time.Unix(expireTs, 0)
	}
	t.mu.Unlock()
	// 新登录或 IP 变化时通知订阅者，把更新推送给前端
	if changed {
		t.notify()
	}
}

// recordGateway 记录（或刷新）一条网关访问记录。id 由 gatewayVisitorID 构造
// （用户 + 客户端 IP），ip 写入记录并在列表里展示；username/admin 来自网关注入的
// 身份头；网关已完成登录认证，这里没有会话凭据可存，因此不写 ExpiresAt（保持零值，
// 前端据此不显示「登录有效期至」）。
func (t *VisitorTracker) recordGateway(id, ip, username string, admin bool) {
	if id == "" {
		return
	}
	t.mu.Lock()
	// 网关条目不因注销而被拉黑（Revoke 对它们直接返回 false），只按闲置清理；
	// 若同一用户此前被清理过，这里会重新建一条。
	v, existed := t.byToken[id]
	changed := false
	if !existed {
		v = &Visitor{ID: id, Source: visitorSourceGateway}
		t.byToken[id] = v
		changed = true
	}
	v.LastAccess = time.Now()
	if v.Source != visitorSourceGateway {
		v.Source = visitorSourceGateway
		changed = true
	}
	if ip != "" && ip != v.IP {
		v.IP = ip
		changed = true
	}
	if username != "" && username != v.Username {
		v.Username = username
		changed = true
	}
	if admin != v.Admin {
		v.Admin = admin
		changed = true
	}
	t.mu.Unlock()
	if changed {
		t.notify()
	}
}

// PurgeExpired removes visitor records that are no longer meaningful, revoking
// the tokens of expired port visitors so they must log in again. Gateway
// entries have no expiry and are dropped once idle beyond gatewayVisitorIdleTTL.
// Returns the number of records removed.
func (t *VisitorTracker) PurgeExpired(now time.Time) int {
	if now.IsZero() {
		now = time.Now()
	}
	t.mu.Lock()
	removed := 0
	for tok, v := range t.byToken {
		if v.Source == visitorSourceGateway {
			if now.Sub(v.LastAccess) > gatewayVisitorIdleTTL {
				delete(t.byToken, tok)
				removed++
			}
			continue
		}
		if !v.ExpiresAt.IsZero() && v.ExpiresAt.Before(now) {
			delete(t.byToken, tok)
			t.revoked[tok] = true
			removed++
		}
	}
	t.mu.Unlock()
	return removed
}

// List returns a snapshot of all active visitors. Expired records are purged
// first so the login list never shows stale entries.
func (t *VisitorTracker) List() []Visitor {
	t.PurgeExpired(time.Now())
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Visitor, 0, len(t.byToken))
	for _, v := range t.byToken {
		out = append(out, *v)
	}
	return out
}

// Revoke logs out the visitor identified by token: the record is removed and
// the token is revoked so subsequent requests are redirected to login. Returns
// true if a matching active visitor was found. 网关访问没有会话凭据可吊销
// （请求每次都带网关注入的身份头），一律返回 false，由调用方给出解释。
func (t *VisitorTracker) Revoke(token string) bool {
	if token == "" || isGatewayVisitorID(token) {
		return false
	}
	t.mu.Lock()
	if _, ok := t.byToken[token]; !ok {
		t.mu.Unlock()
		return false
	}
	delete(t.byToken, token)
	t.revoked[token] = true
	t.mu.Unlock()
	t.notify()
	return true
}

// isRevokedToken reports whether a session cookie token has been logged out.
func (t *VisitorTracker) isRevokedToken(token string) bool {
	if token == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.revoked[token]
}
