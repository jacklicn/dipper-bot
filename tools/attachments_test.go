package tools

import (
	"context"
	"strings"
	"testing"
)

func TestParseAttachmentPaths_MultipleLinesAndDedup(t *testing.T) {
	in := `
hello
[附件] uploads/a.pdf, uploads/b.docx
random text
[attachment] uploads/b.docx, uploads/c.xlsx
`
	got := parseAttachmentPaths(in)
	want := []string{"uploads/a.pdf", "uploads/b.docx", "uploads/c.xlsx"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d mismatch: got=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestParseAttachmentPaths_EmptyAttachmentLineIgnored(t *testing.T) {
	in := `
[附件]
[附件] uploads/a.pdf
`
	got := parseAttachmentPaths(in)
	if len(got) != 1 || got[0] != "uploads/a.pdf" {
		t.Fatalf("unexpected parsed paths: %v", got)
	}
}

func TestExpandDocumentsInUserMessage_AppendsConvertedDocsAndSkipsNonDocs(t *testing.T) {
	origResolve := resolveWorkspacePython
	origRun := runMarkitdownCommand
	t.Cleanup(func() {
		resolveWorkspacePython = origResolve
		runMarkitdownCommand = origRun
	})
	resolveWorkspacePython = func(workspace string) (string, error) { return "python", nil }
	runMarkitdownCommand = func(ctx context.Context, py, absPath, workspaceDir string) (string, error) {
		return "converted-body", nil
	}

	ws := t.TempDir()
	in := "请处理附件\n[附件] uploads/a.pdf, uploads/image.png"
	out := ExpandDocumentsInUserMessage(context.Background(), ws, in, 4000)

	if !strings.Contains(out, "## Attached documents (converted to Markdown)") {
		t.Fatalf("expected attachment converted section, got: %s", out)
	}
	if !strings.Contains(out, "### uploads/a.pdf") || !strings.Contains(out, "converted-body") {
		t.Fatalf("expected converted pdf body in output, got: %s", out)
	}
	if strings.Contains(out, "### uploads/image.png") {
		t.Fatalf("non-document attachment should not be converted, got: %s", out)
	}
}

func TestExpandDocumentsInUserMessage_TruncatesWhenBudgetExceeded(t *testing.T) {
	origResolve := resolveWorkspacePython
	origRun := runMarkitdownCommand
	t.Cleanup(func() {
		resolveWorkspacePython = origResolve
		runMarkitdownCommand = origRun
	})
	resolveWorkspacePython = func(workspace string) (string, error) { return "python", nil }
	runMarkitdownCommand = func(ctx context.Context, py, absPath, workspaceDir string) (string, error) {
		return strings.Repeat("x", 200), nil
	}

	ws := t.TempDir()
	in := "[附件] uploads/a.pdf"
	out := ExpandDocumentsInUserMessage(context.Background(), ws, in, 120)

	if !strings.Contains(out, "... (truncated)") {
		t.Fatalf("expected truncated marker, got: %s", out)
	}
	if !strings.Contains(out, "Further attachments omitted: message size limit.") {
		t.Fatalf("expected omission marker, got: %s", out)
	}
}

