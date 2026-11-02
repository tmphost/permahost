// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package mcp is the stateless MCP (streamable HTTP) transport. It offers
// one tool, "file", whose actions mirror the `tmphost file` CLI commands.
package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	folderctrl "github.com/tmphost/tmphost/backend/internal/controller/folder"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
)

// The file tool's actions.
const (
	ActionUpload   = "upload"
	ActionDownload = "download"
	ActionGet      = "get"
	ActionURL      = "url"
)

// The content formats: which field carries a file's content.
const (
	FormatBase64 = "base64Content"
	FormatText   = "textContent"
)

const _toolDescription = `Temporary file hosting on tmphost.io. Files live in folders that expire after a TTL; each file is served at a public URL until then.
Actions:
- upload: store a file (fileName, its content in base64Content or textContent as format says, optional path inside the folder). Without id a new folder is created with ttl (default 30m); with id the file goes into that folder, and a file already at that path is replaced in place (an update; its url stays the same). Returns id and url. Costs 1 credit per started MB of the file per started 30 minutes of the folder's remaining lifetime.
- download: read a file (id, fileName, optional path). Returns its content in base64Content or, for UTF-8 text, in textContent as format says.
- get: list a folder's files (id).
- url: build the public URL of a folder (id), a directory (id, path) or a file (id, fileName, optional path).
Paths are slash-separated directories inside the folder, at most 5 levels deep.
format is base64Content (the default, for any file) or textContent (UTF-8 text such as HTML, CSS or JSON, sent as is).`

type (
	// Params are the transport's dependencies.
	Params struct {
		Folders folderctrl.Controller
		Auth    authctrl.Controller
		// MaxBodyBytes caps a request; uploads arrive base64-encoded inside
		// it.
		MaxBodyBytes int64
		// ResourceMetadataURL is where clients without a token learn how
		// to get one (RFC 9728).
		ResourceMetadataURL string
	}

	// FileInput is the file tool's arguments.
	FileInput struct {
		Action   string `json:"action" jsonschema:"what to do: upload, download, get or url"`
		ID       string `json:"id,omitempty" jsonschema:"folder id; upload without it creates a new folder, with it adds or replaces files in that folder"`
		TTL      string `json:"ttl,omitempty" jsonschema:"lifetime of a new folder, such as 30m or 2h (default 30m); upload only"`
		FileName string `json:"fileName,omitempty" jsonschema:"file name, without directories"`
		Path     string `json:"path,omitempty" jsonschema:"directory inside the folder, such as site/css, at most 5 levels; empty for the folder root"`
		Format   string `json:"format,omitempty" jsonschema:"which field carries the content: base64Content (default) or textContent; upload and download only"`
		// Base64Content and TextContent are pointers so an empty file can
		// be told apart from a missing field.
		Base64Content *string `json:"base64Content,omitempty" jsonschema:"base64 file content; upload with format base64Content"`
		TextContent   *string `json:"textContent,omitempty" jsonschema:"UTF-8 text file content; upload with format textContent"`
	}

	// FileOutput is the file tool's result; each action fills its part.
	FileOutput struct {
		ID           string `json:"id,omitempty"`
		URL          string `json:"url,omitempty"`
		ExpiresAt    string `json:"expiresAt,omitempty"`
		FileName     string `json:"fileName,omitempty"`
		Path         string `json:"path,omitempty"`
		Size         int64  `json:"size,omitempty"`
		CreditsSpent int    `json:"creditsSpent,omitempty"`
		// Base64Content and TextContent are pointers so an empty file
		// still carries its (empty) content field.
		Base64Content *string    `json:"base64Content,omitempty"`
		TextContent   *string    `json:"textContent,omitempty"`
		Files         []FileInfo `json:"files,omitempty"`
	}

	// FileInfo is a file in a get listing.
	FileInfo struct {
		FileName string `json:"fileName"`
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		URL      string `json:"url"`
	}

	tool struct {
		folders folderctrl.Controller
	}
)

// New returns the MCP endpoint. Every request needs an OAuth access token
// as a Bearer token; without one, the 401 points to the resource metadata.
func New(p Params) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "tmphost", Title: "tmphost.io"}, nil)
	t := &tool{folders: p.Folders}
	mcp.AddTool(server, &mcp.Tool{Name: "file", Description: _toolDescription, InputSchema: inputSchema()}, t.handle)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: p.MaxBodyBytes,
	})
	return auth.RequireBearerToken(verifier(p.Auth), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL:    p.ResourceMetadataURL,
		AllowMissingExpiration: true,
	})(handler)
}

// verifier accepts access tokens; the user ID travels in the token info.
func verifier(controller authctrl.Controller) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		rs, err := controller.Authenticate(ctx, &entity.AuthenticateRequest{AccessToken: token})
		if err != nil {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: monoflake.ID(rs.UserID).String()}, nil
	}
}

func inputSchema() *jsonschema.Schema {
	// FileInput is a plain struct, so inference cannot fail.
	s, _ := jsonschema.For[FileInput](nil)
	s.Properties["action"].Enum = []any{ActionUpload, ActionDownload, ActionGet, ActionURL}
	s.Properties["format"].Enum = []any{FormatBase64, FormatText}
	return s
}

func (t *tool) handle(ctx context.Context, req *mcp.CallToolRequest, in FileInput) (*mcp.CallToolResult, FileOutput, error) {
	userID := monoflake.IDFromBase62(req.Extra.TokenInfo.UserID).Int64()
	var (
		out FileOutput
		err error
	)
	switch in.Action {
	case ActionUpload:
		out, err = t.upload(ctx, userID, in)
	case ActionDownload:
		out, err = t.download(ctx, userID, in)
	case ActionGet:
		out, err = t.get(ctx, userID, in)
	case ActionURL:
		out, err = t.url(ctx, userID, in)
	default:
		err = fmt.Errorf("unknown action %q: use upload, download, get or url", in.Action)
	}
	return nil, out, err
}

func (t *tool) upload(ctx context.Context, userID int64, in FileInput) (FileOutput, error) {
	p, err := filePath(in.Path, in.FileName)
	if err != nil {
		return FileOutput{}, err
	}
	data, err := uploadContent(in)
	if err != nil {
		return FileOutput{}, err
	}
	folderID, created, err := t.uploadFolder(ctx, userID, in)
	if err != nil {
		return FileOutput{}, err
	}
	rs, err := t.folders.PutFiles(ctx, &entity.PutFilesRequest{UserID: userID, FolderID: folderID, Files: []entity.FileUpload{{Path: p, Data: data}}})
	if err != nil {
		if created != nil {
			// Don't leave an empty folder behind for a failed first upload.
			_ = t.folders.DeleteFolder(ctx, &entity.DeleteFolderRequest{UserID: userID, FolderID: folderID})
		}
		return FileOutput{}, err
	}
	out := FileOutput{
		ID:           idgen.FormatFolderID(folderID),
		URL:          rs.Files[0].URL,
		FileName:     in.FileName,
		Path:         dirOf(p),
		Size:         rs.Files[0].Size,
		CreditsSpent: rs.CreditsSpent,
	}
	if created != nil {
		out.ExpiresAt = formatTime(created.ExpiresAt)
	}
	return out, nil
}

// uploadContent decodes the content field that format names. The other
// field must be empty, so content is never silently ignored.
func uploadContent(in FileInput) ([]byte, error) {
	format, err := contentFormat(in.Format)
	if err != nil {
		return nil, err
	}
	if format == FormatText {
		if in.Base64Content != nil {
			return nil, errors.New("base64Content needs format base64Content")
		}
		if in.TextContent == nil {
			return nil, errors.New("textContent is required with format textContent")
		}
		return []byte(*in.TextContent), nil
	}
	if in.TextContent != nil {
		return nil, errors.New("textContent needs format textContent")
	}
	if in.Base64Content == nil {
		return nil, errors.New("base64Content is required with format base64Content")
	}
	data, err := base64.StdEncoding.DecodeString(*in.Base64Content)
	if err != nil {
		return nil, errors.New("base64Content must be base64")
	}
	return data, nil
}

// contentFormat checks a format, defaulting to base64Content.
func contentFormat(format string) (string, error) {
	switch format {
	case "", FormatBase64:
		return FormatBase64, nil
	case FormatText:
		return FormatText, nil
	default:
		return "", fmt.Errorf("unknown format %q: use base64Content or textContent", format)
	}
}

// uploadFolder resolves the folder an upload goes to, creating it when no
// id is given. created is the new folder, if any.
func (t *tool) uploadFolder(ctx context.Context, userID int64, in FileInput) (int64, *entity.Folder, error) {
	if in.ID != "" {
		if in.TTL != "" {
			return 0, nil, errors.New("ttl only applies to a new folder: leave out id or ttl")
		}
		id, err := folderID(in.ID)
		return id, nil, err
	}
	minutes := 0
	if in.TTL != "" {
		ttl, err := time.ParseDuration(in.TTL)
		if err != nil || ttl%time.Minute != 0 {
			return 0, nil, errors.New("ttl must be whole minutes, such as 30m or 2h")
		}
		minutes = int(ttl / time.Minute)
	}
	rs, err := t.folders.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: userID, TTLMinutes: minutes})
	if err != nil {
		return 0, nil, err
	}
	return rs.Folder.ID, &rs.Folder, nil
}

func (t *tool) download(ctx context.Context, userID int64, in FileInput) (FileOutput, error) {
	format, err := contentFormat(in.Format)
	if err != nil {
		return FileOutput{}, err
	}
	id, err := folderID(in.ID)
	if err != nil {
		return FileOutput{}, err
	}
	p, err := filePath(in.Path, in.FileName)
	if err != nil {
		return FileOutput{}, err
	}
	rs, err := t.folders.OpenFile(ctx, &entity.OpenFileRequest{UserID: userID, FolderID: id, Path: p})
	if err != nil {
		return FileOutput{}, err
	}
	defer func() { _ = rs.Body.Close() }()
	data, err := io.ReadAll(rs.Body)
	if err != nil {
		return FileOutput{}, fmt.Errorf("read file: %w", err)
	}
	out := FileOutput{ID: in.ID, FileName: in.FileName, Path: dirOf(p), Size: int64(len(data))}
	if format == FormatText {
		if !utf8.Valid(data) {
			return FileOutput{}, errors.New("the file is not UTF-8 text: download it with format base64Content")
		}
		text := string(data)
		out.TextContent = &text
	} else {
		encoded := base64.StdEncoding.EncodeToString(data)
		out.Base64Content = &encoded
	}
	return out, nil
}

func (t *tool) get(ctx context.Context, userID int64, in FileInput) (FileOutput, error) {
	rs, err := t.folder(ctx, userID, in.ID)
	if err != nil {
		return FileOutput{}, err
	}
	files := make([]FileInfo, len(rs.Files))
	for i, f := range rs.Files {
		files[i] = FileInfo{FileName: f.Path[strings.LastIndex(f.Path, "/")+1:], Path: dirOf(f.Path), Size: f.Size, URL: f.URL}
	}
	return FileOutput{ID: in.ID, URL: rs.Folder.URL, ExpiresAt: formatTime(rs.Folder.ExpiresAt), Files: files}, nil
}

func (t *tool) url(ctx context.Context, userID int64, in FileInput) (FileOutput, error) {
	dir := strings.Trim(in.Path, "/")
	full := strings.Trim(dir+"/"+in.FileName, "/")
	if strings.Contains(in.FileName, "/") || (full != "" && !folderctrl.ValidPath(full)) {
		return FileOutput{}, folderctrl.ErrInvalidPath
	}
	rs, err := t.folder(ctx, userID, in.ID)
	if err != nil {
		return FileOutput{}, err
	}
	var segs []string
	if dir != "" {
		segs = strings.Split(dir, "/")
	}
	segs = append(segs, in.FileName)
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	// The folder URL ends with a slash; an empty fileName keeps one at the
	// end of a directory URL too.
	return FileOutput{ID: in.ID, URL: rs.Folder.URL + strings.Join(segs, "/"), ExpiresAt: formatTime(rs.Folder.ExpiresAt), FileName: in.FileName, Path: dir}, nil
}

func (t *tool) folder(ctx context.Context, userID int64, rawID string) (*entity.GetFolderResponse, error) {
	id, err := folderID(rawID)
	if err != nil {
		return nil, err
	}
	return t.folders.GetFolder(ctx, &entity.GetFolderRequest{UserID: userID, FolderID: id})
}

func folderID(raw string) (int64, error) {
	id := idgen.ParseFolderID(raw)
	if id == 0 {
		return 0, errors.New("id must be a folder id")
	}
	return id, nil
}

// filePath joins a directory and a file name into a folder-relative path.
func filePath(dir, fileName string) (string, error) {
	if fileName == "" || strings.Contains(fileName, "/") {
		return "", errors.New("fileName is required and must not contain /")
	}
	if dir = strings.Trim(dir, "/"); dir != "" {
		fileName = dir + "/" + fileName
	}
	if !folderctrl.ValidPath(fileName) {
		return "", folderctrl.ErrInvalidPath
	}
	return fileName, nil
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
