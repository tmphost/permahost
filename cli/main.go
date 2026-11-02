// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Command tmphost uploads files to temporary public folders on a tmphost
// server.
package main

import (
	"os"
	"time"

	"github.com/tmphost/tmphost/cli/internal/cmd"
	"github.com/tmphost/tmphost/cli/internal/oauth"
)

// version is set at release time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

// exit is swapped in tests so main can run without ending the test binary.
var exit = os.Exit

func main() {
	exit(cmd.Execute(os.Args[1:], cmd.Options{
		Out:         os.Stdout,
		Err:         os.Stderr,
		Getenv:      os.Getenv,
		Now:         time.Now,
		OpenBrowser: oauth.OpenBrowser,
		Version:     version,
	}))
}
