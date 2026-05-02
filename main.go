package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/charlievieth/fastwalk"
	"golang.org/x/sync/errgroup"
)

const (
	peekSize = 8000
)

var readerPool = sync.Pool{
	New: func() any {
		return bufio.NewReader(nil)
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
	pattern    []byte
	root       string
	quiet      bool
	ctx        context.Context
	paths      chan string
	results    chan []byte
	numWorkers int
}

func run() (bool, error) {
	var (
		quiet    bool
		noIgnore bool
	)
	flag.BoolVar(&quiet, "q", false, "quiet: suppress match output")
	flag.BoolVar(&noIgnore, "no-ignore", false, "do not respect ignore files (currently always on)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		return false, fmt.Errorf("usage: mygrep [-q] [--no-ignore] PATTERN [PATH]")
	}
	root := "."
	if len(args) >= 2 {
		root = args[1]
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eg, gCtx := errgroup.WithContext(ctx)

	g := &grepper{
		pattern:    []byte(args[0]),
		root:       root,
		quiet:      quiet,
		ctx:        gCtx,
		paths:      make(chan string, 256),
		results:    make(chan []byte, 64),
		numWorkers: max(runtime.NumCPU()/2, 2),
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
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
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
			if bytes.Contains(line, g.pattern) {
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
