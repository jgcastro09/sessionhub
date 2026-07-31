package webserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/remote"
)

const (
	pairingCookieName = "sessionhub_web_token"
	pairingTokenTTL   = 30 * 24 * time.Hour
)

// pairing is the web panel's lightweight substitute for user accounts: a
// short numeric code, shown in the TUI, exchanged once per device for a
// long-lived HttpOnly cookie. It only guards WebBindLocal — WebBindTailscale
// traffic is already inside the same trust boundary internal/remote relies
// on for its own protocol, so it needs no code.
type pairing struct {
	mu     sync.Mutex
	code   string
	tokens map[string]time.Time
}

func newPairing() *pairing {
	p := &pairing{tokens: map[string]time.Time{}}
	p.Regenerate()
	return p
}

// Regenerate issues a new pairing code, invalidating the previous one (but
// not devices already paired — their cookies remain valid).
func (p *pairing) Regenerate() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code = randomDigits(6)
	return p.code
}

func (p *pairing) Code() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}

func (p *pairing) redeem(code string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if code == "" || code != p.code {
		return "", false
	}
	token := randomToken()
	p.tokens[token] = time.Now().Add(pairingTokenTTL)
	return token, true
}

func (p *pairing) valid(token string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	expiry, ok := p.tokens[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(p.tokens, token)
		return false
	}
	return true
}

func randomDigits(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = '0' + b%10
	}
	return string(out)
}

func randomToken() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	token, ok := s.pairing.redeem(body.Code)
	if !ok {
		http.Error(w, "invalid pairing code", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: pairingCookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(pairingTokenTTL.Seconds()),
	})
	w.WriteHeader(http.StatusNoContent)
}

// requireTrusted rejects any request that isn't from loopback, from a
// Tailscale address (when the bind mode allows it), or carrying a valid
// pairing cookie (when the bind mode allows local pairing).
func (s *Server) requireTrusted(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isTrusted(r) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized: pair this device from SessionHub Settings first", http.StatusUnauthorized)
	})
}

func (s *Server) isTrusted(r *http.Request) bool {
	addr, err := requestAddr(r)
	if err == nil && addr.IsLoopback() {
		return true
	}
	allowTailscale := s.bindMode == config.WebBindTailscale || s.bindMode == config.WebBindBoth
	allowLocal := s.bindMode == config.WebBindLocal || s.bindMode == config.WebBindBoth
	if allowTailscale && err == nil && remote.IsTailscaleIP(addr) {
		return true
	}
	if allowLocal {
		if cookie, cookieErr := r.Cookie(pairingCookieName); cookieErr == nil && s.pairing.valid(cookie.Value) {
			return true
		}
	}
	return false
}

func requestAddr(r *http.Request) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, err
	}
	return addr.Unmap(), nil
}
