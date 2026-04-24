package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed workspace
var onboardWorkspaceFS embed.FS

//go:embed skills
var onboardSkillsFS embed.FS

// skipSkillsPackageGo skips the skills Go package entrypoints (not agent skill content).
func skipSkillsPackageGo(relSlash string) bool {
	dir := filepath.ToSlash(filepath.Dir(relSlash))
	if dir != "." {
		return false
	}
	base := filepath.Base(relSlash)
	return base == "embed.go" || base == "extract.go"
}

// copyOnboardTree walks fsys rooted at prefix (e.g. "workspace") and writes files under destRoot.
// rel paths use slash; onlyIfMissing avoids overwriting existing files.
func copyOnboardTree(fsys embed.FS, prefix, destRoot string, onlyIfMissing bool, skip func(relSlash string) bool) (created int, err error) {
	prefix = strings.TrimPrefix(prefix, "./")
	err = fs.WalkDir(fsys, prefix, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == prefix {
			if d.IsDir() {
				return os.MkdirAll(destRoot, 0o750)
			}
			return nil
		}
		rel, ok := strings.CutPrefix(path, prefix+"/")
		if !ok {
			return fmt.Errorf("onboard: unexpected path %q (expected under %q/)", path, prefix)
		}
		if skip != nil && skip(rel) {
			return nil
		}
		dst := filepath.Join(destRoot, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o750)
		}
		if onlyIfMissing {
			if _, st := os.Stat(dst); st == nil {
				return nil
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		data, err := fsys.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		created++
		return nil
	})
	return created, err
}

func seedOnboardWorkspaceAndSkills(workspace string, onlyIfMissing bool) error {
	nw, err := copyOnboardTree(onboardWorkspaceFS, "workspace", workspace, onlyIfMissing, nil)
	if err != nil {
		return fmt.Errorf("workspace template: %w", err)
	}
	skillsDir := filepath.Join(workspace, "skills")
	if err := os.MkdirAll(skillsDir, 0o750); err != nil {
		return err
	}
	ns, err := copyOnboardTree(onboardSkillsFS, "skills", skillsDir, onlyIfMissing, skipSkillsPackageGo)
	if err != nil {
		return fmt.Errorf("skills template: %w", err)
	}
	fmt.Printf("  Seeded %d file(s) from builtin workspace/ → %s\n", nw, workspace)
	fmt.Printf("  Seeded %d file(s) from builtin skills/ → %s\n", ns, skillsDir)
	return nil
}
