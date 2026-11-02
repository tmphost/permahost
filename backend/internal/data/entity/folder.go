// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

import (
	"io"
	"time"
)

type (
	// Folder is a group of uploaded files that expire together.
	Folder struct {
		ID        int64
		UserID    int64
		CreatedAt time.Time
		ExpiresAt time.Time
		// URL is the folder's public URL; its files are served below it.
		URL string
		// Title is the name the owner gave the folder; empty until renamed.
		Title string
		// DefaultTitle names a folder without a title: the name of its only
		// file, else its ID.
		DefaultTitle string
	}

	// File is an uploaded file.
	File struct {
		// Path is the file's slash-separated path inside its folder.
		Path      string
		Size      int64
		UpdatedAt time.Time
		URL       string
	}

	// FileUpload is a file to store.
	FileUpload struct {
		Path string
		Data []byte
	}

	// GetLimitsRequest asks for the limits that apply to the caller.
	GetLimitsRequest struct {
		Anonymous bool
	}

	// GetLimitsResponse carries the upload limits and pricing.
	GetLimitsResponse struct {
		DefaultTTLMinutes   int
		MaxTTLMinutes       int
		MaxUploadBytes      int64
		BillingBlockMinutes int
		BillingUnitBytes    int64
		// Anonymous limits replace credits with a per-IP quota: at most
		// FileOps file writes per FileOpsWindowMinutes.
		Anonymous            bool
		FileOps              int
		FileOpsWindowMinutes int
	}

	// CreateFolderRequest creates an empty folder.
	CreateFolderRequest struct {
		UserID int64
		// Anonymous marks a visitor without an account, who has the
		// anonymous limits.
		Anonymous bool
		// TTLMinutes is the folder's lifetime; 0 picks the default.
		TTLMinutes int
	}

	// CreateFolderResponse carries the new folder.
	CreateFolderResponse struct {
		Folder Folder
	}

	// ListFoldersRequest lists a user's live folders.
	ListFoldersRequest struct {
		UserID int64
	}

	// ListFoldersResponse carries live folders, newest first.
	ListFoldersResponse struct {
		Folders []Folder
	}

	// GetFolderRequest asks for one of a user's folders.
	GetFolderRequest struct {
		UserID   int64
		FolderID int64
	}

	// GetFolderResponse carries a folder and its files.
	GetFolderResponse struct {
		Folder Folder
		Files  []File
	}

	// UpdateFolderRequest renames one of a user's folders.
	UpdateFolderRequest struct {
		UserID   int64
		FolderID int64
		// Title is the new title; empty clears it.
		Title string
	}

	// UpdateFolderResponse carries the renamed folder.
	UpdateFolderResponse struct {
		Folder Folder
	}

	// DeleteFolderRequest deletes a folder and its files.
	DeleteFolderRequest struct {
		UserID   int64
		FolderID int64
	}

	// PutFilesRequest writes files into a folder.
	PutFilesRequest struct {
		UserID int64
		// Anonymous marks a visitor without an account: the upload spends
		// their per-IP quota instead of credits.
		Anonymous bool
		FolderID  int64
		Files     []FileUpload
	}

	// PutFilesResponse carries the written files and their cost.
	PutFilesResponse struct {
		Files        []File
		CreditsSpent int
	}

	// DeleteFileRequest deletes one file of a folder.
	DeleteFileRequest struct {
		UserID   int64
		FolderID int64
		Path     string
	}

	// OpenFileRequest asks for one of a user's files.
	OpenFileRequest struct {
		UserID   int64
		FolderID int64
		Path     string
	}

	// OpenPublicFileRequest asks for a file by its public URL parts.
	OpenPublicFileRequest struct {
		// UserID is the owner named in the folder's path URL; 0 for a
		// folder host, which names no owner.
		UserID   int64
		FolderID int64
		// Path is the file path; empty or ending in "/" means the
		// directory's index.html.
		Path string
	}

	// OpenFileResponse is an open file. The caller closes Body.
	OpenFileResponse struct {
		Body        io.ReadCloser
		Size        int64
		ContentType string
		ModTime     time.Time
	}

	// DeleteExpiredFoldersResponse reports how many folders were removed.
	DeleteExpiredFoldersResponse struct {
		Deleted int
	}
)
