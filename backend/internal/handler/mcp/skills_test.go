// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const _skillURI = "skill://permahost/SKILL.md"

// params2026 are request params for the 2026-07-28 protocol, with the
// request _meta it requires plus fields (a JSON object's members).
func params2026(fields string) string {
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}`
	if fields == "" {
		return "{" + meta + "}"
	}
	return "{" + meta + "," + fields + "}"
}

func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestSkillsList(t *testing.T) {
	body, err := _skillFiles.ReadFile("skills/permahost/SKILL.md")
	require.NoError(t, err)

	rs := rpc(t, "2026-07-28", "skills/list", params2026(""))
	assert.Equal(t, "complete", rs["resultType"])
	assert.Equal(t, float64(3600000), rs["ttlMs"])
	assert.Equal(t, "public", rs["cacheScope"])
	require.Len(t, rs["skills"], 1)
	skill := rs["skills"].([]any)[0].(map[string]any)
	assert.Equal(t, _skillURI, skill["uri"])
	fm := skill["frontmatter"].(map[string]any)
	assert.Equal(t, "permahost", fm["name"])
	assert.NotEmpty(t, fm["description"])
	assert.Equal(t, []any{map[string]any{"uri": _skillURI, "digest": digest(body), "size": float64(len(body))}}, skill["resources"], "the manifest matches the bytes served")
}

func TestSkillsGet(t *testing.T) {
	rs := rpc(t, "2026-07-28", "skills/get", params2026(`"uri":"`+_skillURI+`"`))
	assert.Equal(t, "complete", rs["resultType"])
	assert.Equal(t, float64(3600000), rs["ttlMs"])
	assert.Equal(t, "public", rs["cacheScope"])
	assert.Equal(t, _skillURI, rs["skill"].(map[string]any)["uri"])

	_, rpcErr := rpcCall(t, "2026-07-28", "skills/get", params2026(`"uri":"skill://nope/SKILL.md"`))
	require.NotNil(t, rpcErr)
	assert.Equal(t, float64(-32602), rpcErr["code"], "an unknown skill is invalid params")
}

func TestDirectoryRead(t *testing.T) {
	body, err := _skillFiles.ReadFile("skills/permahost/SKILL.md")
	require.NoError(t, err)

	rs := rpc(t, "2026-07-28", "resources/directory/read", params2026(`"uri":"skill://permahost"`))
	assert.Equal(t, "complete", rs["resultType"])
	assert.Equal(t, float64(3600000), rs["ttlMs"])
	assert.Equal(t, "public", rs["cacheScope"])
	assert.Equal(t, []any{map[string]any{"uri": _skillURI, "name": "permahost/SKILL.md", "description": _skills.entries[0].Frontmatter["description"], "mimeType": "text/markdown", "size": float64(len(body))}}, rs["resources"])
	assert.NotContains(t, rs, "nextCursor", "every child fits on one page")

	for _, uri := range []string{_skillURI, "skill://nope", "skill://permahost/", "skill:"} {
		_, rpcErr := rpcCall(t, "2026-07-28", "resources/directory/read", params2026(`"uri":"`+uri+`"`))
		require.NotNil(t, rpcErr, uri)
		assert.Equal(t, float64(-32602), rpcErr["code"], "%s is not a directory: invalid params", uri)
	}
}

// The SKILL.md is a resource; a client with a session reads it the same way.
func TestSkillResource(t *testing.T) {
	e := newEnv(t)
	list, err := e.session.ListResources(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, list.Resources, 1)
	assert.Equal(t, _skillURI, list.Resources[0].URI)
	assert.Equal(t, "permahost/SKILL.md", list.Resources[0].Name)
	assert.Equal(t, _skills.entries[0].Frontmatter["description"], list.Resources[0].Description, "the SKILL.md resource has the skill's description")
	assert.NotEmpty(t, list.Resources[0].Description)
	assert.Equal(t, "text/markdown", list.Resources[0].MIMEType)

	rs, err := e.session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: _skillURI})
	require.NoError(t, err)
	body, _ := _skillFiles.ReadFile("skills/permahost/SKILL.md")
	require.Len(t, rs.Contents, 1)
	assert.Equal(t, string(body), rs.Contents[0].Text)
	assert.Equal(t, "text/markdown", rs.Contents[0].MIMEType)
	assert.Equal(t, 3600000, rs.TTLMs)
	assert.Equal(t, "public", rs.CacheScope)
}

func TestLoadSkills_SupportingFiles(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0xff}
	set, err := loadSkills(fstest.MapFS{
		"s/a/SKILL.md":              {Data: []byte("---\nname: a\ndescription: d\nlicense: MIT\n---\n# A\n")},
		"s/a/references/guide.md":   {Data: []byte("guide")},
		"s/a/assets/logo.png":       {Data: png},
		"s/a/assets/data.zzunknown": {Data: []byte("x")},
	}, "s")
	require.NoError(t, err)
	require.Len(t, set.entries, 1)
	entry := set.entries[0]
	assert.Equal(t, map[string]any{"name": "a", "description": "d", "license": "MIT"}, entry.Frontmatter, "every frontmatter field, unchanged")
	var uris []string
	for _, r := range entry.Resources {
		uris = append(uris, r.URI)
	}
	assert.Equal(t, []string{"skill://a/SKILL.md", "skill://a/assets/data.zzunknown", "skill://a/assets/logo.png", "skill://a/references/guide.md"}, uris, "SKILL.md first")

	dirs := map[string][]string{}
	for uri, children := range set.dirs {
		for _, c := range children {
			dirs[uri] = append(dirs[uri], c.URI+" "+c.MIMEType)
		}
	}
	assert.Equal(t, map[string][]string{
		"skill://a":            {"skill://a/SKILL.md text/markdown", "skill://a/assets inode/directory", "skill://a/references inode/directory"},
		"skill://a/assets":     {"skill://a/assets/data.zzunknown application/octet-stream", "skill://a/assets/logo.png image/png"},
		"skill://a/references": {"skill://a/references/guide.md text/markdown"},
	}, dirs, "every directory, the root included, lists its direct children in URI order")
	assert.Equal(t, "a/assets", set.dirs["skill://a"][1].Name)

	descriptions := map[string]string{}
	for _, children := range set.dirs {
		for _, c := range children {
			descriptions[c.URI] = c.Description
		}
	}
	assert.Equal(t, map[string]string{
		"skill://a/SKILL.md":              "d",
		"skill://a/assets":                "The assets directory of the a skill.",
		"skill://a/references":            "The references directory of the a skill.",
		"skill://a/assets/data.zzunknown": "The assets/data.zzunknown file of the a skill.",
		"skill://a/assets/logo.png":       "The assets/logo.png file of the a skill.",
		"skill://a/references/guide.md":   "The references/guide.md file of the a skill.",
	}, descriptions, "every resource has a description")
	assert.Equal(t, "d", entry.describe("skill://a", "directory"), "the skill root has the skill's description")

	for uri, want := range map[string]string{
		"skill://a/references/guide.md":   "text/markdown",
		"skill://a/assets/logo.png":       "image/png",
		"skill://a/assets/data.zzunknown": "application/octet-stream",
	} {
		assert.Equal(t, want, skillMIMEType(uri), uri)
	}

	rs, err := set.read(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "skill://a/assets/logo.png"}})
	require.NoError(t, err)
	assert.Equal(t, png, rs.Contents[0].Blob, "binary files are blobs")
	assert.Empty(t, rs.Contents[0].Text)
}

// failFS fails to open one path. It has only Open, so every read goes
// through it.
type failFS struct {
	fsys fs.FS
	path string
}

func (f failFS) Open(name string) (fs.File, error) {
	if name == f.path {
		return nil, errors.New("boom")
	}
	return f.fsys.Open(name)
}

func TestLoadSkills_Errors(t *testing.T) {
	skill := func(body string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(body)} }
	valid := skill("---\nname: a\ndescription: d\n---\n")
	for name, tc := range map[string]struct {
		fsys fs.FS
		want string
	}{
		"no root":           {fstest.MapFS{}, "open s"},
		"file at the root":  {fstest.MapFS{"s/x.md": skill("x")}, "x.md: not a skill directory"},
		"no SKILL.md":       {fstest.MapFS{"s/a/other.md": skill("x")}, "skill a: open s/a/SKILL.md"},
		"no frontmatter":    {fstest.MapFS{"s/a/SKILL.md": skill("# A\n")}, "must start with YAML frontmatter"},
		"unclosed":          {fstest.MapFS{"s/a/SKILL.md": skill("---\nname: a\n")}, "no closing ---"},
		"bad yaml":          {fstest.MapFS{"s/a/SKILL.md": skill("---\nname: [a\n---\n")}, "SKILL.md frontmatter:"},
		"name mismatch":     {fstest.MapFS{"s/a/SKILL.md": skill("---\nname: b\ndescription: d\n---\n")}, `frontmatter name must be "a"`},
		"no description":    {fstest.MapFS{"s/a/SKILL.md": skill("---\nname: a\n---\n")}, "needs a description"},
		"unreadable file":   {failFS{fstest.MapFS{"s/a/SKILL.md": valid, "s/a/ref.md": skill("x")}, "s/a/ref.md"}, "boom"},
		"unreadable subdir": {failFS{fstest.MapFS{"s/a/SKILL.md": valid, "s/a/refs/r.md": skill("x")}, "s/a/refs"}, "boom"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadSkills(tc.fsys, "s")
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMustLoadSkills(t *testing.T) {
	assert.Equal(t, _skills.entries, mustLoadSkills(_skillFiles).entries)
	assert.Panics(t, func() { mustLoadSkills(fstest.MapFS{}) })
}
