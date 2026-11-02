// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the user's default browser.
func OpenBrowser(url string) error {
	return browserCommand(runtime.GOOS, url).Start()
}

// browserCommand is the platform's way to open a URL.
func browserCommand(goos, url string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		// "start" would treat & in the URL as a command separator.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return exec.Command("xdg-open", url)
	}
}
