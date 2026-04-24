package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jacklicn/dipper-bot/utils"
	"github.com/kuhahalong/ddgsearch"
)

const (
	webUserAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7_2) AppleWebKit/537.36"
	webMaxRedirect = 5
	webSearchURL   = "https://api.search.brave.com/res/v1/web/search"
)

// WebSearchTool searches the web via configured provider (brave, tavily, jina, searxng, duckduckgo).
type WebSearchTool struct {
	Provider   string
	APIKey     string
	BaseURL    string
	MaxResults int
	ProxyURL   string
	Client     *http.Client
}

// NewWebSearchTool creates a WebSearchTool. provider: duckduckgo (default), brave, tavily, jina, searxng.
// Default duckduckgo requires no API key. duckduckgo tries github.com/kuhahalong/ddgsearch first, then
// api.duckduckgo.com instant JSON if that yields no results (DDG occasionally changes the d.js surface).
func NewWebSearchTool(provider, apiKey, baseURL string, maxResults int, proxyURL string) *WebSearchTool {
	if provider == "" {
		provider = "duckduckgo"
	}
	if apiKey == "" && provider == "brave" {
		apiKey = os.Getenv("BRAVE_API_KEY")
	}
	if apiKey == "" && provider == "tavily" {
		apiKey = os.Getenv("TAVILY_API_KEY")
	}
	if apiKey == "" && provider == "jina" {
		apiKey = os.Getenv("JINA_API_KEY")
	}
	if baseURL == "" && provider == "searxng" {
		baseURL = os.Getenv("SEARXNG_BASE_URL")
	}
	if maxResults <= 0 {
		maxResults = 5
	}
	client := utils.HTTPClientWithProxy(proxyURL, 15*time.Second)
	return &WebSearchTool{Provider: strings.ToLower(provider), APIKey: apiKey, BaseURL: strings.TrimSuffix(baseURL, "/"), MaxResults: maxResults, ProxyURL: proxyURL, Client: client}
}

func (w *WebSearchTool) Name() string { return "web_search" }
func (w *WebSearchTool) Description() string {
	return "Search the web (titles, URLs, snippets). Call only when the user explicitly asked to search the web or look something up online—not by default."
}

func (w *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"count": map[string]any{"type": "integer", "description": "Results (1-10)", "minimum": 1, "maximum": 10},
		},
		"required": []any{"query"},
	}
}

type searchResult struct {
	title string
	url   string
	desc  string
}

func (w *WebSearchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "Error: query is required", nil
	}
	count, _ := params["count"].(float64)
	n := w.MaxResults
	if count > 0 {
		if count > 10 {
			count = 10
		}
		n = int(count)
	}

	provider := w.Provider
	if provider == "" {
		provider = "duckduckgo"
	}

	var items []searchResult
	var errStr string

	switch provider {
	case "duckduckgo":
		items, errStr = w.searchDuckDuckGo(ctx, query, n)
	case "brave":
		if w.APIKey == "" {
			items, errStr = w.searchDuckDuckGo(ctx, query, n)
		} else {
			items, errStr = w.searchBrave(ctx, query, n)
		}
	case "tavily":
		items, errStr = w.searchTavily(ctx, query, n)
	case "jina":
		items, errStr = w.searchJina(ctx, query, n)
	case "searxng":
		items, errStr = w.searchSearXNG(ctx, query, n)
	default:
		items, errStr = w.searchDuckDuckGo(ctx, query, n)
	}

	if errStr != "" {
		return errStr, nil
	}
	if len(items) == 0 {
		return "No results for: " + query, nil
	}
	lines := make([]string, 0, 1+len(items)*2)
	lines = append(lines, "Results for: "+query)
	for i, r := range items {
		lines = append(lines, fmt.Sprintf("%d. %s\n   %s", i+1, r.title, r.url))
		if r.desc != "" {
			lines = append(lines, "   "+r.desc)
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (w *WebSearchTool) searchBrave(ctx context.Context, query string, n int) ([]searchResult, string) {
	reqURL := webSearchURL + "?q=" + url.QueryEscape(query) + "&count=" + fmt.Sprint(n)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", w.APIKey)
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Sprintf("Error: API returned %d: %s", resp.StatusCode, string(body))
	}
	var data struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "Error: " + err.Error()
	}
	out := make([]searchResult, 0, len(data.Web.Results))
	for _, r := range data.Web.Results {
		out = append(out, searchResult{title: r.Title, url: r.URL, desc: r.Description})
	}
	return out, ""
}

func (w *WebSearchTool) searchTavily(ctx context.Context, query string, n int) ([]searchResult, string) {
	if w.APIKey == "" {
		return w.searchDuckDuckGo(ctx, query, n)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", nil)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.APIKey)
	body, _ := json.Marshal(map[string]any{"query": query, "max_results": n})
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Sprintf("Error: API returned %d: %s", resp.StatusCode, string(b))
	}
	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "Error: " + err.Error()
	}
	out := make([]searchResult, 0, len(data.Results))
	for _, r := range data.Results {
		out = append(out, searchResult{title: r.Title, url: r.URL, desc: r.Content})
	}
	return out, ""
}

func (w *WebSearchTool) searchJina(ctx context.Context, query string, n int) ([]searchResult, string) {
	if w.APIKey == "" {
		return w.searchDuckDuckGo(ctx, query, n)
	}
	reqURL := "https://s.jina.ai/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.APIKey)
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Sprintf("Error: API returned %d: %s", resp.StatusCode, string(b))
	}
	var data struct {
		Data []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "Error: " + err.Error()
	}
	out := make([]searchResult, 0, n)
	for i, r := range data.Data {
		if i >= n {
			break
		}
		desc := r.Content
		if len(desc) > 500 {
			desc = desc[:500]
		}
		out = append(out, searchResult{title: r.Title, url: r.URL, desc: desc})
	}
	return out, ""
}

func (w *WebSearchTool) searchSearXNG(ctx context.Context, query string, n int) ([]searchResult, string) {
	if w.BaseURL == "" {
		return w.searchDuckDuckGo(ctx, query, n)
	}
	endpoint := w.BaseURL + "/search?q=" + url.QueryEscape(query) + "&format=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	req.Header.Set("User-Agent", webUserAgent)
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Sprintf("Error: API returned %d: %s", resp.StatusCode, string(b))
	}
	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "Error: " + err.Error()
	}
	out := make([]searchResult, 0, n)
	for i, r := range data.Results {
		if i >= n {
			break
		}
		out = append(out, searchResult{title: r.Title, url: r.URL, desc: r.Content})
	}
	return out, ""
}

func (w *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string, n int) ([]searchResult, string) {
	// Primary: ddgsearch (links.duckduckgo.com JSON). DuckDuckGo changes this surface occasionally;
	// when it returns an error or zero hits, fall back to the official instant answer API.
	cfg := &ddgsearch.Config{
		Timeout:    15 * time.Second,
		MaxRetries: 2,
		Cache:      false,
		Proxy:      w.ProxyURL,
	}
	client, err := ddgsearch.New(cfg)
	if err != nil {
		return w.searchDuckDuckGoInstantAPI(ctx, query, n)
	}
	params := &ddgsearch.SearchParams{
		Query:      query,
		MaxResults: n,
	}
	resp, err := client.Search(ctx, params)
	if err == nil && len(resp.Results) > 0 {
		out := make([]searchResult, 0, len(resp.Results))
		for _, r := range resp.Results {
			out = append(out, searchResult{title: r.Title, url: r.URL, desc: r.Description})
		}
		if len(out) > n {
			out = out[:n]
		}
		return out, ""
	}
	return w.searchDuckDuckGoInstantAPI(ctx, query, n)
}

// searchDuckDuckGoInstantAPI uses https://api.duckduckgo.com/ (no API key).
// Coverage differs from full web search (strong for disambiguated topics); used as fallback when ddgsearch fails.
func (w *WebSearchTool) searchDuckDuckGoInstantAPI(ctx context.Context, query string, n int) ([]searchResult, string) {
	if n <= 0 {
		n = 5
	}
	if n > 10 {
		n = 10
	}
	reqURL := "https://api.duckduckgo.com/?format=json&no_html=1&skip_disambig=1&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	req.Header.Set("User-Agent", webUserAgent)
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, "Error: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Sprintf("Error: DuckDuckGo API returned %d: %s", resp.StatusCode, string(b))
	}
	var root map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return nil, "Error: " + err.Error()
	}
	out := make([]searchResult, 0, n)
	add := func(title, u, desc string) {
		if len(out) >= n || strings.TrimSpace(u) == "" {
			return
		}
		title = strings.TrimSpace(title)
		if title == "" {
			title = u
		}
		out = append(out, searchResult{title: title, url: u, desc: strings.TrimSpace(desc)})
	}
	if u, _ := root["AbstractURL"].(string); u != "" {
		txt, _ := root["AbstractText"].(string)
		if txt == "" {
			txt, _ = root["Abstract"].(string)
		}
		head, _ := root["Heading"].(string)
		if head == "" {
			head = "Instant answer"
		}
		add(head, u, txt)
	}
	var walkTopics func(v any)
	walkTopics = func(v any) {
		if len(out) >= n {
			return
		}
		switch t := v.(type) {
		case []any:
			for _, item := range t {
				walkTopics(item)
				if len(out) >= n {
					return
				}
			}
		case map[string]any:
			if subs, ok := t["Topics"].([]any); ok {
				walkTopics(subs)
				return
			}
			fu, _ := t["FirstURL"].(string)
			if strings.TrimSpace(fu) == "" {
				return
			}
			text, _ := t["Text"].(string)
			add(text, fu, "")
		}
	}
	if rt, ok := root["RelatedTopics"].([]any); ok {
		walkTopics(rt)
	}
	if res, ok := root["Results"].([]any); ok {
		for _, item := range res {
			if len(out) >= n {
				break
			}
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			u, _ := m["FirstURL"].(string)
			if u == "" {
				u, _ = m["URL"].(string)
			}
			text, _ := m["Text"].(string)
			if text == "" {
				text, _ = m["Title"].(string)
			}
			add(text, u, "")
		}
	}
	if len(out) == 0 {
		return nil, "No results for: " + query
	}
	return out, ""
}

// WebFetchTool fetches a URL and returns readable content (HTML stripped to text).
type WebFetchTool struct {
	MaxChars int
	Client   *http.Client
}

// NewWebFetchTool creates a WebFetchTool. proxyURL for HTTP/SOCKS5.
func NewWebFetchTool(maxChars int, proxyURL string) *WebFetchTool {
	if maxChars <= 0 {
		maxChars = 50000
	}
	client := utils.HTTPClientWithProxy(proxyURL, 30*time.Second)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= webMaxRedirect {
			return fmt.Errorf("too many redirects")
		}
		if err := validateHTTPURL(req.URL); err != nil {
			return err
		}
		return nil
	}
	return &WebFetchTool{MaxChars: maxChars, Client: client}
}

func (f *WebFetchTool) Name() string { return "web_fetch" }
func (f *WebFetchTool) Description() string {
	return "Fetch a URL and extract readable content (HTML → text). Call only when the user explicitly asked to open/fetch a URL or read a specific page—not by default."
}

func (f *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":      map[string]any{"type": "string", "description": "URL to fetch"},
			"maxChars": map[string]any{"type": "integer", "description": "Max characters to return", "minimum": 100},
		},
		"required": []any{"url"},
	}
}

var (
	scriptRe  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
	spaceRe   = regexp.MustCompile(`[ \t]+`)
	newlineRe = regexp.MustCompile(`\n{3,}`)
)

func stripHTML(html string) string {
	html = scriptRe.ReplaceAllString(html, "")
	html = styleRe.ReplaceAllString(html, "")
	html = tagRe.ReplaceAllString(html, " ")
	html = spaceRe.ReplaceAllString(html, " ")
	html = newlineRe.ReplaceAllString(strings.TrimSpace(html), "\n\n")
	return html
}

func (f *WebFetchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	rawURL, _ := params["url"].(string)
	if rawURL == "" {
		return `{"error":"url is required"}`, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return `{"error":"invalid URL"}`, nil
	}
	if err := validateHTTPURL(parsed); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error()), nil
	}
	maxChars := f.MaxChars
	if mc, ok := params["maxChars"].(float64); ok && mc > 0 {
		maxChars = int(mc)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	req.Header.Set("User-Agent", webUserAgent)
	resp, err := f.Client.Do(req)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	defer resp.Body.Close()
	if err := validateHTTPURL(resp.Request.URL); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error()), nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "Error: " + err.Error(), nil
	}
	text := string(body)
	ctype := resp.Header.Get("Content-Type")
	if strings.Contains(ctype, "application/json") {
		var j map[string]any
		if json.Unmarshal(body, &j) == nil {
			b, _ := json.MarshalIndent(j, "", "  ")
			text = string(b)
		}
	} else if strings.Contains(ctype, "text/html") || strings.HasPrefix(strings.ToLower(string(body[:min(256, len(body))])), "<!doctype") || strings.HasPrefix(strings.ToLower(string(body[:min(256, len(body))])), "<html") {
		text = stripHTML(text)
		if title := extractTitle(string(body)); title != "" {
			text = "# " + title + "\n\n" + text
		}
	}
	truncated := len(text) > maxChars
	if truncated {
		text = text[:maxChars]
	}
	out := map[string]any{
		"url":       rawURL,
		"finalUrl":  resp.Request.URL.String(),
		"status":    resp.StatusCode,
		"truncated": truncated,
		"length":    len(text),
		"text":      text,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func validateHTTPURL(u *url.URL) error {
	if u == nil || u.Host == "" {
		return errors.New("only http/https URLs with a valid host are allowed")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("only http/https URLs with a valid host are allowed")
	}
	if isPrivateOrLoopback(u.Host) {
		return errors.New("access to private/loopback addresses is not allowed (SSRF protection)")
	}
	return nil
}

func isPrivateOrLoopback(host string) bool {
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}
	if hostname == "" {
		return true
	}
	ip := net.ParseIP(hostname)
	if ip == nil {
		ips, err := net.LookupIP(hostname)
		if err != nil || len(ips) == 0 {
			return true
		}
		for _, cand := range ips {
			if isBlockedIP(cand) {
				return true
			}
		}
		return false
	}
	return isBlockedIP(ip)
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.IsLoopback() ||
			ip4.IsPrivate() ||
			ip4.IsLinkLocalUnicast() ||
			ip4.IsLinkLocalMulticast() ||
			ip4.IsUnspecified()
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func extractTitle(html string) string {
	re := regexp.MustCompile(`(?is)<title[^>]*>([^<]*)</title>`)
	m := re.FindStringSubmatch(html)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
