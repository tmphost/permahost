// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

// ExtensionSkills is the MCP Skills extension (SEP-2640): the server lists
// its Agent Skills with skills/list and skills/get, serves their files as
// resources, and lists their directories with resources/directory/read.
const ExtensionSkills = "io.modelcontextprotocol/skills"

// The skills methods.
const (
	MethodSkillsList    = "skills/list"
	MethodSkillsGet     = "skills/get"
	MethodDirectoryRead = "resources/directory/read"
)

// MIMETypeDirectory marks a directory resource.
const MIMETypeDirectory = "inode/directory"

const (
	// _skillsRoot holds one directory per skill, named after it.
	_skillsRoot = "skills"
	// _skillFile is the file every skill has.
	_skillFile = "SKILL.md"
	// _skillScheme prefixes the skill files' resource URIs.
	_skillScheme = "skill://"
	// _skillsTTL is how long clients may cache skill entries and files:
	// they change only with a new release.
	_skillsTTL = time.Hour
	// _resultComplete is the resultType of a final result.
	_resultComplete = "complete"
)

// _skillFiles are the skills the server serves, built into the binary.
//
//go:embed skills
var _skillFiles embed.FS

type (
	// SkillEntry describes a skill: its SKILL.md's URI, the SKILL.md
	// frontmatter as is, and every file in it.
	SkillEntry struct {
		URI         string          `json:"uri"`
		Frontmatter map[string]any  `json:"frontmatter"`
		Resources   []SkillResource `json:"resources"`
	}

	// SkillResource is a file in a skill's manifest, with the SHA-256
	// digest and size of the bytes resources/read returns.
	SkillResource struct {
		URI    string `json:"uri"`
		Digest string `json:"digest"`
		Size   int    `json:"size"`
	}

	// SkillsListParams are skills/list's parameters. Every skill fits on
	// one page, so the cursor is never set by the server.
	SkillsListParams struct {
		mcp.ParamsBase
		Cursor string `json:"cursor,omitempty"`
	}

	// SkillsListResult is skills/list's result.
	SkillsListResult struct {
		mcp.ResultBase
		mcp.Cacheable
		ResultType string       `json:"resultType"`
		Skills     []SkillEntry `json:"skills"`
	}

	// SkillsGetParams are skills/get's parameters.
	SkillsGetParams struct {
		mcp.ParamsBase
		URI string `json:"uri"`
	}

	// SkillsGetResult is skills/get's result.
	SkillsGetResult struct {
		mcp.ResultBase
		mcp.Cacheable
		ResultType string     `json:"resultType"`
		Skill      SkillEntry `json:"skill"`
	}

	// DirectoryReadParams are resources/directory/read's parameters. Every
	// directory fits on one page, so the cursor is never set by the server.
	DirectoryReadParams struct {
		mcp.ParamsBase
		URI    string `json:"uri"`
		Cursor string `json:"cursor,omitempty"`
	}

	// DirectoryReadResult is resources/directory/read's result: the
	// directory's direct children, subdirectories as directory resources.
	DirectoryReadResult struct {
		mcp.ResultBase
		mcp.Cacheable
		ResultType string          `json:"resultType"`
		Resources  []*mcp.Resource `json:"resources"`
	}

	// skillSet is the loaded skills: their entries in name order, every
	// file's bytes by URI, and every directory's children by URI.
	skillSet struct {
		entries []SkillEntry
		files   map[string][]byte
		dirs    map[string][]*mcp.Resource
	}
)

// loadSkills reads the skills under root: each directory is a skill, named
// after it, with a SKILL.md and any supporting files.
func loadSkills(fsys fs.FS, root string) (*skillSet, error) {
	dirs, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}
	set := &skillSet{files: map[string][]byte{}}
	for _, dir := range dirs {
		if !dir.IsDir() {
			return nil, fmt.Errorf("%s: not a skill directory", dir.Name())
		}
		entry, err := set.loadSkill(fsys, root, dir.Name())
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", dir.Name(), err)
		}
		set.entries = append(set.entries, entry)
	}
	set.index()
	return set, nil
}

// index lists every directory of every skill, the skill root included,
// with its direct children in URI order.
func (s *skillSet) index() {
	children := map[string]map[string]*mcp.Resource{}
	for _, entry := range s.entries {
		for _, r := range entry.Resources {
			child := entry.fileResource(r)
			for dir := path.Dir(strings.TrimPrefix(r.URI, _skillScheme)); dir != "."; dir = path.Dir(dir) {
				uri := _skillScheme + dir
				if children[uri] == nil {
					children[uri] = map[string]*mcp.Resource{}
				}
				children[uri][child.URI] = child
				child = &mcp.Resource{URI: uri, Name: dir, Description: entry.describe(uri, "directory"), MIMEType: MIMETypeDirectory}
			}
		}
	}
	s.dirs = map[string][]*mcp.Resource{}
	for uri, byURI := range children {
		list := make([]*mcp.Resource, 0, len(byURI))
		for _, child := range byURI {
			list = append(list, child)
		}
		slices.SortFunc(list, func(a, b *mcp.Resource) int { return strings.Compare(a.URI, b.URI) })
		s.dirs[uri] = list
	}
}

// fileResource is a skill file's resource metadata.
func (e SkillEntry) fileResource(r SkillResource) *mcp.Resource {
	return &mcp.Resource{URI: r.URI, Name: r.URI[len(_skillScheme):], Description: e.describe(r.URI, "file"), MIMEType: skillMIMEType(r.URI), Size: int64(r.Size)}
}

// describe is the description of the skill's resource at uri, a file or a
// directory: the skill's own description for its SKILL.md and its root,
// else what the resource is and which skill it belongs to.
func (e SkillEntry) describe(uri, kind string) string {
	name := e.Frontmatter["name"].(string)
	if uri == e.URI || uri == _skillScheme+name {
		return e.Frontmatter["description"].(string)
	}
	return fmt.Sprintf("The %s %s of the %s skill.", strings.TrimPrefix(uri, _skillScheme+name+"/"), kind, name)
}

// loadSkill reads one skill's files, SKILL.md first, into the set.
func (s *skillSet) loadSkill(fsys fs.FS, root, name string) (SkillEntry, error) {
	entry := SkillEntry{URI: _skillScheme + name + "/" + _skillFile}
	main, err := fs.ReadFile(fsys, path.Join(root, name, _skillFile))
	if err != nil {
		return entry, err
	}
	if entry.Frontmatter, err = frontmatter(main); err != nil {
		return entry, err
	}
	if entry.Frontmatter["name"] != name {
		return entry, fmt.Errorf("frontmatter name must be %q", name)
	}
	if d, _ := entry.Frontmatter["description"].(string); d == "" {
		return entry, errors.New("frontmatter needs a description")
	}
	entry.Resources = append(entry.Resources, s.add(entry.URI, main))
	err = fs.WalkDir(fsys, path.Join(root, name), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == path.Join(root, name, _skillFile) {
			return err
		}
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		entry.Resources = append(entry.Resources, s.add(_skillScheme+p[len(root)+1:], body))
		return nil
	})
	return entry, err
}

// add keeps a file's bytes and returns its manifest entry.
func (s *skillSet) add(uri string, body []byte) SkillResource {
	s.files[uri] = body
	sum := sha256.Sum256(body)
	return SkillResource{URI: uri, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(body)}
}

// frontmatter parses the YAML block a SKILL.md starts with.
func frontmatter(body []byte) (map[string]any, error) {
	rest, ok := bytes.CutPrefix(body, []byte("---\n"))
	if !ok {
		return nil, errors.New("SKILL.md must start with YAML frontmatter")
	}
	block, _, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		return nil, errors.New("SKILL.md frontmatter has no closing ---")
	}
	fields := map[string]any{}
	if err := yaml.Unmarshal(block, &fields); err != nil {
		return nil, fmt.Errorf("SKILL.md frontmatter: %w", err)
	}
	return fields, nil
}

// mustLoadSkills loads the built-in skills; a broken one is a build bug.
func mustLoadSkills(fsys fs.FS) *skillSet {
	set, err := loadSkills(fsys, _skillsRoot)
	if err != nil {
		panic(err)
	}
	return set
}

// register serves the skills on server: their files as resources, and the
// skills/list, skills/get and resources/directory/read methods.
func (s *skillSet) register(server *mcp.Server) {
	for _, entry := range s.entries {
		for _, r := range entry.Resources {
			server.AddResource(entry.fileResource(r), s.read)
		}
	}
	// No name shadows a standard MCP method, so registering can't fail.
	_ = mcp.AddReceivingCustomMethod(server, MethodSkillsList, s.list)
	_ = mcp.AddReceivingCustomMethod(server, MethodSkillsGet, s.get)
	_ = mcp.AddReceivingCustomMethod(server, MethodDirectoryRead, s.readDirectory)
}

// skillMIMEType names a skill file's type from its extension.
func skillMIMEType(uri string) string {
	ext := path.Ext(uri)
	if ext == ".md" {
		return "text/markdown"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// cacheable lets any client cache skills for _skillsTTL.
func cacheable() mcp.Cacheable {
	return mcp.Cacheable{TTLMs: int(_skillsTTL / time.Millisecond), CacheScope: "public"}
}

// read serves a skill file: UTF-8 as text, anything else as a blob. The
// server only routes registered URIs here.
func (s *skillSet) read(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	body := s.files[uri]
	contents := &mcp.ResourceContents{URI: uri, MIMEType: skillMIMEType(uri)}
	if utf8.Valid(body) {
		contents.Text = string(body)
	} else {
		contents.Blob = body
	}
	return &mcp.ReadResourceResult{Cacheable: cacheable(), Contents: []*mcp.ResourceContents{contents}}, nil
}

func (s *skillSet) list(context.Context, *mcp.ServerSession, *SkillsListParams) (*SkillsListResult, error) {
	return &SkillsListResult{Cacheable: cacheable(), ResultType: _resultComplete, Skills: s.entries}, nil
}

func (s *skillSet) get(_ context.Context, _ *mcp.ServerSession, params *SkillsGetParams) (*SkillsGetResult, error) {
	for _, entry := range s.entries {
		if entry.URI == params.URI {
			return &SkillsGetResult{Cacheable: cacheable(), ResultType: _resultComplete, Skill: entry}, nil
		}
	}
	return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unknown skill %q", params.URI)}
}

// readDirectory lists a skill directory's direct children. A URI that is
// not a directory, a file's included, is invalid params, as SEP-2640 says.
func (s *skillSet) readDirectory(_ context.Context, _ *mcp.ServerSession, params *DirectoryReadParams) (*DirectoryReadResult, error) {
	children, ok := s.dirs[params.URI]
	if !ok {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("not a directory %q", params.URI)}
	}
	return &DirectoryReadResult{Cacheable: cacheable(), ResultType: _resultComplete, Resources: children}, nil
}
