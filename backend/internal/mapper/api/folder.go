// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/view"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
)

// Folder IDs are 11 base36 characters, not base62 like other IDs: they
// are part of each folder's hostname, and hostnames are case-insensitive.

// FromHTTPRequestToGetLimitsRequestEntity reads GET /limits.
func FromHTTPRequestToGetLimitsRequestEntity(c fiber.Ctx) *entity.GetLimitsRequest {
	return &entity.GetLimitsRequest{Anonymous: localAnonymous(c)}
}

// FromHTTPRequestToCreateFolderRequestEntity reads POST /folders. The body
// is optional.
func FromHTTPRequestToCreateFolderRequestEntity(c fiber.Ctx) *entity.CreateFolderRequest {
	userID := localUserID(c)
	if userID == 0 {
		return nil
	}
	var rq view.CreateFolderRequest
	if body := c.Body(); len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &rq); err != nil {
			return nil
		}
	}
	return &entity.CreateFolderRequest{UserID: userID, Anonymous: localAnonymous(c), TTLMinutes: rq.TTLMinutes}
}

// FromHTTPRequestToListFoldersRequestEntity reads GET /folders.
func FromHTTPRequestToListFoldersRequestEntity(c fiber.Ctx) *entity.ListFoldersRequest {
	userID := localUserID(c)
	if userID == 0 {
		return nil
	}
	return &entity.ListFoldersRequest{UserID: userID}
}

// FromHTTPRequestToGetFolderRequestEntity reads GET /folders/:id.
func FromHTTPRequestToGetFolderRequestEntity(c fiber.Ctx) *entity.GetFolderRequest {
	userID, folderID := localUserID(c), idgen.ParseFolderID(c.Params("id"))
	if userID == 0 || folderID == 0 {
		return nil
	}
	return &entity.GetFolderRequest{UserID: userID, FolderID: folderID}
}

// FromHTTPRequestToUpdateFolderRequestEntity reads PATCH /folders/:id,
// whose body must set the title.
func FromHTTPRequestToUpdateFolderRequestEntity(c fiber.Ctx) *entity.UpdateFolderRequest {
	rq := FromHTTPRequestToGetFolderRequestEntity(c)
	var body view.UpdateFolderRequest
	if rq == nil || json.Unmarshal(c.Body(), &body) != nil || body.Title == nil {
		return nil
	}
	return &entity.UpdateFolderRequest{UserID: rq.UserID, FolderID: rq.FolderID, Title: *body.Title}
}

// FromHTTPRequestToDeleteFolderRequestEntity reads DELETE /folders/:id.
func FromHTTPRequestToDeleteFolderRequestEntity(c fiber.Ctx) *entity.DeleteFolderRequest {
	rq := FromHTTPRequestToGetFolderRequestEntity(c)
	if rq == nil {
		return nil
	}
	return &entity.DeleteFolderRequest{UserID: rq.UserID, FolderID: rq.FolderID}
}

// FromHTTPRequestToPutFilesRequestEntity reads PUT /folders/:id/files/*,
// whose raw body is the file.
func FromHTTPRequestToPutFilesRequestEntity(c fiber.Ctx) *entity.PutFilesRequest {
	rq := fileRequest(c)
	if rq == nil {
		return nil
	}
	// fasthttp reuses the body buffer once the handler returns.
	return &entity.PutFilesRequest{UserID: rq.UserID, Anonymous: localAnonymous(c), FolderID: rq.FolderID, Files: []entity.FileUpload{{Path: rq.Path, Data: bytes.Clone(c.Body())}}}
}

// FromHTTPRequestToDeleteFileRequestEntity reads DELETE /folders/:id/files/*.
func FromHTTPRequestToDeleteFileRequestEntity(c fiber.Ctx) *entity.DeleteFileRequest {
	rq := fileRequest(c)
	if rq == nil {
		return nil
	}
	return &entity.DeleteFileRequest{UserID: rq.UserID, FolderID: rq.FolderID, Path: rq.Path}
}

// FromHTTPRequestToOpenFileRequestEntity reads GET /folders/:id/files/*.
func FromHTTPRequestToOpenFileRequestEntity(c fiber.Ctx) *entity.OpenFileRequest {
	return fileRequest(c)
}

// FromHTTPRequestToOpenPublicFileRequestEntity reads a request to a
// folder's host, f<folderID>.<domain>; the whole path is the file
// path. It returns nil when the host is not a folder host.
func FromHTTPRequestToOpenPublicFileRequestEntity(c fiber.Ctx, domain string) *entity.OpenPublicFileRequest {
	host := strings.ToLower(string(c.Request().Host()))
	label, ok := strings.CutSuffix(host, "."+strings.ToLower(domain))
	if !ok || domain == "" {
		return nil
	}
	folder, ok := strings.CutPrefix(label, "f")
	folderID := idgen.ParseFolderID(folder)
	if !ok || folderID == 0 {
		return nil
	}
	p, err := url.PathUnescape(strings.TrimPrefix(c.Path(), "/"))
	if err != nil {
		return nil
	}
	return &entity.OpenPublicFileRequest{FolderID: folderID, Path: p}
}

// FromHTTPRequestToOpenPublicFileByPathRequestEntity reads a request to
// /files/:user/:folder/*, where the wildcard is the file path. It returns
// nil for malformed IDs.
func FromHTTPRequestToOpenPublicFileByPathRequestEntity(c fiber.Ctx) *entity.OpenPublicFileRequest {
	userID, folderID := idgen.ParseBase36(c.Params("user")), idgen.ParseFolderID(c.Params("folder"))
	if userID == 0 || folderID == 0 {
		return nil
	}
	p, err := url.PathUnescape(c.Params("*"))
	if err != nil {
		return nil
	}
	// The wildcard drops a trailing slash, which asks for a directory's
	// index.html.
	if p != "" && strings.HasSuffix(c.Path(), "/") {
		p += "/"
	}
	return &entity.OpenPublicFileRequest{UserID: userID, FolderID: folderID, Path: p}
}

// FromGetLimitsResponseEntityToHTTPResponse renders GET /limits.
func FromGetLimitsResponseEntityToHTTPResponse(rs *entity.GetLimitsResponse) []byte {
	return marshal(view.Limits{
		DefaultTTLMinutes:    rs.DefaultTTLMinutes,
		MaxTTLMinutes:        rs.MaxTTLMinutes,
		MaxUploadBytes:       rs.MaxUploadBytes,
		BillingBlockMinutes:  rs.BillingBlockMinutes,
		BillingUnitBytes:     rs.BillingUnitBytes,
		Anonymous:            rs.Anonymous,
		FileOps:              rs.FileOps,
		FileOpsWindowMinutes: rs.FileOpsWindowMinutes,
	})
}

// FromCreateFolderResponseEntityToHTTPResponse renders POST /folders.
func FromCreateFolderResponseEntityToHTTPResponse(rs *entity.CreateFolderResponse) []byte {
	return marshal(view.FolderResponse{Folder: FromFolderEntityToFolderView(rs.Folder)})
}

// FromListFoldersResponseEntityToHTTPResponse renders GET /folders.
func FromListFoldersResponseEntityToHTTPResponse(rs *entity.ListFoldersResponse) []byte {
	folders := make([]view.Folder, len(rs.Folders))
	for i, f := range rs.Folders {
		folders[i] = FromFolderEntityToFolderView(f)
	}
	return marshal(view.ListFoldersResponse{Folders: folders})
}

// FromGetFolderResponseEntityToHTTPResponse renders GET /folders/:id.
func FromGetFolderResponseEntityToHTTPResponse(rs *entity.GetFolderResponse) []byte {
	return marshal(view.GetFolderResponse{Folder: FromFolderEntityToFolderView(rs.Folder), Files: FromFileEntitiesToFileViews(rs.Files)})
}

// FromUpdateFolderResponseEntityToHTTPResponse renders PATCH /folders/:id.
func FromUpdateFolderResponseEntityToHTTPResponse(rs *entity.UpdateFolderResponse) []byte {
	return marshal(view.FolderResponse{Folder: FromFolderEntityToFolderView(rs.Folder)})
}

// FromPutFilesResponseEntityToHTTPResponse renders PUT /folders/:id/files/*,
// which writes exactly one file.
func FromPutFilesResponseEntityToHTTPResponse(rs *entity.PutFilesResponse) []byte {
	return marshal(view.PutFileResponse{File: FromFileEntityToFileView(rs.Files[0]), CreditsSpent: rs.CreditsSpent})
}

// FromFolderEntityToFolderView maps a folder to its JSON view.
func FromFolderEntityToFolderView(e entity.Folder) view.Folder {
	return view.Folder{
		ID:           idgen.FormatFolderID(e.ID),
		CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:    e.ExpiresAt.UTC().Format(time.RFC3339),
		URL:          e.URL,
		Title:        e.Title,
		DefaultTitle: e.DefaultTitle,
	}
}

// FromFileEntityToFileView maps a file to its JSON view.
func FromFileEntityToFileView(e entity.File) view.File {
	return view.File{Path: e.Path, Size: e.Size, UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339), URL: e.URL}
}

// FromFileEntitiesToFileViews maps files to their JSON views.
func FromFileEntitiesToFileViews(es []entity.File) []view.File {
	out := make([]view.File, len(es))
	for i, e := range es {
		out[i] = FromFileEntityToFileView(e)
	}
	return out
}

// fileRequest reads the user, folder ID and file path of a
// /folders/:id/files/* route.
func fileRequest(c fiber.Ctx) *entity.OpenFileRequest {
	userID, folderID := localUserID(c), idgen.ParseFolderID(c.Params("id"))
	p, err := url.PathUnescape(c.Params("*"))
	if userID == 0 || folderID == 0 || err != nil || p == "" {
		return nil
	}
	return &entity.OpenFileRequest{UserID: userID, FolderID: folderID, Path: p}
}

// localUserID is the user the auth middleware authenticated, or 0.
func localUserID(c fiber.Ctx) int64 {
	id, ok := c.Locals(LocalUserID).(int64)
	if !ok || id <= 0 {
		return 0
	}
	return id
}

// localAnonymous reports whether the auth middleware let the request in
// as an anonymous visitor.
func localAnonymous(c fiber.Ctx) bool {
	anonymous, _ := c.Locals(LocalAnonymous).(bool)
	return anonymous
}
