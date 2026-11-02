// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// Limits are the upload limits and pricing.
	Limits struct {
		MaxUploadBytes int64   `json:"maxUploadBytes"`
		Pricing        Pricing `json:"pricing"`
		// Anonymous visitors pay no credits: their folders expire, and they
		// may write at most FileOps files per FileOpsWindowMinutes.
		DefaultTTLMinutes    int  `json:"defaultTtlMinutes,omitempty"`
		MaxTTLMinutes        int  `json:"maxTtlMinutes,omitempty"`
		Anonymous            bool `json:"anonymous,omitempty"`
		FileOps              int  `json:"fileOps,omitempty"`
		FileOpsWindowMinutes int  `json:"fileOpsWindowMinutes,omitempty"`
	}

	// Pricing is what hosting costs, in credits.
	Pricing struct {
		StorageBlockBytes  int64 `json:"storageBlockBytes"`
		StorageCredits     int   `json:"storageCredits"`
		FolderCredits      int   `json:"folderCredits"`
		DomainCredits      int   `json:"domainCredits"`
		DomainSetupCredits int   `json:"domainSetupCredits"`
	}

	// Folder is a group of files hosted together.
	Folder struct {
		ID        string `json:"id"`
		CreatedAt string `json:"createdAt"`
		// ExpiresAt is set only for an anonymous folder.
		ExpiresAt string `json:"expiresAt,omitempty"`
		SizeBytes int64  `json:"sizeBytes"`
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

	// CreateFolderResponse is the new folder and what it cost.
	CreateFolderResponse struct {
		Folder       Folder `json:"folder"`
		CreditsSpent int    `json:"creditsSpent"`
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
