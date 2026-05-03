package main

import (
	"bytes"
	"fmt"
	"regexp"
	"regexp/syntax"
)

// matcher decides whether a line matches the user's pattern. It supports three
// regimes: pure fixed-string (re == nil), regex with a literal pre-filter, and
// regex without one.
type matcher struct {
	re      *regexp.Regexp
	literal []byte
}

func (m *matcher) match(line []byte) bool {
	if m.re == nil {
		return bytes.Contains(line, m.literal)
	}
	if len(m.literal) > 0 && !bytes.Contains(line, m.literal) {
		return false
	}
	return m.re.Match(line)
}

func compileMatcher(pattern string, fixedString bool) (*matcher, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	if fixedString {
		return &matcher{literal: []byte(pattern)}, nil
	}
	tree, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, err
	}
	literal := extractLiteral(tree)

	// If the entire pattern is a single literal, skip the regex engine.
	if isPureLiteral(tree) {
		return &matcher{literal: []byte(pattern)}, nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &matcher{re: re, literal: literal}, nil
}

func isPureLiteral(re *syntax.Regexp) bool {
	return re.Op == syntax.OpLiteral
}

// extractLiteral walks the regex AST and returns the longest literal substring
// that must appear in any match. Returns nil when no such guarantee can be
// made (e.g. top-level alternation, optional groups only).
func extractLiteral(re *syntax.Regexp) []byte {
	switch re.Op {
	case syntax.OpLiteral:
		return []byte(string(re.Rune))

	case syntax.OpConcat:
		// All children are required; pick the longest literal among them.
		var best []byte
		for _, sub := range re.Sub {
			if cand := extractLiteral(sub); len(cand) > len(best) {
				best = cand
			}
		}
		return best

	case syntax.OpCapture:
		if len(re.Sub) == 1 {
			return extractLiteral(re.Sub[0])
		}

	case syntax.OpPlus:
		// x+ requires at least one x.
		if len(re.Sub) == 1 {
			return extractLiteral(re.Sub[0])
		}

	case syntax.OpRepeat:
		// {n,m} with n >= 1 requires at least one match.
		if re.Min >= 1 && len(re.Sub) == 1 {
			return extractLiteral(re.Sub[0])
		}
	}
	// OpAlternate, OpStar, OpQuest, OpCharClass, OpAnyChar(NotNL), anchors,
	// empty matches: not guaranteed to contain any specific literal.
	return nil
}
