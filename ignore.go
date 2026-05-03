package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// ignoreSet caches per-directory gitignore patterns for a single search root.
// The walker populates the cache for each directory it visits via ensureNode;
// match() walks up from a path's parent directory to the root, applying each
// level's patterns to implement gitignore semantics (the deepest-matching
// pattern wins; within a level, the last pattern wins).
type ignoreSet struct {
	root  string
	cache sync.Map // string (rel dir, "" for root) -> *ignoreNode
}

type ignoreNode struct {
	ownPatterns []gitignore.Pattern // patterns from this dir's .gitignore + .ignore
}

func newIgnoreSet(root string) *ignoreSet {
	s := &ignoreSet{root: root}
	s.cache.Store("", &ignoreNode{
		ownPatterns: readDirPatterns(root, nil),
	})
	return s
}

// ensureNode reads relDir's .gitignore/.ignore and caches the patterns there.
// Idempotent. The walker calls this for every directory it visits.
func (s *ignoreSet) ensureNode(relDir string) {
	if relDir == "." || relDir == "" {
		return
	}
	if _, ok := s.cache.Load(relDir); ok {
		return
	}
	domain := strings.Split(filepath.ToSlash(relDir), "/")
	s.cache.LoadOrStore(relDir, &ignoreNode{
		ownPatterns: readDirPatterns(filepath.Join(s.root, relDir), domain),
	})
}

// match reports whether rel (relative to root) is ignored. Walks from rel's
// parent directory up to the root, consulting each level's patterns in
// reverse order. Returns at the first non-NoMatch result, which is also the
// deepest-matching one.
func (s *ignoreSet) match(rel string, isDir bool) bool {
	if rel == "" || rel == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")

	cur := filepath.Dir(rel)
	if cur == "." || cur == rel {
		cur = ""
	}
	for {
		if v, ok := s.cache.Load(cur); ok {
			n := v.(*ignoreNode)
			for i := len(n.ownPatterns) - 1; i >= 0; i-- {
				if r := n.ownPatterns[i].Match(parts, isDir); r != gitignore.NoMatch {
					return r == gitignore.Exclude
				}
			}
		}
		if cur == "" {
			return false
		}
		next := filepath.Dir(cur)
		if next == "." || next == cur {
			next = ""
		}
		cur = next
	}
}

// readDirPatterns reads .gitignore and .ignore from absDir, parses each non-empty,
// non-comment line as a gitignore.Pattern with the given domain, and returns them.
// Lines between "# Managed by gitjoin" markers are skipped.
func readDirPatterns(absDir string, domain []string) []gitignore.Pattern {
	var patterns []gitignore.Pattern
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
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			patterns = append(patterns, gitignore.ParsePattern(trimmed, domain))
		}
		f.Close()
	}
	return patterns
}
