package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/jacklicn/dipper-bot/config"
)

// TranscriptionProvider transcribes audio files to text.
type TranscriptionProvider interface {
	Transcribe(ctx context.Context, filePath string) (string, error)
}

const groqTranscribeURL = "https://api.groq.com/openai/v1/audio/transcriptions"

// GroqTranscriptionProvider transcribes audio using Groq's Whisper API.
type GroqTranscriptionProvider struct {
	APIKey string
	Client *http.Client
}

// NewGroqTranscriptionProvider creates a transcription provider.
func NewGroqTranscriptionProvider(apiKey string) *GroqTranscriptionProvider {
	if apiKey == "" {
		apiKey = os.Getenv("GROQ_API_KEY")
	}
	return &GroqTranscriptionProvider{
		APIKey: apiKey,
		Client: http.DefaultClient,
	}
}

// Transcribe transcribes an audio file and returns the text.
func (p *GroqTranscriptionProvider) Transcribe(ctx context.Context, filePath string) (string, error) {
	if p.APIKey == "" {
		return "", nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	_ = w.WriteField("model", "whisper-large-v3")
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqTranscribeURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := p.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("groq transcription %s: %s", resp.Status, string(respBody))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

// VoskTranscriptionProvider transcribes audio using a local Vosk WebSocket server.
// Requires ffmpeg for audio conversion (OGG/WAV/WebM -> 16kHz mono PCM).
// Start Vosk server: docker run -d -p 2700:2700 alphacep/kaldi-en (English)
// or alphacep/kaldi-cn (Chinese).
type VoskTranscriptionProvider struct {
	WebSocketURL string // e.g. ws://localhost:2700
}

// NewVoskTranscriptionProvider creates a Vosk transcription provider.
func NewVoskTranscriptionProvider(wsURL string) *VoskTranscriptionProvider {
	if wsURL == "" {
		wsURL = os.Getenv("VOSK_WS_URL")
	}
	if wsURL == "" {
		wsURL = "ws://localhost:2700"
	}
	return &VoskTranscriptionProvider{WebSocketURL: wsURL}
}

// Transcribe converts the audio file to 16kHz mono PCM via ffmpeg, streams to Vosk WebSocket, and returns the text.
func (p *VoskTranscriptionProvider) Transcribe(ctx context.Context, filePath string) (string, error) {
	if p.WebSocketURL == "" {
		return "", nil
	}
	// Ensure ws:// or wss://
	u := p.WebSocketURL
	if !strings.HasPrefix(u, "ws://") && !strings.HasPrefix(u, "wss://") {
		u = "ws://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", fmt.Errorf("vosk url: %w", err)
	}
	wsURL := parsed.String()

	// Run ffmpeg to convert to 16kHz mono 16-bit PCM
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin", "-loglevel", "quiet", "-i", filePath,
		"-ar", "16000", "-ac", "1", "-f", "s16le", "-",
	)
	cmd.Stderr = nil
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("vosk ffmpeg pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("vosk ffmpeg start: %w", err)
	}
	defer cmd.Wait()

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("vosk connect: %w", err)
	}
	defer conn.Close()

	// Send config
	configMsg := []byte(`{"config":{"sample_rate":16000}}`)
	if err := conn.WriteMessage(websocket.TextMessage, configMsg); err != nil {
		return "", fmt.Errorf("vosk config: %w", err)
	}

	// Stream PCM chunks (8000 bytes = 0.25s at 16kHz)
	const chunkSize = 8000
	var fullText strings.Builder
	for {
		buf := make([]byte, chunkSize)
		n, err := stdout.Read(buf)
		if err != nil && n == 0 {
			break
		}
		if n > 0 {
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return "", fmt.Errorf("vosk send: %w", err)
			}
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return "", fmt.Errorf("vosk recv: %w", err)
			}
			if text := extractVoskText(msg); text != "" {
				fullText.WriteString(text)
			}
		}
		if err != nil {
			break
		}
	}

	// Send EOF
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"eof":1}`)); err != nil {
		return "", fmt.Errorf("vosk eof: %w", err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return "", fmt.Errorf("vosk final: %w", err)
	}
	if text := extractVoskText(msg); text != "" {
		fullText.WriteString(text)
	}

	return strings.TrimSpace(fullText.String()), nil
}

func extractVoskText(msg []byte) string {
	var v struct {
		Text   string `json:"text"`
		Result []struct {
			Word string `json:"word"`
		} `json:"result"`
	}
	if err := json.Unmarshal(msg, &v); err != nil {
		return ""
	}
	if v.Text != "" {
		return v.Text
	}
	var b strings.Builder
	for _, r := range v.Result {
		b.WriteString(r.Word)
	}
	return b.String()
}

// NewTranscriptionProviderFromConfig creates a TranscriptionProvider from config.
// Returns nil if no transcription is configured (provider empty, or groq without APIKey, or vosk without URL).
func NewTranscriptionProviderFromConfig(cfg *config.Config) TranscriptionProvider {
	if cfg == nil || cfg.Providers.Transcription.Provider == "" {
		// Fallback: use Groq if API key is set (backward compatible)
		if cfg != nil && cfg.Providers.Groq.APIKey != "" {
			return NewGroqTranscriptionProvider(cfg.Providers.Groq.APIKey)
		}
		return nil
	}
	switch strings.ToLower(cfg.Providers.Transcription.Provider) {
	case "groq":
		if cfg.Providers.Groq.APIKey == "" {
			return nil
		}
		return NewGroqTranscriptionProvider(cfg.Providers.Groq.APIKey)
	case "vosk":
		url := cfg.Providers.Transcription.VoskURL
		if url == "" {
			url = "ws://localhost:2700"
		}
		return NewVoskTranscriptionProvider(url)
	default:
		return nil
	}
}
