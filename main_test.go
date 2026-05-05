// Copyright 2026 Bjørn Erik Pedersen
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testscripts",
	})
}

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"mygrep": main,
	})
}
