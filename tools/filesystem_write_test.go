package tools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileBase64RoundTrip(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	w := &WriteFileTool{WorkspaceDir: ws}
	want := []byte{0x50, 0x4b, 0x03, 0x04, 0xff, 0x00}
	b64 := base64.StdEncoding.EncodeToString(want)
	out, err := w.Execute(ctx, map[string]any{
		"path":             filepath.Join("outputs", "sample.bin"),
		"content":          b64,
		"content_encoding": "base64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "OK" {
		t.Fatalf("Execute: %s", out)
	}
	got, err := os.ReadFile(filepath.Join(ws, "outputs", "sample.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("bytes mismatch: got %v want %v", got, want)
	}
}

func TestWriteFileBase64RawStdPadding(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	w := &WriteFileTool{WorkspaceDir: ws}
	want := []byte{1, 2, 3, 4, 5}
	b64 := base64.RawStdEncoding.EncodeToString(want)
	out, err := w.Execute(ctx, map[string]any{
		"path":             "outputs/raw.bin",
		"content":          b64,
		"content_encoding": "base64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "OK" {
		t.Fatalf("Execute: %s", out)
	}
	got, err := os.ReadFile(filepath.Join(ws, "outputs", "raw.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("bytes mismatch")
	}
}

func TestWriteFileInvalidUTF8WithoutBase64(t *testing.T) {
	ctx := context.Background()
	w := &WriteFileTool{WorkspaceDir: t.TempDir()}
	out, err := w.Execute(ctx, map[string]any{
		"path":    "x.txt",
		"content": string([]byte{0xff, 0xfe, 0xfd}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out == "OK" {
		t.Fatal("expected error message for invalid UTF-8")
	}
}
