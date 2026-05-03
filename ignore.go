package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// ignoreSet caches per-directory gitignore matchers for a single search root.
type ignoreSet struct {
	root  string
	cache sync.Map // string (rel dir, "" for root) -> *ignoreNode
}

type ignoreNode struct {
	parent       *ignoreNode
	ownPatterns  []gitignore.Pattern // patterns parsed from this dir's .gitignore + .ignore
	once         sync.Once
	fullPatterns []gitignore.Pattern // own + ancestors, root-first; computed lazily
	matcher      gitignore.Matcher
}

func newIgnoreSet(root string) *ignoreSet {
	s := &ignoreSet{root: root}
	rootNode := &ignoreNode{
		ownPatterns: readDirPatterns(root, nil),
	}
	s.cache.Store("", rootNode)
	return s
}

// node returns the ignoreNode for a directory, building ancestors lazily.
// relDir uses OS-native separators; "" or "." means the search root.
func (s *ignoreSet) node(relDir string) *ignoreNode {
	if relDir == "." {
		relDir = ""
	}
	if v, ok := s.cache.Load(relDir); ok {
		return v.(*ignoreNode)
	}
	parentRel := filepath.Dir(relDir)
	if parentRel == "." || parentRel == relDir {
		parentRel = ""
	}
	parent := s.node(parentRel)
	domain := strings.Split(filepath.ToSlash(relDir), "/")
	n := &ignoreNode{
		parent:      parent,
		ownPatterns: readDirPatterns(filepath.Join(s.root, relDir), domain),
	}
	actual, _ := s.cache.LoadOrStore(relDir, n)
	return actual.(*ignoreNode)
}

// match reports whether the path (relative to root, slash-separated) is ignored.
func (s *ignoreSet) match(rel string, isDir bool) bool {
	if rel == "" || rel == "." {
		return false
	}
	parentRel := filepath.Dir(rel)
	if parentRel == "." || parentRel == rel {
		parentRel = ""
	}
	n := s.node(parentRel)
	n.initMatcher()
	if n.matcher == nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return n.matcher.Match(parts, isDir)
}

func (n *ignoreNode) initMatcher() {
	n.once.Do(func() {
		var parentPatterns []gitignore.Pattern
		if n.parent != nil {
			n.parent.initMatcher()
			parentPatterns = n.parent.fullPatterns
		}
		if len(n.ownPatterns) == 0 {
			n.fullPatterns = parentPatterns
			if n.parent != nil {
				n.matcher = n.parent.matcher
				return
			}
		} else {
			n.fullPatterns = make([]gitignore.Pattern, 0, len(parentPatterns)+len(n.ownPatterns))
			n.fullPatterns = append(n.fullPatterns, parentPatterns...)
			n.fullPatterns = append(n.fullPatterns, n.ownPatterns...)
		}
		if len(n.fullPatterns) == 0 {
			return
		}
		n.matcher = gitignore.NewMatcher(n.fullPatterns)
	})
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
