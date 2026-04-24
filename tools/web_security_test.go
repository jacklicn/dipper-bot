package tools

import (
	"net/url"
	"testing"
)

func TestValidateHTTPURLRejectsPrivateHosts(t *testing.T) {
	cases := []string{
		"http://127.0.0.1",
		"http://localhost:8080",
		"http://10.0.0.2/path",
		"http://192.168.1.9",
		"http://169.254.1.1",
	}
	for _, raw := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if err := validateHTTPURL(u); err == nil {
			t.Fatalf("expected %q to be blocked", raw)
		}
	}
}

func TestValidateHTTPURLAllowsPublicHosts(t *testing.T) {
	u, err := url.Parse("https://example.com/docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHTTPURL(u); err != nil {
		t.Fatalf("expected public URL to be allowed, got: %v", err)
	}
}
