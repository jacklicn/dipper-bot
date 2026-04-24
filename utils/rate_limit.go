package utils

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// FixedWindowLimiter is a lightweight per-key fixed-window rate limiter.
type FixedWindowLimiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	now        func() time.Time
	entries    map[string]fixedWindowEntry
	gcInterval time.Duration
	lastGC     time.Time
}

type fixedWindowEntry struct {
	windowStart time.Time
	count       int
}

// NewFixedWindowLimiter creates a per-key limiter.
func NewFixedWindowLimiter(limit int, window time.Duration) *FixedWindowLimiter {
	if limit <= 0 {
		limit = 60
	}
	if window <= 0 {
		window = time.Minute
	}
	now := time.Now
	return &FixedWindowLimiter{
		limit:      limit,
		window:     window,
		now:        now,
		entries:    make(map[string]fixedWindowEntry),
		gcInterval: 5 * window,
		lastGC:     now(),
	}
}

// Allow reports whether key is within rate limit.
func (l *FixedWindowLimiter) Allow(key string) bool {
	if key == "" {
		key = "unknown"
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	ent, ok := l.entries[key]
	if !ok || now.Sub(ent.windowStart) >= l.window {
		l.entries[key] = fixedWindowEntry{windowStart: now, count: 1}
		l.gcLocked(now)
		return true
	}
	if ent.count >= l.limit {
		l.gcLocked(now)
		return false
	}
	ent.count++
	l.entries[key] = ent
	l.gcLocked(now)
	return true
}

func (l *FixedWindowLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < l.gcInterval {
		return
	}
	ttl := l.window * 2
	for k, ent := range l.entries {
		if now.Sub(ent.windowStart) >= ttl {
			delete(l.entries, k)
		}
	}
	l.lastGC = now
}

// ClientIP extracts the caller IP from X-Forwarded-For / X-Real-IP / RemoteAddr.
func ClientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		for _, p := range parts {
			ip := strings.TrimSpace(p)
			if ip != "" {
				return ip
			}
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if strings.TrimSpace(r.RemoteAddr) != "" {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return "unknown"
}

// RateLimitKeyer builds consistent rate-limit keys for IPv4/IPv6/CIDR rules.
type RateLimitKeyer struct {
	ipv4Prefix int
	ipv6Prefix int
	cidrs      []*net.IPNet
}

// NewRateLimitKeyer creates a keyer.
// Defaults: IPv4 /32, IPv6 /128 (single-IP limiting).
func NewRateLimitKeyer(ipv4Prefix, ipv6Prefix int, cidrRules []string) *RateLimitKeyer {
	if ipv4Prefix <= 0 || ipv4Prefix > 32 {
		ipv4Prefix = 32
	}
	if ipv6Prefix <= 0 || ipv6Prefix > 128 {
		ipv6Prefix = 128
	}
	k := &RateLimitKeyer{
		ipv4Prefix: ipv4Prefix,
		ipv6Prefix: ipv6Prefix,
	}
	for _, raw := range cidrRules {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, n, err := net.ParseCIDR(raw)
		if err != nil || n == nil {
			continue
		}
		k.cidrs = append(k.cidrs, n)
	}
	return k
}

// KeyFromRequest returns the rate-limit key for request client IP.
func (k *RateLimitKeyer) KeyFromRequest(r *http.Request) string {
	return k.KeyFromIP(ClientIP(r))
}

// KeyFromIP returns normalized key from IP text.
func (k *RateLimitKeyer) KeyFromIP(ipStr string) string {
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "" {
		return "unknown"
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr
	}
	// Highest priority: user-configured CIDR buckets.
	for _, n := range k.cidrs {
		if n.Contains(ip) {
			return "cidr:" + n.String()
		}
	}
	if ip4 := ip.To4(); ip4 != nil {
		masked := ip4.Mask(net.CIDRMask(k.ipv4Prefix, 32))
		return "v4:" + masked.String() + "/" + itoa(k.ipv4Prefix)
	}
	masked := ip.Mask(net.CIDRMask(k.ipv6Prefix, 128))
	return "v6:" + masked.String() + "/" + itoa(k.ipv6Prefix)
}

func itoa(v int) string {
	// avoid extra strconv import in this tiny utility.
	if v == 0 {
		return "0"
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return sign + string(b[i:])
}
