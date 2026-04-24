package utils

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// HTTPClientWithProxy returns an http.Client that uses the given proxy URL.
// Supports http://, https://, and socks5:// proxy URLs.
func HTTPClientWithProxy(proxyURL string, timeout time.Duration) *http.Client {
	if proxyURL == "" {
		return &http.Client{Timeout: timeout}
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return &http.Client{Timeout: timeout}
	}
	transport := &http.Transport{}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
		addr := u.Host
		if u.Port() == "" && !strings.Contains(addr, ":") {
			addr = net.JoinHostPort(addr, "1080")
		}
		dialer, err := proxy.SOCKS5("tcp", addr, nil, proxy.Direct)
		if err != nil {
			return &http.Client{Timeout: timeout}
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	default:
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}
