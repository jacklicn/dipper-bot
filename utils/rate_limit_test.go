package utils

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestFixedWindowLimiterAllowAndBlock(t *testing.T) {
	l := NewFixedWindowLimiter(2, time.Minute)
	base := time.Unix(1000, 0)
	l.now = func() time.Time { return base }

	if !l.Allow("k") {
		t.Fatal("first request should pass")
	}
	if !l.Allow("k") {
		t.Fatal("second request should pass")
	}
	if l.Allow("k") {
		t.Fatal("third request should be blocked")
	}
}

func TestFixedWindowLimiterResetsAfterWindow(t *testing.T) {
	l := NewFixedWindowLimiter(1, time.Minute)
	now := time.Unix(2000, 0)
	l.now = func() time.Time { return now }

	if !l.Allow("k") {
		t.Fatal("first request should pass")
	}
	if l.Allow("k") {
		t.Fatal("second request in same window should be blocked")
	}

	now = now.Add(61 * time.Second)
	if !l.Allow("k") {
		t.Fatal("request should pass after window reset")
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest("POST", "http://example.com", nil)
	req.Header.Set("X-Forwarded-For", " 1.2.3.4 , 5.6.7.8 ")
	if got := ClientIP(req); got != "1.2.3.4" {
		t.Fatalf("ClientIP XFF got %q", got)
	}

	req2 := httptest.NewRequest("POST", "http://example.com", nil)
	req2.Header.Set("X-Real-IP", "9.9.9.9")
	if got := ClientIP(req2); got != "9.9.9.9" {
		t.Fatalf("ClientIP X-Real-IP got %q", got)
	}
}

func TestRateLimitKeyerIPv4Prefix(t *testing.T) {
	k := NewRateLimitKeyer(24, 64, nil)
	a := k.KeyFromIP("10.1.2.3")
	b := k.KeyFromIP("10.1.2.99")
	c := k.KeyFromIP("10.1.3.4")
	if a != b {
		t.Fatalf("same /24 should match: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("different /24 should differ: %q vs %q", a, c)
	}
}

func TestRateLimitKeyerIPv6Prefix(t *testing.T) {
	k := NewRateLimitKeyer(32, 64, nil)
	a := k.KeyFromIP("2001:db8:1::1")
	b := k.KeyFromIP("2001:db8:1::ff")
	c := k.KeyFromIP("2001:db8:2::1")
	if a != b {
		t.Fatalf("same /64 should match: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("different /64 should differ: %q vs %q", a, c)
	}
}

func TestRateLimitKeyerCIDRRuleTakesPriority(t *testing.T) {
	k := NewRateLimitKeyer(32, 128, []string{"10.0.0.0/8"})
	a := k.KeyFromIP("10.1.2.3")
	b := k.KeyFromIP("10.9.8.7")
	if a != "cidr:10.0.0.0/8" || b != "cidr:10.0.0.0/8" {
		t.Fatalf("CIDR key mismatch: a=%q b=%q", a, b)
	}
}
