package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ConnectMCPServers connects to configured MCP servers in parallel and registers their tools.
func ConnectMCPServers(ctx context.Context, reg *Registry, servers map[string]config.MCPServerConfig) {
	if len(servers) == 0 {
		return
	}
	type result struct {
		name  string
		sess  *mcp.ClientSession
		tools []*mcp.Tool
		err   error
	}
	ch := make(chan result, len(servers))
	for name, cfg := range servers {
		name, cfg := name, cfg
		go func() {
			sess, toolsList, err := connectOne(ctx, name, cfg)
			ch <- result{name, sess, toolsList, err}
		}()
	}
	for i := 0; i < len(servers); i++ {
		r := <-ch
		if r.err != nil {
			slog.Error("MCP connect failed", "server", r.name, "error", r.err)
			continue
		}
		cfg := servers[r.name]
		enabled := map[string]bool{}
		allowAll := len(cfg.EnabledTools) == 0
		for _, t := range cfg.EnabledTools {
			if t == "*" {
				allowAll = true
				break
			}
			enabled[t] = true
		}
		timeoutSec := 30
		if cfg.ToolTimeout > 0 {
			timeoutSec = cfg.ToolTimeout
		}
		for _, t := range r.tools {
			wrappedName := "mcp_" + r.name + "_" + t.Name
			if !allowAll && len(enabled) > 0 {
				if !enabled[t.Name] && !enabled[wrappedName] {
					continue
				}
			}
			wrapper := &mcpToolWrapper{
				session:    r.sess,
				serverName: r.name,
				serverCfg:  cfg,
				tool:       t,
				timeoutSec: timeoutSec,
			}
			reg.Register(wrapper)
		}
	}
}

// connectOne connects to one MCP server (stdio or HTTP) and returns session + tools.
func connectOne(ctx context.Context, name string, cfg config.MCPServerConfig) (sess *mcp.ClientSession, tools []*mcp.Tool, err error) {
	if cfg.Command == "" && cfg.URL == "" {
		return nil, nil, nil
	}
	var transport mcp.Transport
	if cfg.URL != "" {
		t := &mcp.StreamableClientTransport{Endpoint: cfg.URL}
		if len(cfg.Headers) > 0 {
			t.HTTPClient = &http.Client{
				Transport: &headerRoundTripper{
					headers: cfg.Headers,
					base:    http.DefaultTransport,
				},
			}
		}
		transport = t
	} else {
		args := normalizedMCPArgs(cfg.Command, cfg.Args)
		cmd := exec.Command(cfg.Command, args...)
		if len(cfg.Env) > 0 {
			cmd.Env = append(cmd.Env, envSlice(cfg.Env)...)
		}
		transport = &mcp.CommandTransport{Command: cmd}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "dipper-bot", Version: "1.0.0"}, nil)
	sess, err = client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := sess.ListTools(ctx, nil)
	if err != nil {
		sess.Close()
		return nil, nil, err
	}
	toolsList := resp.Tools
	if toolsList == nil {
		toolsList = []*mcp.Tool{}
	}
	return sess, toolsList, nil
}

// headerRoundTripper adds custom headers to outgoing HTTP requests.
type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	for k, v := range h.headers {
		req2.Header.Set(k, v)
	}
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}

func envSlice(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	s := make([]string, 0, len(m))
	for k, v := range m {
		s = append(s, k+"="+v)
	}
	return s
}

type mcpToolWrapper struct {
	session     *mcp.ClientSession
	serverName  string
	serverCfg   config.MCPServerConfig
	tool        *mcp.Tool
	timeoutSec  int
	mu          sync.Mutex
}

func (m *mcpToolWrapper) Name() string {
	return "mcp_" + m.serverName + "_" + m.tool.Name
}

func (m *mcpToolWrapper) Description() string {
	if m.tool.Description != "" {
		return m.tool.Description
	}
	return m.tool.Name
}

func (m *mcpToolWrapper) Parameters() map[string]any {
	if m.tool.InputSchema != nil {
		if schema, ok := m.tool.InputSchema.(map[string]any); ok {
			return schema
		}
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (m *mcpToolWrapper) Execute(ctx context.Context, params map[string]any) (string, error) {
	m.mu.Lock()
	session := m.session
	tool := m.tool
	timeoutSec := m.timeoutSec
	m.mu.Unlock()
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}
	res, err := m.callTool(ctx, session, tool, params)
	if err != nil && shouldReconnectMCP(err) {
		if recErr := m.reconnect(); recErr == nil {
			m.mu.Lock()
			session = m.session
			tool = m.tool
			m.mu.Unlock()
			res, err = m.callTool(ctx, session, tool, params)
		}
	}
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	if res.IsError {
		errMsg := ""
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				errMsg = tc.Text
				break
			}
		}
		if errMsg == "" {
			errMsg = "tool error"
		}
		return "Error: " + errMsg, nil
	}
	parts := make([]string, 0, len(res.Content))
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		} else {
			b, _ := json.Marshal(c)
			parts = append(parts, string(b))
		}
	}
	if len(parts) == 0 {
		return "(no output)", nil
	}
	return strings.Join(parts, "\n"), nil
}

func (m *mcpToolWrapper) callTool(ctx context.Context, sess *mcp.ClientSession, tool *mcp.Tool, params map[string]any) (*mcp.CallToolResult, error) {
	if sess == nil || tool == nil {
		return nil, context.Canceled
	}
	return sess.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: params})
}

func (m *mcpToolWrapper) reconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, toolsList, err := connectOne(context.Background(), m.serverName, m.serverCfg)
	if err != nil {
		return err
	}
	var matched *mcp.Tool
	for _, t := range toolsList {
		if t != nil && m.tool != nil && t.Name == m.tool.Name {
			matched = t
			break
		}
	}
	if matched == nil {
		return context.Canceled
	}
	m.session = sess
	m.tool = matched
	return nil
}

func shouldReconnectMCP(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "closed") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "transport")
}

func normalizedMCPArgs(command string, args []string) []string {
	if len(args) == 0 || !isChromeDevtoolsCommand(command, args) {
		return args
	}
	out := make([]string, 0, len(args))
	hasAutoConnect := false
	for _, a := range args {
		if a == "--auto-connect" || a == "--autoConnect" {
			hasAutoConnect = true
			break
		}
	}
	for _, a := range args {
		if hasAutoConnect && (strings.HasPrefix(a, "--browser-url=") || strings.HasPrefix(a, "--browserUrl=")) {
			// Prefer auto-connect when both are set to avoid attach mode conflicts.
			continue
		}
		if strings.HasPrefix(a, "--browser-url=") || strings.HasPrefix(a, "--browserUrl=") {
			parts := strings.SplitN(a, "=", 2)
			if len(parts) == 2 && !hasURLScheme(parts[1]) {
				a = parts[0] + "=http://" + parts[1]
			}
		}
		out = append(out, a)
	}
	return out
}

func isChromeDevtoolsCommand(command string, args []string) bool {
	if strings.Contains(strings.ToLower(command), "chrome-devtools-mcp") {
		return true
	}
	for _, a := range args {
		if strings.Contains(strings.ToLower(a), "chrome-devtools-mcp") {
			return true
		}
	}
	return false
}

func hasURLScheme(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme != ""
}
