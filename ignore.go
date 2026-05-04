package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"

	gitignore "github.com/git-pkgs/gitignore"
)

// ignoreSet wraps a single global git-pkgs/gitignore matcher with an RWMutex.
// The walker calls ensureNode for every directory it visits, which appends
// that directory's .gitignore/.ignore patterns to the matcher (with the dir's
// relative path as their scope). match() does a read-locked lookup.
type ignoreSet struct {
	root    string
	mu      sync.RWMutex
	matcher *gitignore.Matcher
	seen    sync.Map // string (rel dir) -> struct{} ; ensures each dir is loaded once
}

func newIgnoreSet(root string) *ignoreSet {
	s := &ignoreSet{
		root:    root,
		matcher: gitignore.New(""),
	}
	s.loadDir(root, "")
	s.seen.Store("", struct{}{})
	return s
}

// ensureNode loads relDir's .gitignore/.ignore (if any) into the global
// matcher. Idempotent. The walker calls this for every directory it visits.
func (s *ignoreSet) ensureNode(relDir string) {
	if relDir == "." || relDir == "" {
		return
	}
	if _, ok := s.seen.Load(relDir); ok {
		return
	}
	if _, loaded := s.seen.LoadOrStore(relDir, struct{}{}); loaded {
		return
	}
	s.loadDir(filepath.Join(s.root, relDir), relDir)
}

func (s *ignoreSet) loadDir(absDir, relDir string) {
	for _, name := range []string{".gitignore", ".ignore"} {
		data, err := os.ReadFile(filepath.Join(absDir, name))
		if err != nil {
			continue
		}
		data = stripGitjoinSection(data)
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		s.mu.Lock()
		s.matcher.AddPatterns(data, relDir)
		s.mu.Unlock()
	}
}

// match reports whether rel (relative to root, OS-native separators) is ignored.
func (s *ignoreSet) match(rel string, isDir bool) bool {
	if rel == "" || rel == "." {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.matcher.MatchPath(filepath.ToSlash(rel), isDir)
}

// stripGitjoinSection removes any lines between
// "# Managed by gitjoin - do not edit this section" and
// "# End gitjoin managed section" so that gitjoin-managed entries (which point
// at subrepos the user wants searched) are NOT treated as ignore rules.
func stripGitjoinSection(data []byte) []byte {
	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	in := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Managed by gitjoin") {
			in = true
			continue
		}
		if strings.Contains(line, "End gitjoin managed section") {
			in = false
			continue
		}
		if in {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}
