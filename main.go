package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/charlievieth/fastwalk"
	"golang.org/x/sync/errgroup"
)

const (
	peekSize      = 8000
	readerBufSize = 1 << 20 // 1 MiB; bufio fallback for files exceeding scanBufSize.
	scanBufSize   = 1 << 20 // 1 MiB; whole-file pool buffer.
)

var readerPool = sync.Pool{
	New: func() any {
		return bufio.NewReaderSize(nil, readerBufSize)
	},
}

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, scanBufSize)
		return &b
	},
}

func main() {
	found, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !found {
		os.Exit(1)
	}
}

type grepper struct {
	m          *matcher
	root       string
	quiet      bool
	ctx        context.Context
	paths      chan string
	results    chan []byte
	numWorkers int
	ignores    *ignoreSet // nil if --no-ignore
}

func run() (bool, error) {
	var (
		quiet        bool
		noIgnore     bool
		fixedStrings bool
	)
	flag.BoolVar(&quiet, "q", false, "quiet: suppress match output")
	flag.BoolVar(&noIgnore, "no-ignore", false, "do not respect .gitignore/.ignore files")
	flag.BoolVar(&fixedStrings, "F", false, "treat PATTERN as a fixed string, not a regex")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		return false, fmt.Errorf("usage: mygrep [-q] [-F] [--no-ignore] PATTERN [PATH]")
	}
	root := "."
	if len(args) >= 2 {
		root = args[1]
	}

	m, err := compileMatcher(args[0], fixedStrings)
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eg, gCtx := errgroup.WithContext(ctx)

	g := &grepper{
		m:          m,
		root:       root,
		quiet:      quiet,
		ctx:        gCtx,
		paths:      make(chan string, 256),
		results:    make(chan []byte, 64),
		numWorkers: max(runtime.NumCPU()/2, 2),
	}
	if !noIgnore {
		g.ignores = newIgnoreSet(root)
	}

	eg.Go(g.walk)

	eg.Go(func() error {
		var wg sync.WaitGroup
		for i := 0; i < g.numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				g.worker()
			}()
		}
		wg.Wait()
		close(g.results)
		return nil
	})

	found := false
	for buf := range g.results {
		found = true
		if len(buf) > 0 {
			os.Stdout.Write(buf)
		}
		if g.quiet {
			cancel()
			break
		}
	}
	return found, eg.Wait()
}

func (g *grepper) walk() error {
	defer close(g.paths)
	cfg := &fastwalk.Config{NumWorkers: g.numWorkers}
	err := fastwalk.Walk(cfg, g.root, func(path string, d fs.DirEntry, err error) error {
		if g.ctx.Err() != nil {
			return fs.SkipAll
		}
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != g.root && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			if g.ignores != nil && path != g.root {
				if rel, err := filepath.Rel(g.root, path); err == nil && g.ignores.match(rel, true) {
					return fs.SkipDir
				}
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if g.ignores != nil {
			if rel, err := filepath.Rel(g.root, path); err == nil && g.ignores.match(rel, false) {
				return nil
			}
		}
		select {
		case g.paths <- path:
		case <-g.ctx.Done():
			return fs.SkipAll
		}
		return nil
	})
	if errors.Is(err, fs.SkipAll) {
		return nil
	}
	return err
}

func (g *grepper) worker() {
	for p := range g.paths {
		if buf := g.scanFile(p); buf != nil {
			select {
			case g.results <- buf:
			case <-g.ctx.Done():
				return
			}
		}
	}
}

func (g *grepper) scanFile(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	bufp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufp)
	buf := *bufp

	n, err := io.ReadFull(f, buf)
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
		// File fit in the buffer (possibly empty).
		return g.scanWholeBody(path, buf[:n])
	case err == nil:
		// Buffer filled exactly; probe for extra bytes.
		var probe [1]byte
		if m, _ := f.Read(probe[:]); m == 0 {
			// File was exactly scanBufSize.
			return g.scanWholeBody(path, buf)
		}
		// File is larger than the pool buffer; rewind and stream.
		if _, e := f.Seek(0, io.SeekStart); e != nil {
			return nil
		}
		return g.scanFileStream(path, f)
	default:
		return nil
	}
}

// scanWholeBody finds matches by sliding bytes.Index over data. Cheap when a
// file has no matches at all — one bytes.Index call returns -1 and we're done.
func (g *grepper) scanWholeBody(path string, data []byte) []byte {
	headLimit := len(data)
	if headLimit > peekSize {
		headLimit = peekSize
	}
	if bytes.IndexByte(data[:headLimit], 0) >= 0 {
		return nil
	}

	// Pure-regex (no extracted literal): one FindAllIndex over the whole body.
	if g.m.re != nil && len(g.m.literal) == 0 {
		return g.scanWholeRegex(path, data)
	}

	// Literal or literal pre-filter: slide bytes.Index, validate with re if present.
	lit := g.m.literal
	var out bytes.Buffer
	lineNum := 1
	cursor := 0
	for {
		idx := bytes.Index(data[cursor:], lit)
		if idx < 0 {
			break
		}
		matchPos := cursor + idx
		lineNum += bytes.Count(data[cursor:matchPos], []byte{'\n'})
		lineStart := 0
		if i := bytes.LastIndexByte(data[:matchPos], '\n'); i >= 0 {
			lineStart = i + 1
		}
		lineEnd := len(data)
		if i := bytes.IndexByte(data[matchPos:], '\n'); i >= 0 {
			lineEnd = matchPos + i
		}
		line := data[lineStart:lineEnd]
		if g.m.re == nil || g.m.re.Match(line) {
			if g.quiet {
				return []byte{}
			}
			fmt.Fprintf(&out, "%s:%d:%s\n", path, lineNum, line)
		}
		// Advance past this line so we don't re-match on it.
		cursor = lineEnd
		if cursor < len(data) {
			cursor++ // skip the '\n'
			lineNum++
		}
	}
	if out.Len() == 0 {
		return nil
	}
	return out.Bytes()
}

func (g *grepper) scanWholeRegex(path string, data []byte) []byte {
	hits := g.m.re.FindAllIndex(data, -1)
	if len(hits) == 0 {
		return nil
	}
	if g.quiet {
		return []byte{}
	}
	var out bytes.Buffer
	lineNum := 1
	cursor := 0
	prevLineEnd := -1
	for _, h := range hits {
		matchPos := h[0]
		lineNum += bytes.Count(data[cursor:matchPos], []byte{'\n'})
		lineStart := 0
		if i := bytes.LastIndexByte(data[:matchPos], '\n'); i >= 0 {
			lineStart = i + 1
		}
		lineEnd := len(data)
		if i := bytes.IndexByte(data[matchPos:], '\n'); i >= 0 {
			lineEnd = matchPos + i
		}
		// Multiple regex hits can land on the same line — emit the line once.
		if lineEnd != prevLineEnd {
			line := data[lineStart:lineEnd]
			fmt.Fprintf(&out, "%s:%d:%s\n", path, lineNum, line)
			prevLineEnd = lineEnd
		}
		cursor = matchPos
	}
	return out.Bytes()
}

// scanFileStream is the existing bufio fallback used for files larger than
// scanBufSize.
func (g *grepper) scanFileStream(path string, f *os.File) []byte {
	br := readerPool.Get().(*bufio.Reader)
	defer readerPool.Put(br)
	br.Reset(f)

	head, _ := br.Peek(peekSize)
	if bytes.IndexByte(head, 0) >= 0 {
		return nil
	}

	var out bytes.Buffer
	lineNum := 0
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			return nil
		}
		if len(line) > 0 || err == nil {
			lineNum++
			if n := len(line); n > 0 && line[n-1] == '\n' {
				line = line[:n-1]
			}
			if g.m.match(line) {
				if g.quiet {
					return []byte{}
				}
				fmt.Fprintf(&out, "%s:%d:%s\n", path, lineNum, line)
			}
		}
		if err != nil {
			break
		}
	}
	if out.Len() == 0 {
		return nil
	}
	return out.Bytes()
}
