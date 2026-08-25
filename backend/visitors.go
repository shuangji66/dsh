package main

import (
	"sync"
	"time"
)

// Visitor represents one authenticated session against the reverse proxy. It
// is keyed by the session token so that multiple clients (even behind the same
// IP) each appear as their own card. The `ID` is the session cookie value used
// as the stable unique key; `IP` is the (possibly shared) source address.
type Visitor struct {
	ID         string    `json:"id"`
	IP         string    `json:"ip"`
	LastAccess time.Time `json:"lastAccess"`
	ExpiresAt  time.Time `json:"expiresAt"` // 登录有效期至
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
		v = &Visitor{ID: token}
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

// List returns a snapshot of all active visitors.
func (t *VisitorTracker) List() []Visitor {
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
// true if a matching active visitor was found.
func (t *VisitorTracker) Revoke(token string) bool {
	if token == "" {
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