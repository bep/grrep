// Copyright 2026 Bjørn Erik Pedersen
// SPDX-License-Identifier: Apache-2.0

package internal

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/bep/gogitignore"
)

// IgnoreSet wraps a gogitignore.Tree. The walker calls ensureNode for each
// directory it visits; the Tree internally keeps per-dir matchers so match()
// only iterates patterns from the path's ancestor chain.
type IgnoreSet struct {
	root string
	tree *gogitignore.Tree
}

func NewIgnoreSet(root string) *IgnoreSet {
	s := &IgnoreSet{
		root: root,
		tree: gogitignore.New(),
	}
	s.LoadDir(root, "")
	return s
}

// EnsureNode loads relDir's .gitignore/.ignore (if any) into the tree.
// Called once per directory by the walker.
func (s *IgnoreSet) EnsureNode(relDir string) {
	if relDir == "." || relDir == "" {
		return
	}
	s.LoadDir(filepath.Join(s.root, relDir), relDir)
}

func (s *IgnoreSet) LoadDir(absDir, relDir string) {
	var lines []string
	for _, name := range []string{".gitignore", ".ignore"} {
		f, err := os.Open(filepath.Join(absDir, name))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		inGitjoin := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Managed by gitjoin") {
				inGitjoin = true
				continue
			}
			if strings.Contains(line, "End gitjoin managed section") {
				inGitjoin = false
				continue
			}
			if inGitjoin {
				continue
			}
			lines = append(lines, line)
		}
		f.Close()
	}
	if len(lines) == 0 {
		return
	}
	treePath := "/"
	if relDir != "" {
		treePath = "/" + filepath.ToSlash(relDir)
	}
	s.tree.InsertPatterns(treePath, lines...)
}

// Match reports whether rel (relative to root) is ignored.
func (s *IgnoreSet) Match(rel string, isDir bool) bool {
	if rel == "" || rel == "." {
		return false
	}
	return s.tree.Match("/"+filepath.ToSlash(rel), isDir)
}
