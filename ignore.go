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
	parent      *ignoreNode         // ancestor; used by match() to walk up in O(depth)
	ownPatterns []gitignore.Pattern // patterns from this dir's .gitignore + .ignore
	// ownDirOnly[i] is true when ownPatterns[i]'s source line ended with `/`,
	// i.e. it can only match directories. We skip those when matching files.
	ownDirOnly []bool
}

func newIgnoreSet(root string) *ignoreSet {
	s := &ignoreSet{root: root}
	patterns, dirOnly := readDirPatterns(root, nil)
	s.cache.Store("", &ignoreNode{
		ownPatterns: patterns,
		ownDirOnly:  dirOnly,
	})
	return s
}

// ensureNode reads relDir's .gitignore/.ignore and caches the patterns there.
// Idempotent. The walker calls this for every directory it visits, and
// because fastwalk visits parents before children, the parent's node is
// always already in the cache by the time we get here.
func (s *ignoreSet) ensureNode(relDir string) {
	if relDir == "." || relDir == "" {
		return
	}
	if _, ok := s.cache.Load(relDir); ok {
		return
	}
	parentRel := filepath.Dir(relDir)
	if parentRel == "." || parentRel == relDir {
		parentRel = ""
	}
	parentVal, _ := s.cache.Load(parentRel)
	parent, _ := parentVal.(*ignoreNode) // nil-safe: zero value is fine if missing
	domain := strings.Split(filepath.ToSlash(relDir), "/")
	patterns, dirOnly := readDirPatterns(filepath.Join(s.root, relDir), domain)
	s.cache.LoadOrStore(relDir, &ignoreNode{
		parent:      parent,
		ownPatterns: patterns,
		ownDirOnly:  dirOnly,
	})
}

// match reports whether rel (relative to root) is ignored. Looks up rel's
// parent directory once, then walks up via the node parent pointer chain,
// consulting each level's patterns in reverse order. Returns at the first
// non-NoMatch result, which is also the deepest-matching one.
func (s *ignoreSet) match(rel string, isDir bool) bool {
	if rel == "" || rel == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	parentRel := filepath.Dir(rel)
	if parentRel == "." || parentRel == rel {
		parentRel = ""
	}
	v, ok := s.cache.Load(parentRel)
	if !ok {
		return false
	}
	for n := v.(*ignoreNode); n != nil; n = n.parent {
		for i := len(n.ownPatterns) - 1; i >= 0; i-- {
			if !isDir && n.ownDirOnly[i] {
				continue // dir-only pattern can never match a file
			}
			if r := n.ownPatterns[i].Match(parts, isDir); r != gitignore.NoMatch {
				return r == gitignore.Exclude
			}
		}
	}
	return false
}

// readDirPatterns reads .gitignore and .ignore from absDir, parses each non-empty,
// non-comment line as a gitignore.Pattern with the given domain, and returns the
// parallel slices (patterns, dir-only flags). Lines between
// "# Managed by gitjoin" markers are skipped. A pattern is dir-only when its
// source line ends with `/`.
func readDirPatterns(absDir string, domain []string) ([]gitignore.Pattern, []bool) {
	var (
		patterns []gitignore.Pattern
		dirOnly  []bool
	)
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
			dirOnly = append(dirOnly, strings.HasSuffix(trimmed, "/"))
		}
		f.Close()
	}
	return patterns, dirOnly
}
