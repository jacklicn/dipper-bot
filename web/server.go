package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jacklicn/dipper-bot/agent"
	"github.com/jacklicn/dipper-bot/providers"
	"github.com/jacklicn/dipper-bot/tools"
	"github.com/jacklicn/dipper-bot/utils"
)

//go:embed static
var chatFS embed.FS

// Server serves the web chat UI and API.
type Server struct {
	loopMu      sync.RWMutex
	loop        *agent.AgentLoop
	workspace   string
	host        string
	port        int
	server      *http.Server
	transcriber providers.TranscriptionProvider
}

// NewServer creates a web chat server. host may be empty for 0.0.0.0.
// workspace is the expanded workspace path where uploaded files are stored.
// transcriber enables voice input in the web UI (nil to disable).
func NewServer(loop *agent.AgentLoop, workspace string, host string, port int, transcriber providers.TranscriptionProvider) *Server {
	s := &Server{loop: loop, workspace: workspace, host: host, port: port, transcriber: transcriber}
	addr := fmt.Sprintf("%s:%d", host, port)
	if host == "" {
		addr = ":" + fmt.Sprintf("%d", port)
	}
	mux := http.NewServeMux()
	staticFS, _ := fs.Sub(chatFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /", s.serveIndex)
	mux.HandleFunc("GET /api/learning/dashboard", s.handleLearningDashboardAPI)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("POST /api/chat/stream", s.handleChatStream)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("POST /api/transcribe", s.handleTranscribe)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 6 * time.Minute, // LLM + tools may take a while; must exceed handleChatStream's 5min ctx
	}
	return s
}

func (s *Server) getLoop() *agent.AgentLoop {
	s.loopMu.RLock()
	defer s.loopMu.RUnlock()
	return s.loop
}

// SetLoop atomically swaps the active agent loop (used by config hot-reload).
func (s *Server) SetLoop(loop *agent.AgentLoop) {
	if loop == nil {
		return
	}
	s.loopMu.Lock()
	s.loop = loop
	s.loopMu.Unlock()
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, _ := chatFS.ReadFile("static/chat.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleLearningDashboardAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	window := 168
	if v := r.URL.Query().Get("window_hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			window = n
		}
	}
	p := filepath.Join(s.workspace, "memory", "learning_telemetry.jsonl")
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			// First-run friendly: no telemetry file yet should render an empty dashboard, not an error.
			m, berr := tools.BuildLearningDashboardMap(s.workspace, nil, window)
			if berr == nil {
				respondJSON(w, m)
				return
			}
		}
		respondJSON(w, map[string]any{"success": false, "error": "telemetry unavailable"})
		return
	}
	lines := strings.Split(string(raw), "\n")
	m, err := tools.BuildLearningDashboardMap(s.workspace, lines, window)
	if err != nil {
		respondJSON(w, map[string]any{"success": false, "error": err.Error()})
		return
	}
	respondJSON(w, m)
}

// ChatRequest is the JSON body for POST /api/chat.
type ChatRequest struct {
	Content string `json:"content"`
}

const maxChatRequestBytes = 1 << 20 // 1MB

// ChatResponse is the JSON response from POST /api/chat.
type ChatResponse struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req ChatRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var tail any
	if err := dec.Decode(&tail); err != io.EOF {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		respondJSON(w, ChatResponse{Error: "content is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	resp, err := s.getLoop().ProcessDirect(ctx, req.Content, "web:default", "web", "default", nil)
	if err != nil {
		slog.Error("web chat", "error", err)
		respondJSON(w, ChatResponse{Error: err.Error()})
		return
	}
	respondJSON(w, ChatResponse{Content: resp})
}

// handleChatStream streams chat response with progress events (NDJSON).
// Events: {"type":"progress","tool":"read_file","detail":"path"} | {"type":"done","content":"..."} | {"type":"error","error":"..."}
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req ChatRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var tail any
	if err := dec.Decode(&tail); err != io.EOF {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		respondJSON(w, ChatResponse{Error: "content is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	type streamEvent struct {
		Type    string `json:"type"`
		Tool    string `json:"tool,omitempty"`
		Detail  string `json:"detail,omitempty"`
		Content string `json:"content,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	evCh := make(chan streamEvent, 32)

	go func() {
		defer close(evCh)
		emit := func(ev streamEvent) bool {
			select {
			case evCh <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		resp, err := s.getLoop().ProcessDirect(ctx, req.Content, "web:default", "web", "default", func(toolName, detail string) {
			_ = emit(streamEvent{Type: "progress", Tool: toolName, Detail: detail})
		})
		if err != nil {
			_ = emit(streamEvent{Type: "error", Error: err.Error()})
		} else {
			_ = emit(streamEvent{Type: "done", Content: resp})
		}
	}()

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	enc := json.NewEncoder(w)
	for ev := range evCh {
		if err := enc.Encode(ev); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		if ev.Type == "done" || ev.Type == "error" {
			return
		}
	}
}

// UploadResponse is the JSON response from POST /api/upload.
type UploadResponse struct {
	Paths []string `json:"paths"`
	Error string   `json:"error,omitempty"`
}

const maxUploadBytes = 50 << 20 // 50MB

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		respondJSON(w, UploadResponse{Error: "文件过大或格式错误"})
		return
	}
	uploads, ok := r.MultipartForm.File["file"]
	if !ok || len(uploads) == 0 {
		respondJSON(w, UploadResponse{Error: "请选择要上传的文件"})
		return
	}

	uploadDir := filepath.Join(s.workspace, "uploads")
	if _, err := utils.EnsureDir(uploadDir); err != nil {
		slog.Error("web upload mkdir", "error", err)
		respondJSON(w, UploadResponse{Error: "无法创建上传目录"})
		return
	}

	var paths []string
	for _, fh := range uploads {
		base := filepath.Base(fh.Filename)
		if base == "" || base == "." || strings.Contains(base, "..") {
			continue
		}
		safe := utils.SafeFilename(base)
		if safe == "" {
			safe = "upload"
		}
		dst := filepath.Join(uploadDir, safe)
		// avoid overwrite: append number if exists
		for i := 1; ; i++ {
			if _, err := os.Stat(dst); os.IsNotExist(err) {
				break
			}
			ext := filepath.Ext(safe)
			name := strings.TrimSuffix(safe, ext)
			dst = filepath.Join(uploadDir, fmt.Sprintf("%s_%d%s", name, i, ext))
		}
		src, err := fh.Open()
		if err != nil {
			slog.Error("web upload open", "file", fh.Filename, "error", err)
			continue
		}
		if err := copyToFile(dst, src); err != nil {
			src.Close()
			slog.Error("web upload save", "file", fh.Filename, "error", err)
			continue
		}
		src.Close()
		rel, _ := filepath.Rel(s.workspace, dst)
		if rel == "" || strings.HasPrefix(rel, "..") {
			rel = filepath.Join("uploads", safe)
		}
		paths = append(paths, filepath.ToSlash(rel))
	}
	if len(paths) == 0 {
		respondJSON(w, UploadResponse{Error: "没有成功保存任何文件"})
		return
	}
	respondJSON(w, UploadResponse{Paths: paths})
}

// TranscribeResponse is the JSON response from POST /api/transcribe.
type TranscribeResponse struct {
	Text  string `json:"text"`
	Error string `json:"error,omitempty"`
}

func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.transcriber == nil {
		respondJSON(w, TranscribeResponse{Error: "voice transcription not configured"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondJSON(w, TranscribeResponse{Error: "invalid multipart form"})
		return
	}
	file, _, err := r.FormFile("audio")
	if err != nil {
		respondJSON(w, TranscribeResponse{Error: "missing audio file"})
		return
	}
	defer file.Close()

	uploadDir := filepath.Join(s.workspace, "uploads")
	if _, err := utils.EnsureDir(uploadDir); err != nil {
		slog.Error("transcribe mkdir", "error", err)
		respondJSON(w, TranscribeResponse{Error: "failed to create upload dir"})
		return
	}
	tmp, err := os.CreateTemp(uploadDir, "voice-*.webm")
	if err != nil {
		respondJSON(w, TranscribeResponse{Error: "failed to create temp file"})
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		respondJSON(w, TranscribeResponse{Error: "failed to save audio"})
		return
	}
	if err := tmp.Close(); err != nil {
		respondJSON(w, TranscribeResponse{Error: "failed to close temp file"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	text, err := s.transcriber.Transcribe(ctx, tmpPath)
	if err != nil {
		slog.Warn("transcribe", "error", err)
		respondJSON(w, TranscribeResponse{Error: err.Error()})
		return
	}
	respondJSON(w, TranscribeResponse{Text: text})
}

func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func copyToFile(dst string, src io.Reader) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}

// Start starts the HTTP server (blocking).
func (s *Server) Start() error {
	slog.Info("web chat listening", "addr", s.server.Addr)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
