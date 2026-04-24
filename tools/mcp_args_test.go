package tools

import "testing"

func TestNormalizedMCPArgs_AddsBrowserURLScheme(t *testing.T) {
	in := []string{"-y", "chrome-devtools-mcp@latest", "--browser-url=127.0.0.1:9222"}
	out := normalizedMCPArgs("npx", in)
	want := "--browser-url=http://127.0.0.1:9222"
	found := false
	for _, a := range out {
		if a == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in args, got %#v", want, out)
	}
}

func TestNormalizedMCPArgs_PrefersAutoConnectOverBrowserURL(t *testing.T) {
	in := []string{
		"-y", "chrome-devtools-mcp@latest",
		"--auto-connect",
		"--browser-url=http://127.0.0.1:9222",
	}
	out := normalizedMCPArgs("npx", in)
	for _, a := range out {
		if a == "--browser-url=http://127.0.0.1:9222" {
			t.Fatalf("browser-url should be removed when auto-connect is present, got %#v", out)
		}
	}
}

