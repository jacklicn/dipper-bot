package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// DefaultMaxEmbeddedAttachmentBytes is the cap for MarkItDown-appended content when agents.defaults.attachments.maxEmbeddedBytes is 0 or unset.
const DefaultMaxEmbeddedAttachmentBytes = 400_000

// EffectiveMaxEmbeddedBytes returns n if n > 0, otherwise DefaultMaxEmbeddedAttachmentBytes.
func EffectiveMaxEmbeddedBytes(n int) int {
	if n <= 0 {
		return DefaultMaxEmbeddedAttachmentBytes
	}
	return n
}

// attachmentPrefixes are recognized at the start of a trimmed line (web UI uses [附件]).
var attachmentPrefixes = []string{"[附件]", "[attachment]"}

// ResolveWorkspaceRelativePath returns an absolute path under workspace, or an error if unsafe.
func ResolveWorkspaceRelativePath(workspace, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	rel = filepath.ToSlash(rel)
	if rel == "" || strings.Contains(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed")
	}
	ws, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	if abs != ws && !strings.HasPrefix(abs, ws+sep) {
		return "", fmt.Errorf("path outside workspace")
	}
	return abs, nil
}

func parseAttachmentPaths(content string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range attachmentPrefixes {
			if strings.HasPrefix(line, prefix) {
				rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				if rest == "" {
					continue
				}
				parts := strings.Split(rest, ",")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						if _, ok := seen[p]; ok {
							continue
						}
						seen[p] = struct{}{}
						out = append(out, p)
					}
				}
				break
			}
		}
	}
	return out
}

// ExpandDocumentsInUserMessage finds attachment path lines (e.g. "[附件] uploads/a.pdf") and appends
// MarkItDown-converted Markdown so the LLM receives document text without relying on read_file.
// Non-document paths are skipped (e.g. images). On conversion failure, an error section is appended.
// maxEmbeddedBytes is the total byte budget for appended Markdown (use EffectiveMaxEmbeddedBytes from config).
func ExpandDocumentsInUserMessage(ctx context.Context, workspace, content string, maxEmbeddedBytes int) string {
	if strings.TrimSpace(workspace) == "" {
		return content
	}
	maxEmbeddedBytes = EffectiveMaxEmbeddedBytes(maxEmbeddedBytes)
	paths := parseAttachmentPaths(content)
	if len(paths) == 0 {
		return content
	}
	var b strings.Builder
	remaining := maxEmbeddedBytes
	for _, rel := range paths {
		abs, err := ResolveWorkspaceRelativePath(workspace, rel)
		if err != nil {
			sect := fmt.Sprintf("### %s\n\n*(skipped: %v)*\n\n", rel, err)
			if len(sect) > remaining {
				break
			}
			b.WriteString(sect)
			remaining -= len(sect)
			continue
		}
		if !needsMarkitdownConversion(abs) {
			continue
		}
		body, err := runMarkItDownBody(ctx, abs, workspace)
		if err != nil {
			sect := fmt.Sprintf("### %s\n\n*(MarkItDown failed: %v)*\n\n", rel, err)
			if len(sect) > remaining {
				break
			}
			b.WriteString(sect)
			remaining -= len(sect)
			continue
		}
		header := "### " + rel + "\n\n"
		need := len(header) + len(body)
		if need <= remaining {
			b.WriteString(header)
			b.WriteString(body)
			b.WriteString("\n\n")
			remaining -= need
			continue
		}
		if remaining <= len(header)+100 {
			b.WriteString("\n*(Further attachments omitted: message size limit.)*\n")
			break
		}
		take := remaining - len(header) - 80
		if take < 0 {
			take = 0
		}
		if take > len(body) {
			take = len(body)
		}
		b.WriteString(header)
		b.WriteString(body[:take])
		b.WriteString("\n\n... (truncated)\n\n")
		b.WriteString("*(Further attachments omitted: message size limit.)*\n")
		break
	}
	if b.Len() == 0 {
		return content
	}
	return content + "\n\n---\n\n## Attached documents (converted to Markdown)\n\n" + b.String()
}
