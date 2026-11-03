// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package mcp is the stateless MCP (streamable HTTP) transport. It offers
// one tool per operation (uploadFile, downloadFile, listFiles, getUrl,
// listFolders), mirroring the `permahost file` CLI commands; each takes only
// the arguments its operation needs.
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

	authctrl "github.com/tmphost/permahost/backend/internal/controller/auth"
	"github.com/tmphost/permahost/backend/internal/controller/credit"
	folderctrl "github.com/tmphost/permahost/backend/internal/controller/folder"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/service/idgen"
)

// The tools' names.
const (
	ToolUploadFile   = "uploadFile"
	ToolDownloadFile = "downloadFile"
	ToolListFiles    = "listFiles"
	ToolGetURL       = "getUrl"
	ToolListFolders  = "listFolders"
)

// The content formats: which field carries a file's content.
const (
	FormatBase64 = "base64Content"
	FormatText   = "textContent"
)

// _credits spells out what folders and storage cost, so agents reuse
// folders instead of paying for new ones.
var _credits = fmt.Sprintf(`A new folder costs %[1]d credits a month; only uploadFile without folderId creates one. Uploading with folderId into an existing folder never costs folder credits again, however many files it adds or replaces. Storage costs %[2]d credits a month per started %[3]d MB the whole account stores; an upload pays only when it pushes the total into a block not paid for this month, so most uploads cost 0, and replacing a file counts only the size difference.`,
	credit.FolderCredits, credit.StorageCredits, credit.StorageBlockBytes>>20)

var (
	_uploadFileDescription   = `Store a file in a permanent folder and get its public URL, served for as long as the account has credits. Without folderId a new folder is created; with folderId the file goes into that existing folder, and a file already at that path is replaced in place (its url stays the same). Put the content in base64Content or, for UTF-8 text with format textContent, in textContent. Returns folderId, the file's url and creditsSpent. ` + _credits + ` So upload the first file of a site without folderId, keep the folderId it returns and pass it for every other file; to add to or update something published earlier, find its folder with listFolders.`
	_downloadFileDescription = `Read back the content of a file in one of your folders, such as to check what was uploaded or to edit a page and upload it again with uploadFile (which replaces it at the same url). It costs no credits and changes nothing. To share a file, give its url instead (from uploadFile, listFiles or getUrl). Name the file by fileName and path as listFiles returns them. Returns the file's folderId, fileName, path and size in bytes, with its content in base64Content (any file) or, with format textContent, in textContent as UTF-8 text; textContent fails for a file that is not UTF-8, such as an image, so use base64Content for those.`
	_listFilesDescription    = `List the files in one of your folders. Free. Returns the folder's url and files: one entry per file, sorted by full path, with its fileName, the directory path that holds it, its size in bytes and its public url. Pass a file's fileName and path to downloadFile or getUrl.`
	_getURLDescription       = `Build the public URL of one of your folders, a directory in it (path) or a file in it (fileName, optional path) without uploading anything; it does not check that the directory or file exists (use listFiles for that). Free. Returns the url and the folderId, fileName and path it was built for.`
	_listFoldersDescription  = `List your folders, newest first, each with its folderId, title, url, sizeBytes and createdAt. Free. Use it to find the folderId of an existing folder before adding files to it with uploadFile, which costs no folder credits.`
)

// _instructions tell an agent when and how to use the server.
var _instructions = `permahost.io hosts files permanently: use it to publish a file, a report or a whole static site at a public URL that stays up for as long as the account has credits.
- Call uploadFile without folderId for the first file to create a folder, then pass the returned folderId to add more files; keep paths relative so pages link to each other. A folder serves index.html at its root.
- To add to or update something published earlier, find its folder with listFolders and upload with its folderId. Never create a new folder just to add files.
- Prefer format textContent for UTF-8 text (HTML, CSS, JS, JSON, Markdown, CSV) and base64Content for anything else.
- Uploading to a path that already holds a file replaces it in place, and its URL stays the same.
- ` + _credits + `
- Anything uploaded is public to whoever has the URL: never upload secrets or private data unless the user asks to.
- Give the user the url from the result; use listFiles to list a folder, downloadFile to read a file back, and getUrl to build links without uploading.`

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
		// EndpointURL is the endpoint's public URL, which the setup page
		// shows people who open it in a browser.
		EndpointURL string
	}

	// UploadFileInput is uploadFile's arguments.
	UploadFileInput struct {
		FolderID string `json:"folderId,omitempty" jsonschema:"folder to add the file to or replace it in, at no folder cost; leave out to create (and pay for) a new folder"`
		FileName string `json:"fileName" jsonschema:"file name, without directories, such as index.html"`
		Path     string `json:"path,omitempty" jsonschema:"directory inside the folder, such as site/css, at most 5 levels; empty for the folder root"`
		Format   string `json:"format,omitempty" jsonschema:"which field carries the content: base64Content (default, any file) or textContent (UTF-8 text)"`
		// Base64Content and TextContent are pointers so an empty file can
		// be told apart from a missing field.
		Base64Content *string `json:"base64Content,omitempty" jsonschema:"base64 file content, with format base64Content"`
		TextContent   *string `json:"textContent,omitempty" jsonschema:"UTF-8 text file content, with format textContent"`
	}

	// UploadFileOutput is uploadFile's result.
	UploadFileOutput struct {
		FolderID     string `json:"folderId" jsonschema:"id of the folder that holds the file, newly created or as passed in; pass it as folderId to add more files or to the other tools"`
		URL          string `json:"url" jsonschema:"the file's public URL; give it to the user"`
		FileName     string `json:"fileName" jsonschema:"the file's name, as passed in"`
		Path         string `json:"path" jsonschema:"the directory inside the folder that holds the file, without leading or trailing slashes; empty for the folder root"`
		Size         int64  `json:"size" jsonschema:"the stored file's size in bytes"`
		CreditsSpent int    `json:"creditsSpent" jsonschema:"credits this call took from the user's balance, for a new folder and for storage; 0 for most uploads into an existing folder"`
	}

	// DownloadFileInput is downloadFile's arguments.
	DownloadFileInput struct {
		FolderID string `json:"folderId" jsonschema:"id of a folder you own, as uploadFile or listFolders returned it in folderId, such as 1a2b3c4d"`
		FileName string `json:"fileName" jsonschema:"file name, without directories, such as index.html"`
		Path     string `json:"path,omitempty" jsonschema:"directory inside the folder that holds the file, as listFiles returns it in path, such as site/css; leave out for a file at the folder root"`
		Format   string `json:"format,omitempty" jsonschema:"which field carries the content: base64Content (default, any file) or textContent (UTF-8 text)"`
	}

	// DownloadFileOutput is downloadFile's result.
	DownloadFileOutput struct {
		FolderID string `json:"folderId" jsonschema:"the folder's id, as passed in"`
		FileName string `json:"fileName" jsonschema:"the file's name, as passed in; pass it with path to uploadFile to replace the file"`
		Path     string `json:"path" jsonschema:"the directory inside the folder that holds the file, without leading or trailing slashes; empty for the folder root"`
		Size     int64  `json:"size" jsonschema:"the file's size in bytes, the length of its content once decoded"`
		// Base64Content and TextContent are pointers so an empty file
		// still carries its (empty) content field.
		Base64Content *string `json:"base64Content,omitempty" jsonschema:"the file's bytes, base64-encoded: decode it to get the file; present when format is base64Content (the default), an empty string for an empty file"`
		TextContent   *string `json:"textContent,omitempty" jsonschema:"the file's content as UTF-8 text, ready to read or edit; present when format is textContent, an empty string for an empty file"`
	}

	// FolderInput is the arguments of the tools that take only a folder.
	FolderInput struct {
		FolderID string `json:"folderId" jsonschema:"id of a folder you own, as uploadFile or listFolders returned it in folderId, such as 1a2b3c4d"`
	}

	// ListFilesOutput is listFiles' result.
	ListFilesOutput struct {
		FolderID string     `json:"folderId" jsonschema:"the folder's id, as passed in"`
		URL      string     `json:"url" jsonschema:"the folder's public URL, ending with a slash; it serves index.html when the folder has one at its root. Give it to the user to open the folder"`
		Files    []FileInfo `json:"files" jsonschema:"the folder's files, one entry per file sorted by full path; an empty array, never null, when the folder has none"`
	}

	// FileInfo is a file in a listFiles listing.
	FileInfo struct {
		FileName string `json:"fileName" jsonschema:"the file's name, without directories"`
		Path     string `json:"path" jsonschema:"the directory inside the folder that holds the file, such as site/css; empty for the folder root"`
		Size     int64  `json:"size" jsonschema:"the file's size in bytes"`
		URL      string `json:"url" jsonschema:"the file's public URL; give it to the user to open or share the file"`
	}

	// GetURLInput is getUrl's arguments.
	GetURLInput struct {
		FolderID string `json:"folderId" jsonschema:"id of a folder you own, as uploadFile or listFolders returned it in folderId, such as 1a2b3c4d"`
		Path     string `json:"path,omitempty" jsonschema:"directory inside the folder to link to, or that holds fileName, as listFiles returns it in path, such as site/css; leave out for the folder root"`
		FileName string `json:"fileName,omitempty" jsonschema:"name of the file to link to, as listFiles returns it in fileName, such as index.html; leave out for the URL of the folder or of path"`
	}

	// GetURLOutput is getUrl's result.
	GetURLOutput struct {
		FolderID string `json:"folderId" jsonschema:"the folder's id, as passed in"`
		URL      string `json:"url" jsonschema:"the public URL of the folder, directory or file; a folder or directory URL ends with a slash"`
		FileName string `json:"fileName,omitempty" jsonschema:"the file name the url points to, as passed in; absent for a folder or directory URL"`
		Path     string `json:"path,omitempty" jsonschema:"the directory the url points into, without leading or trailing slashes; absent for the folder root"`
	}

	// ListFoldersInput is listFolders' arguments: none.
	ListFoldersInput struct{}

	// ListFoldersOutput is listFolders' result.
	ListFoldersOutput struct {
		Folders []FolderInfo `json:"folders" jsonschema:"your folders, newest first; an empty array, never null, when you have none"`
	}

	// FolderInfo is a folder in a listFolders listing.
	FolderInfo struct {
		FolderID string `json:"folderId" jsonschema:"the folder's id; pass it as folderId to uploadFile to add files at no folder cost, or to the other tools"`
		// Title is the owner's title, else the name of the folder's only
		// file, else empty.
		Title     string `json:"title,omitempty" jsonschema:"the folder's title, else the name of its only file; absent when it has neither"`
		URL       string `json:"url" jsonschema:"the folder's public URL, ending with a slash"`
		SizeBytes int64  `json:"sizeBytes" jsonschema:"the total size of the folder's files in bytes"`
		CreatedAt string `json:"createdAt" jsonschema:"when the folder was created, as an RFC 3339 UTC time"`
	}

	tools struct {
		folders folderctrl.Controller
	}
)

// New returns the MCP endpoint. Every request needs an OAuth access token
// as a Bearer token; without one, the 401 points to the resource metadata.
func New(p Params) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "permahost", Title: "permahost.io"}, &mcp.ServerOptions{Instructions: _instructions})
	t := &tools{folders: p.Folders}
	mcp.AddTool(server, &mcp.Tool{Name: ToolUploadFile, Title: "Upload a file", Description: _uploadFileDescription, InputSchema: schemaFor[UploadFileInput](), OutputSchema: schemaFor[UploadFileOutput](), Annotations: writeAnnotations("Upload a file")}, handle(t.uploadFile))
	mcp.AddTool(server, &mcp.Tool{Name: ToolDownloadFile, Title: "Download a file", Description: _downloadFileDescription, InputSchema: schemaFor[DownloadFileInput](), OutputSchema: schemaFor[DownloadFileOutput](), Annotations: readAnnotations("Download a file")}, handle(t.downloadFile))
	mcp.AddTool(server, &mcp.Tool{Name: ToolListFiles, Title: "List a folder's files", Description: _listFilesDescription, InputSchema: schemaFor[FolderInput](), OutputSchema: listFilesSchema(), Annotations: readAnnotations("List a folder's files")}, handle(t.listFiles))
	mcp.AddTool(server, &mcp.Tool{Name: ToolGetURL, Title: "Get a public URL", Description: _getURLDescription, InputSchema: schemaFor[GetURLInput](), OutputSchema: schemaFor[GetURLOutput](), Annotations: readAnnotations("Get a public URL")}, handle(t.getURL))
	mcp.AddTool(server, &mcp.Tool{Name: ToolListFolders, Title: "List your folders", Description: _listFoldersDescription, InputSchema: schemaFor[ListFoldersInput](), OutputSchema: listFoldersSchema(), Annotations: readAnnotations("List your folders")}, handle(t.listFolders))

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: p.MaxBodyBytes,
	})
	return setupPage(renderSetupPage(p.EndpointURL), auth.RequireBearerToken(verifier(p.Auth), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL:    p.ResourceMetadataURL,
		AllowMissingExpiration: true,
	})(handler))
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

// readAnnotations describe a tool that only reads the caller's folders.
// destructiveHint is set to false rather than left out: platform checks want
// every hint declared as a boolean.
func readAnnotations(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false)}
}

// writeAnnotations describe uploadFile: it can replace a file, publishes to
// the open web and is not safe to repeat, as each new folder is paid for.
func writeAnnotations(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: new(true), OpenWorldHint: new(true)}
}

// listFilesSchema is listFiles' output schema, with each files entry
// described too.
func listFilesSchema() *jsonschema.Schema {
	s := schemaFor[ListFilesOutput]()
	// singleTypes moved the array into the second anyOf branch.
	s.Properties["files"].AnyOf[1].Items.Description = "one file in the folder: pass its fileName and path to downloadFile or getUrl, or give its url to the user"
	return s
}

// listFoldersSchema is listFolders' output schema, with each folders entry
// described too.
func listFoldersSchema() *jsonschema.Schema {
	s := schemaFor[ListFoldersOutput]()
	s.Properties["folders"].AnyOf[1].Items.Description = "one of your folders: pass its folderId to uploadFile, listFiles, downloadFile or getUrl"
	return s
}

// schemaFor infers a tool's input or output schema, limiting format to the
// known content formats and spelling nullable fields portably.
func schemaFor[T any]() *jsonschema.Schema {
	// The inputs and outputs are plain structs, so inference cannot fail.
	s, _ := jsonschema.For[T](nil)
	if f := s.Properties["format"]; f != nil {
		f.Enum = []any{FormatBase64, FormatText}
	}
	singleTypes(s)
	return s
}

// singleTypes rewrites every `type` array the inference makes for pointers
// and slices, such as ["null","string"], into anyOf branches of one type
// each. Clients that map tool schemas onto a single-type dialect (like
// Gemini's OpenAPI subset) refuse or mishandle type arrays.
func singleTypes(s *jsonschema.Schema) {
	for _, p := range s.Properties {
		singleTypes(p)
	}
	if s.Items != nil {
		singleTypes(s.Items)
	}
	if len(s.Types) < 2 {
		return
	}
	for _, typ := range s.Types {
		branch := &jsonschema.Schema{Type: typ}
		if typ == "array" {
			branch.Items = s.Items
		}
		s.AnyOf = append(s.AnyOf, branch)
	}
	s.Types, s.Items = nil, nil
}

// handle adapts a tool method to the SDK, passing it the caller's user ID
// from the token.
func handle[In, Out any](fn func(context.Context, int64, In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := fn(ctx, monoflake.IDFromBase62(req.Extra.TokenInfo.UserID).Int64(), in)
		return nil, out, err
	}
}

func (t *tools) uploadFile(ctx context.Context, userID int64, in UploadFileInput) (UploadFileOutput, error) {
	p, err := filePath(in.Path, in.FileName)
	if err != nil {
		return UploadFileOutput{}, err
	}
	data, err := uploadContent(in)
	if err != nil {
		return UploadFileOutput{}, err
	}
	folderID, created, err := t.uploadFolder(ctx, userID, in)
	if err != nil {
		return UploadFileOutput{}, err
	}
	rs, err := t.folders.PutFiles(ctx, &entity.PutFilesRequest{UserID: userID, FolderID: folderID, Files: []entity.FileUpload{{Path: p, Data: data}}})
	if err != nil {
		if created != nil {
			// The new folder is paid for, so it is kept for a retry.
			err = fmt.Errorf("%w (folder %s was created for %d credits: pass it as folderId to retry)", err, idgen.FormatFolderID(folderID), created.CreditsSpent)
		}
		return UploadFileOutput{}, err
	}
	out := UploadFileOutput{
		FolderID:     idgen.FormatFolderID(folderID),
		URL:          rs.Files[0].URL,
		FileName:     in.FileName,
		Path:         dirOf(p),
		Size:         rs.Files[0].Size,
		CreditsSpent: rs.CreditsSpent,
	}
	if created != nil {
		out.CreditsSpent += created.CreditsSpent
	}
	return out, nil
}

// uploadContent decodes the content field that format names. The other
// field must be empty, so content is never silently ignored.
func uploadContent(in UploadFileInput) ([]byte, error) {
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
// folderId is given. created is the new folder, if any.
func (t *tools) uploadFolder(ctx context.Context, userID int64, in UploadFileInput) (int64, *entity.CreateFolderResponse, error) {
	if in.FolderID != "" {
		id, err := folderID(in.FolderID)
		return id, nil, err
	}
	rs, err := t.folders.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: userID})
	if err != nil {
		return 0, nil, err
	}
	return rs.Folder.ID, rs, nil
}

func (t *tools) downloadFile(ctx context.Context, userID int64, in DownloadFileInput) (DownloadFileOutput, error) {
	format, err := contentFormat(in.Format)
	if err != nil {
		return DownloadFileOutput{}, err
	}
	id, err := folderID(in.FolderID)
	if err != nil {
		return DownloadFileOutput{}, err
	}
	p, err := filePath(in.Path, in.FileName)
	if err != nil {
		return DownloadFileOutput{}, err
	}
	rs, err := t.folders.OpenFile(ctx, &entity.OpenFileRequest{UserID: userID, FolderID: id, Path: p})
	if err != nil {
		return DownloadFileOutput{}, err
	}
	defer func() { _ = rs.Body.Close() }()
	data, err := io.ReadAll(rs.Body)
	if err != nil {
		return DownloadFileOutput{}, fmt.Errorf("read file: %w", err)
	}
	out := DownloadFileOutput{FolderID: in.FolderID, FileName: in.FileName, Path: dirOf(p), Size: int64(len(data))}
	if format == FormatText {
		if !utf8.Valid(data) {
			return DownloadFileOutput{}, errors.New("the file is not UTF-8 text: download it with format base64Content")
		}
		text := string(data)
		out.TextContent = &text
	} else {
		encoded := base64.StdEncoding.EncodeToString(data)
		out.Base64Content = &encoded
	}
	return out, nil
}

func (t *tools) listFiles(ctx context.Context, userID int64, in FolderInput) (ListFilesOutput, error) {
	rs, err := t.folder(ctx, userID, in.FolderID)
	if err != nil {
		return ListFilesOutput{}, err
	}
	files := make([]FileInfo, len(rs.Files))
	for i, f := range rs.Files {
		files[i] = FileInfo{FileName: f.Path[strings.LastIndex(f.Path, "/")+1:], Path: dirOf(f.Path), Size: f.Size, URL: f.URL}
	}
	return ListFilesOutput{FolderID: in.FolderID, URL: rs.Folder.URL, Files: files}, nil
}

func (t *tools) listFolders(ctx context.Context, userID int64, _ ListFoldersInput) (ListFoldersOutput, error) {
	rs, err := t.folders.ListFolders(ctx, &entity.ListFoldersRequest{UserID: userID})
	if err != nil {
		return ListFoldersOutput{}, err
	}
	folders := make([]FolderInfo, len(rs.Folders))
	for i, f := range rs.Folders {
		id := idgen.FormatFolderID(f.ID)
		title := f.Title
		if title == "" && f.DefaultTitle != id {
			title = f.DefaultTitle
		}
		folders[i] = FolderInfo{FolderID: id, Title: title, URL: f.URL, SizeBytes: f.SizeBytes, CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339)}
	}
	return ListFoldersOutput{Folders: folders}, nil
}

func (t *tools) getURL(ctx context.Context, userID int64, in GetURLInput) (GetURLOutput, error) {
	dir := strings.Trim(in.Path, "/")
	full := strings.Trim(dir+"/"+in.FileName, "/")
	if strings.Contains(in.FileName, "/") || (full != "" && !folderctrl.ValidPath(full)) {
		return GetURLOutput{}, folderctrl.ErrInvalidPath
	}
	rs, err := t.folder(ctx, userID, in.FolderID)
	if err != nil {
		return GetURLOutput{}, err
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
	return GetURLOutput{FolderID: in.FolderID, URL: rs.Folder.URL + strings.Join(segs, "/"), FileName: in.FileName, Path: dir}, nil
}

func (t *tools) folder(ctx context.Context, userID int64, rawID string) (*entity.GetFolderResponse, error) {
	id, err := folderID(rawID)
	if err != nil {
		return nil, err
	}
	return t.folders.GetFolder(ctx, &entity.GetFolderRequest{UserID: userID, FolderID: id})
}

func folderID(raw string) (int64, error) {
	id := idgen.ParseFolderID(raw)
	if id == 0 {
		return 0, errors.New("folderId must be a folder id")
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
