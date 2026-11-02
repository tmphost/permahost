// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMain_exitCode(t *testing.T) {
	t.Setenv("TMPHOSTIO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	origArgs, origExit := os.Args, exit
	t.Cleanup(func() { os.Args, exit = origArgs, origExit })

	var code int
	exit = func(c int) { code = c }

	os.Args = []string{"tmphost", "logout"}
	main()
	assert.Equal(t, 0, code)

	os.Args = []string{"tmphost", "no-such-command"}
	main()
	assert.Equal(t, 1, code)
}
