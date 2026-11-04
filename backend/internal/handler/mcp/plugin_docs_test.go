// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/permahost/backend/internal/controller/credit"
)

// The plugins under plugin/ describe this server's tools to the agent; check
// they don't drift from what it registers.

// _pluginMCPURL is where every plugin connects.
const _pluginMCPURL = "https://mcp.permahost.io/mcp"

// A leading table cell holding a backticked identifier.
var _docRowRe = regexp.MustCompile("(?m)^\\| `([a-zA-Z]+)`")

// _pluginDocs are the files that teach an agent the tools.
var _pluginDocs = []string{
	"backend/internal/handler/mcp/skills/permahost/SKILL.md",
	"plugin/claude/skills/permahost/SKILL.md",
	"plugin/codex/skills/permahost/SKILL.md",
	"plugin/codex/README.md",
	"plugin/cursor/skills/permahost/SKILL.md",
	"plugin/cursor/rules/prefer-permahost-mcp.mdc",
	"plugin/cursor/README.md",
	"plugin/gemini/GEMINI.md",
	"plugin/gemini/README.md",
}

// registeredTools names the tools the server lists.
func registeredTools(t *testing.T) []string {
	t.Helper()
	rs, err := newEnv(t).session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := make([]string, len(rs.Tools))
	for i, tool := range rs.Tools {
		names[i] = tool.Name
	}
	return names
}

// repoRoot walks up from the test's directory to the one holding the
// Gemini CLI extension's manifest.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "gemini-extension.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no gemini-extension.json above the test")
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func TestPluginDocsListExactlyTheRegisteredTools(t *testing.T) {
	var documented []string
	for _, m := range _docRowRe.FindAllStringSubmatch(readFile(t, filepath.Join(repoRoot(t), "plugin", "gemini", "README.md")), -1) {
		documented = append(documented, m[1])
	}
	assert.ElementsMatch(t, registeredTools(t), documented)
}

func TestPluginDocsNameEveryTool(t *testing.T) {
	root := repoRoot(t)
	registered := registeredTools(t)
	for _, doc := range _pluginDocs {
		t.Run(doc, func(t *testing.T) {
			text := readFile(t, filepath.Join(root, doc))
			for _, tool := range registered {
				assert.Contains(t, text, "`"+tool+"`", "%s doesn't document the %q tool", doc, tool)
			}
			assert.NotContains(t, text, "`file` tool", "%s still names the old file tool", doc)
		})
	}
}

// The skills quote the prices; they must be the ones the server charges.
func TestPluginDocsQuoteThePrices(t *testing.T) {
	root := repoRoot(t)
	folder := fmt.Sprintf("A new folder costs %d credits a month", credit.FolderCredits)
	storage := fmt.Sprintf("Storage costs %d credits a month per started %d MB", credit.StorageCredits, credit.StorageBlockBytes>>20)
	for _, doc := range []string{
		"backend/internal/handler/mcp/skills/permahost/SKILL.md",
		"plugin/claude/skills/permahost/SKILL.md",
		"plugin/codex/skills/permahost/SKILL.md",
		"plugin/cursor/skills/permahost/SKILL.md",
		"plugin/gemini/GEMINI.md",
	} {
		t.Run(doc, func(t *testing.T) {
			// Line breaks in the Markdown don't matter.
			text := strings.Join(strings.Fields(readFile(t, filepath.Join(root, doc))), " ")
			assert.Contains(t, text, folder)
			assert.Contains(t, text, storage)
			assert.Contains(t, text, "never costs folder credits again")
		})
	}
}

// Every plugin connects to the same server, under the same name.
func TestPluginMCPServers(t *testing.T) {
	root := repoRoot(t)
	for _, manifest := range []string{
		"plugin/claude/.claude-plugin/plugin.json",
		"plugin/codex/mcp.json",
		"plugin/cursor/mcp.json",
		"gemini-extension.json",
	} {
		t.Run(manifest, func(t *testing.T) {
			var m struct {
				MCPServers map[string]struct {
					URL string `json:"url"`
				} `json:"mcpServers"`
			}
			require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(root, manifest))), &m))
			require.Len(t, m.MCPServers, 1)
			assert.Equal(t, _pluginMCPURL, m.MCPServers["permahost"].URL)
		})
	}
}

// The review team tests the Codex plugin with the cases in its manifest; each
// names the tools it expects, which must be ones the server registers.
func TestCodexReviewCasesNameRegisteredTools(t *testing.T) {
	var manifest struct {
		Extensions struct {
			OpenAI struct {
				Review struct {
					TestCases struct {
						Positive []struct {
							ToolsTriggered string `json:"tools_triggered"`
						} `json:"positive"`
					} `json:"test_cases"`
				} `json:"review"`
			} `json:"com.openai"`
		} `json:"extensions"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(repoRoot(t), "plugin", "codex", "plugin.json"))), &manifest))

	tools := registeredTools(t)
	cases := manifest.Extensions.OpenAI.Review.TestCases.Positive
	require.NotEmpty(t, cases)
	for i, c := range cases {
		for _, tool := range strings.Split(c.ToolsTriggered, ",") {
			assert.Contains(t, tools, strings.TrimSpace(tool), "positive case %d expects a tool the server doesn't register", i)
		}
	}
}

// The plugin directories list the plugin with a square PNG icon, 512 to 2048
// px a side, under 2 MB.
func TestPluginIcons(t *testing.T) {
	root := repoRoot(t)
	for _, icon := range []string{"plugin/claude/.claude-plugin/icon.png", "plugin/codex/assets/icon.png"} {
		t.Run(icon, func(t *testing.T) {
			f, err := os.Open(filepath.Join(root, icon))
			require.NoError(t, err)
			defer func() { _ = f.Close() }()
			cfg, format, err := image.DecodeConfig(f)
			require.NoError(t, err)
			assert.Equal(t, "png", format)
			assert.Equal(t, cfg.Width, cfg.Height, "square")
			assert.GreaterOrEqual(t, cfg.Width, 512)
			assert.LessOrEqual(t, cfg.Width, 2048)
			info, err := f.Stat()
			require.NoError(t, err)
			assert.Less(t, info.Size(), int64(2<<20))
		})
	}
}

// The plugins are Apache-2.0, unlike the AGPL server: every plugin/<client>
// directory carries the license text, and every manifest names it.
func TestPluginLicense(t *testing.T) {
	root := repoRoot(t)
	dirs, err := os.ReadDir(filepath.Join(root, "plugin"))
	require.NoError(t, err)
	require.NotEmpty(t, dirs)
	for _, dir := range dirs {
		t.Run(dir.Name(), func(t *testing.T) {
			text := readFile(t, filepath.Join(root, "plugin", dir.Name(), "LICENSE"))
			assert.Contains(t, text, "Apache License\n                           Version 2.0, January 2004")
		})
	}

	for _, manifest := range []string{"plugin/claude/.claude-plugin/plugin.json", "plugin/codex/plugin.json", "plugin/cursor/.cursor-plugin/plugin.json"} {
		var m struct {
			License string `json:"license"`
		}
		require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(root, manifest))), &m))
		assert.Equal(t, "Apache-2.0", m.License, manifest)
	}
}
