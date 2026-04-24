package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunMarkItDownBody_ControlledFailure(t *testing.T) {
	origResolve := resolveWorkspacePython
	origRun := runMarkitdownCommand
	t.Cleanup(func() {
		resolveWorkspacePython = origResolve
		runMarkitdownCommand = origRun
	})

	resolveWorkspacePython = func(workspace string) (string, error) {
		return "python", nil
	}
	runMarkitdownCommand = func(ctx context.Context, py, absPath, workspaceDir string) (string, error) {
		return "No module named 'markitdown'", errors.New("exit status 1")
	}

	_, err := runMarkItDownBody(context.Background(), "/tmp/sample.pdf", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "markitdown is not installed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMarkItDownBody_TruncatesLongOutput(t *testing.T) {
	origResolve := resolveWorkspacePython
	origRun := runMarkitdownCommand
	t.Cleanup(func() {
		resolveWorkspacePython = origResolve
		runMarkitdownCommand = origRun
	})

	resolveWorkspacePython = func(workspace string) (string, error) {
		return "python", nil
	}
	runMarkitdownCommand = func(ctx context.Context, py, absPath, workspaceDir string) (string, error) {
		return strings.Repeat("a", maxMarkitdownChars+128), nil
	}

	got, err := runMarkItDownBody(context.Background(), "/tmp/sample.pdf", "/tmp/ws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "... (truncated: output exceeded read_file limit for converted documents)") {
		t.Fatalf("expected truncation marker, got len=%d", len(got))
	}
	if len(got) <= maxMarkitdownChars {
		t.Fatalf("expected output to include truncation suffix, len=%d", len(got))
	}
}

func TestRunMarkItDownBody_GenericFailureIncludesStderr(t *testing.T) {
	origResolve := resolveWorkspacePython
	origRun := runMarkitdownCommand
	t.Cleanup(func() {
		resolveWorkspacePython = origResolve
		runMarkitdownCommand = origRun
	})

	resolveWorkspacePython = func(workspace string) (string, error) {
		return "python", nil
	}
	runMarkitdownCommand = func(ctx context.Context, py, absPath, workspaceDir string) (string, error) {
		return "traceback: conversion failed", errors.New("exit status 2")
	}

	_, err := runMarkItDownBody(context.Background(), "/tmp/sample.docx", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "markitdown failed") {
		t.Fatalf("expected generic failure prefix, got: %v", err)
	}
	if !strings.Contains(msg, "traceback: conversion failed") {
		t.Fatalf("expected stderr content in error, got: %v", err)
	}
}

