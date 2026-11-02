// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// Limits are the upload limits and pricing.
	Limits struct {
		DefaultTTLMinutes   int   `json:"defaultTtlMinutes"`
		MaxTTLMinutes       int   `json:"maxTtlMinutes"`
		MaxUploadBytes      int64 `json:"maxUploadBytes"`
		BillingBlockMinutes int   `json:"billingBlockMinutes"`
		BillingUnitBytes    int64 `json:"billingUnitBytes"`
		// Anonymous visitors pay no credits; they may write at most FileOps
		// files per FileOpsWindowMinutes instead.
		Anonymous            bool `json:"anonymous,omitempty"`
		FileOps              int  `json:"fileOps,omitempty"`
		FileOpsWindowMinutes int  `json:"fileOpsWindowMinutes,omitempty"`
	}

	// Folder is a group of files that expire together.
	Folder struct {
		ID        string `json:"id"`
		CreatedAt string `json:"createdAt"`
		ExpiresAt string `json:"expiresAt"`
		URL       string `json:"url"`
		// Title is the name the owner gave the folder; empty until renamed.
		Title string `json:"title"`
		// DefaultTitle is shown while Title is empty: the name of the
		// folder's only file, else its ID.
		DefaultTitle string `json:"defaultTitle"`
	}

	// File is an uploaded file.
	File struct {
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		UpdatedAt string `json:"updatedAt"`
		URL       string `json:"url"`
	}

	// CreateFolderRequest is the POST /folders body.
	CreateFolderRequest struct {
		TTLMinutes int `json:"ttlMinutes"`
	}

	// UpdateFolderRequest is the PATCH /folders/:id body.
	UpdateFolderRequest struct {
		Title *string `json:"title"`
	}

	// FolderResponse wraps a folder.
	FolderResponse struct {
		Folder Folder `json:"folder"`
	}

	// ListFoldersResponse lists folders.
	ListFoldersResponse struct {
		Folders []Folder `json:"folders"`
	}

	// GetFolderResponse is a folder and its files.
	GetFolderResponse struct {
		Folder Folder `json:"folder"`
		Files  []File `json:"files"`
	}

	// PutFileResponse is the written file and what it cost.
	PutFileResponse struct {
		File         File `json:"file"`
		CreditsSpent int  `json:"creditsSpent"`
	}
)
