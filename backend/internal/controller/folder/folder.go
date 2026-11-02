// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package folder implements file hosting: users upload files into folders,
// and every file is served at a public URL for as long as the account pays
// for the folder (see package credit).
//
// Files live in storage under u<userID>/<folderID>/<path>; the database
// records each folder's owner and size. A folder is served below the app,
// at /files/<userID>/<folderID>/, or with subdomains on, on its own
// origin, f<folderID>.<public domain>, and on any custom domain verified
// for it. Both IDs are base36 because hostnames are case-insensitive;
// folder IDs are random and always 11 characters (idgen.FormatFolderID).
// Creating a folder costs credit.FolderCredits, and an upload that takes
// the account's storage into a new storage block costs credit.StorageCredits
// per block.
//
// Anonymous web visitors (idgen.AnonymousUserID) pay no credits. Their
// folders expire after a TTL instead, and they have lower limits and a
// per-IP quota of file writes and new folders.
package folder

//go:generate mockgen -source=folder.go -destination=mocks/folder.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tmphost/permahost/backend/internal/controller/credit"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	mappermodel "github.com/tmphost/permahost/backend/internal/mapper/model"
	"github.com/tmphost/permahost/backend/internal/repository"
	"github.com/tmphost/permahost/backend/internal/service/idgen"
	"github.com/tmphost/permahost/backend/internal/service/quota"
	"github.com/tmphost/permahost/backend/internal/service/storage"
)

const (
	// DefaultMaxUploadMB caps an upload request unless root changes it.
	DefaultMaxUploadMB = 16
	// SweepSchedule is the cron spec of the job that deletes expired
	// anonymous folders.
	SweepSchedule = "* * * * *"
	// MaxDirDepth is how many directories deep a file can be in a folder.
	MaxDirDepth = 5
	// PublicPathPrefix is where folders are served on the app origin when
	// subdomains are off: <base URL>/files/<userID>/<folderID>/<path>.
	PublicPathPrefix = "/files/"

	// DefaultAnonymousMaxTTLMinutes is the longest lifetime of an
	// anonymous folder unless root changes it; it is also their default.
	DefaultAnonymousMaxTTLMinutes = 10
	// AnonymousMaxUploadMB caps an anonymous upload request.
	AnonymousMaxUploadMB = 4
	// AnonymousOps is how many files an anonymous visitor may write, and
	// how many folders they may create, per AnonymousWindow.
	AnonymousOps = 5
	// AnonymousWindow is the window of the anonymous quota.
	AnonymousWindow = 30 * time.Minute

	// MaxTitleLength is how many characters a folder title can have.
	MaxTitleLength = 100

	_maxPathLength    = 512
	_maxSegmentLength = 255
	_indexFile        = "index.html"
	_sweepBatch       = 500
	// Local storage writes uploads to temp files with this prefix.
	_reservedPrefix = ".upload-"
)

var (
	// ErrNotFound is returned for a folder or file that does not exist,
	// has expired, or belongs to someone else.
	ErrNotFound = errors.New("not found")
	// ErrSuspended is returned for a public file of an account whose
	// balance is below zero: its sites are paused until it pays again.
	ErrSuspended = errors.New("site paused")
	// ErrInvalidTTL is returned for a TTL out of range.
	ErrInvalidTTL = errors.New("invalid ttl")
	// ErrInvalidPath is returned for a file path that is not a clean,
	// relative, slash-separated path at most MaxDirDepth directories deep.
	ErrInvalidPath = errors.New("invalid file path")
	// ErrInvalidTitle is returned for a folder title that is too long or
	// has control characters.
	ErrInvalidTitle = errors.New("invalid folder title")
	// ErrNoFiles is returned for an upload without files.
	ErrNoFiles = errors.New("no files to upload")
	// ErrDuplicatePath is returned when an upload names a path twice.
	ErrDuplicatePath = errors.New("duplicate file path")
	// ErrTooLarge is returned for an upload over the size limit.
	ErrTooLarge = errors.New("upload too large")
	// ErrInsufficientCredits is returned when the user cannot pay for an
	// upload.
	ErrInsufficientCredits = errors.New("insufficient credits")
	// ErrQuotaExceeded is returned when an anonymous visitor has used up
	// their quota for the current window.
	ErrQuotaExceeded = errors.New("anonymous quota exceeded")
)

type (
	// Controller is the file hosting business logic.
	Controller interface {
		GetLimits(ctx context.Context, rq *entity.GetLimitsRequest) *entity.GetLimitsResponse
		CreateFolder(ctx context.Context, rq *entity.CreateFolderRequest) (*entity.CreateFolderResponse, error)
		ListFolders(ctx context.Context, rq *entity.ListFoldersRequest) (*entity.ListFoldersResponse, error)
		GetFolder(ctx context.Context, rq *entity.GetFolderRequest) (*entity.GetFolderResponse, error)
		// UpdateFolder renames a folder; an empty title clears it.
		UpdateFolder(ctx context.Context, rq *entity.UpdateFolderRequest) (*entity.UpdateFolderResponse, error)
		DeleteFolder(ctx context.Context, rq *entity.DeleteFolderRequest) error
		PutFiles(ctx context.Context, rq *entity.PutFilesRequest) (*entity.PutFilesResponse, error)
		DeleteFile(ctx context.Context, rq *entity.DeleteFileRequest) error
		// OpenFile opens one of a user's files.
		OpenFile(ctx context.Context, rq *entity.OpenFileRequest) (*entity.OpenFileResponse, error)
		// OpenPublicFile opens a file of a live folder for anyone.
		OpenPublicFile(ctx context.Context, rq *entity.OpenPublicFileRequest) (*entity.OpenFileResponse, error)
		// DeleteExpiredFolders removes a batch of expired anonymous
		// folders, files first, so a failure leaves the record for the
		// next run.
		DeleteExpiredFolders(ctx context.Context) (*entity.DeleteExpiredFoldersResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Folders repository.FolderRepository
		Users   repository.UserRepository
		// Domains are removed with their folder.
		Domains repository.DomainRepository
		Storage storage.Service
		// Anonymous counts anonymous file writes and new folders, per user;
		// AnonymousOps per AnonymousWindow.
		Anonymous quota.Service
		// NewID returns a new folder ID; idgen.NewFolderID when nil.
		NewID func() int64
		// Limits returns the current file settings; they can change at
		// runtime.
		Limits func() entity.FileSettings
		// BaseURL is the app's public URL. Folders are served below it,
		// or on its scheme with subdomains.
		BaseURL string
		Now     func() time.Time
	}

	controller struct {
		folders   repository.FolderRepository
		users     repository.UserRepository
		domains   repository.DomainRepository
		storage   storage.Service
		anonymous quota.Service
		newID     func() int64
		limits    func() entity.FileSettings
		baseURL   string
		scheme    string
		now       func() time.Time
	}
)

// New returns the folder controller.
func New(p Params) Controller {
	c := &controller{
		folders:   p.Folders,
		users:     p.Users,
		domains:   p.Domains,
		storage:   p.Storage,
		anonymous: p.Anonymous,
		newID:     p.NewID,
		limits:    p.Limits,
		baseURL:   strings.TrimRight(p.BaseURL, "/"),
		scheme:    "http",
		now:       p.Now,
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.newID == nil {
		c.newID = idgen.NewFolderID
	}
	if u, err := url.Parse(p.BaseURL); err == nil && u.Scheme != "" {
		c.scheme = u.Scheme
	}
	return c
}

func (c *controller) GetLimits(_ context.Context, rq *entity.GetLimitsRequest) *entity.GetLimitsResponse {
	rs := &entity.GetLimitsResponse{
		MaxUploadBytes: int64(c.maxUploadMB(rq.Anonymous)) << 20,
		Pricing:        credit.CurrentPricing(),
		Anonymous:      rq.Anonymous,
	}
	if rq.Anonymous {
		maxTTL := c.limits().AnonymousMaxTTLMinutes
		rs.DefaultTTLMinutes, rs.MaxTTLMinutes = maxTTL, maxTTL
		rs.FileOps, rs.FileOpsWindowMinutes = AnonymousOps, int(AnonymousWindow/time.Minute)
	}
	return rs
}

func (c *controller) CreateFolder(ctx context.Context, rq *entity.CreateFolderRequest) (*entity.CreateFolderResponse, error) {
	if rq.Anonymous {
		return c.createAnonymousFolder(ctx, rq)
	}
	if err := c.spend(ctx, rq.UserID, credit.FolderCredits); err != nil {
		return nil, err
	}
	f, err := c.folders.CreateFolder(ctx, model.Folder{ID: c.newID(), CreatedAt: c.now().UTC(), UserID: rq.UserID})
	if err != nil {
		err = fmt.Errorf("create folder: %w", err)
		if rerr := c.users.RefundCredits(ctx, rq.UserID, credit.FolderCredits); rerr != nil {
			err = errors.Join(err, fmt.Errorf("refund credits: %w", rerr))
		}
		return nil, err
	}
	return &entity.CreateFolderResponse{Folder: c.folderEntity(f), CreditsSpent: credit.FolderCredits}, nil
}

// createAnonymousFolder makes a folder that expires, paid for with the
// visitor's quota.
func (c *controller) createAnonymousFolder(ctx context.Context, rq *entity.CreateFolderRequest) (*entity.CreateFolderResponse, error) {
	maxTTL := time.Duration(c.limits().AnonymousMaxTTLMinutes) * time.Minute
	ttl := time.Duration(rq.TTLMinutes) * time.Minute
	if rq.TTLMinutes == 0 {
		ttl = maxTTL
	}
	if ttl < time.Minute || ttl > maxTTL {
		return nil, fmt.Errorf("%w: must be between 1 and %d minutes", ErrInvalidTTL, int(maxTTL/time.Minute))
	}
	if !c.anonymous.Take(folderQuotaKey(rq.UserID), 1) {
		return nil, fmt.Errorf("%w: at most %d new folders per %d minutes", ErrQuotaExceeded, AnonymousOps, int(AnonymousWindow/time.Minute))
	}
	now := c.now().UTC()
	expiresAt := now.Add(ttl)
	f, err := c.folders.CreateFolder(ctx, model.Folder{ID: c.newID(), CreatedAt: now, UserID: rq.UserID, ExpiresAt: &expiresAt})
	if err != nil {
		c.anonymous.Give(folderQuotaKey(rq.UserID), 1)
		return nil, fmt.Errorf("create folder: %w", err)
	}
	return &entity.CreateFolderResponse{Folder: c.folderEntity(f)}, nil
}

// spend takes n credits from a user.
func (c *controller) spend(ctx context.Context, userID int64, n int) error {
	err := c.users.SpendCredits(ctx, userID, n)
	if errors.Is(err, repository.ErrInsufficientCredits) {
		return fmt.Errorf("%w: %d needed", ErrInsufficientCredits, n)
	}
	if err != nil {
		return fmt.Errorf("spend credits: %w", err)
	}
	return nil
}

func (c *controller) ListFolders(ctx context.Context, rq *entity.ListFoldersRequest) (*entity.ListFoldersResponse, error) {
	folders, err := c.folders.ListFoldersByUser(ctx, rq.UserID, c.now())
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	out := make([]entity.Folder, len(folders))
	for i, f := range folders {
		out[i] = c.folderEntity(f)
		// A folder whose files cannot be listed still shows, under its ID.
		if objects, err := c.storage.List(ctx, folderPrefix(f)); err == nil {
			out[i].DefaultTitle = defaultTitle(f, objects)
		}
	}
	return &entity.ListFoldersResponse{Folders: out}, nil
}

func (c *controller) GetFolder(ctx context.Context, rq *entity.GetFolderRequest) (*entity.GetFolderResponse, error) {
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	prefix := folderPrefix(f)
	objects, err := c.storage.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	files := make([]entity.File, len(objects))
	for i, o := range objects {
		p := strings.TrimPrefix(o.Key, prefix)
		files[i] = entity.File{Path: p, Size: o.Size, UpdatedAt: o.ModTime.UTC(), URL: c.fileURL(f, p)}
	}
	folder := c.folderEntity(f)
	folder.DefaultTitle = defaultTitle(f, objects)
	return &entity.GetFolderResponse{Folder: folder, Files: files}, nil
}

func (c *controller) UpdateFolder(ctx context.Context, rq *entity.UpdateFolderRequest) (*entity.UpdateFolderResponse, error) {
	title := strings.TrimSpace(rq.Title)
	if !ValidTitle(title) {
		return nil, fmt.Errorf("%w: at most %d characters, no control characters", ErrInvalidTitle, MaxTitleLength)
	}
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	if err := c.folders.UpdateFolderTitle(ctx, f.ID, title); err != nil {
		return nil, fmt.Errorf("update folder: %w", err)
	}
	f.Title = title
	objects, err := c.storage.List(ctx, folderPrefix(f))
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	folder := c.folderEntity(f)
	folder.DefaultTitle = defaultTitle(f, objects)
	return &entity.UpdateFolderResponse{Folder: folder}, nil
}

// ValidTitle reports whether t can be a folder title: at most
// MaxTitleLength characters of valid UTF-8 with no control characters.
// The empty title is valid; it clears the title.
func ValidTitle(t string) bool {
	if !utf8.ValidString(t) || utf8.RuneCountInString(t) > MaxTitleLength {
		return false
	}
	for _, r := range t {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// defaultTitle names a folder that has no title: the name of its only
// file, else its ID.
func defaultTitle(f model.Folder, objects []storage.Object) string {
	if len(objects) == 1 {
		return path.Base(objects[0].Key)
	}
	return idgen.FormatFolderID(f.ID)
}

func (c *controller) DeleteFolder(ctx context.Context, rq *entity.DeleteFolderRequest) error {
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return err
	}
	return c.deleteFolder(ctx, f)
}

func (c *controller) PutFiles(ctx context.Context, rq *entity.PutFilesRequest) (*entity.PutFilesResponse, error) {
	if err := c.validateUploads(rq.Files, c.maxUploadMB(rq.Anonymous)); err != nil {
		return nil, err
	}
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	prefix := folderPrefix(f)
	objects, err := c.storage.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	sizes := make(map[string]int64, len(objects))
	var size int64
	for _, o := range objects {
		sizes[strings.TrimPrefix(o.Key, prefix)] = o.Size
		size += o.Size
	}
	newSize := size
	for _, u := range rq.Files {
		newSize += int64(len(u.Data)) - sizes[u.Path]
	}

	spent := 0
	if rq.Anonymous {
		if !c.anonymous.Take(fileQuotaKey(rq.UserID), len(rq.Files)) {
			return nil, fmt.Errorf("%w: at most %d files per %d minutes", ErrQuotaExceeded, AnonymousOps, int(AnonymousWindow/time.Minute))
		}
	} else if spent, err = c.payStorage(ctx, rq.UserID, newSize-size); err != nil {
		return nil, err
	}

	now := c.now().UTC()
	files := make([]entity.File, 0, len(rq.Files))
	for i, u := range rq.Files {
		if err := c.storage.Put(ctx, prefix+u.Path, u.Data); err != nil {
			err = fmt.Errorf("write %s: %w", u.Path, err)
			// What is stored now is recorded; the renewal charges for it.
			_ = c.folders.UpdateFolderSize(ctx, f.ID, size)
			if rerr := c.refund(ctx, rq, len(rq.Files)-i, spent); rerr != nil {
				err = errors.Join(err, fmt.Errorf("refund credits: %w", rerr))
			}
			return nil, err
		}
		size += int64(len(u.Data)) - sizes[u.Path]
		sizes[u.Path] = int64(len(u.Data))
		files = append(files, entity.File{Path: u.Path, Size: int64(len(u.Data)), UpdatedAt: now, URL: c.fileURL(f, u.Path)})
	}
	if err := c.folders.UpdateFolderSize(ctx, f.ID, size); err != nil {
		return nil, fmt.Errorf("record folder size: %w", err)
	}
	return &entity.PutFilesResponse{Files: files, CreditsSpent: spent}, nil
}

// payStorage charges a user for the storage blocks growing their storage
// by delta bytes starts, and returns what it took.
func (c *controller) payStorage(ctx context.Context, userID int64, delta int64) (int, error) {
	_, stored, err := c.folders.GetUserFolderUsage(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("storage usage: %w", err)
	}
	blocks := credit.StorageBlocks(stored + delta)
	spent, err := c.users.PayStorageBlocks(ctx, userID, blocks, credit.StorageCredits)
	if errors.Is(err, repository.ErrInsufficientCredits) {
		return 0, fmt.Errorf("%w: %d credits a month per started %d MB stored", ErrInsufficientCredits, credit.StorageCredits, credit.StorageBlockBytes>>20)
	}
	if err != nil {
		return 0, fmt.Errorf("pay storage: %w", err)
	}
	return spent, nil
}

// refund gives back what an upload that failed with unwritten files left
// was paid with: the anonymous quota of those files, or every storage
// credit spent.
func (c *controller) refund(ctx context.Context, rq *entity.PutFilesRequest, unwritten, spent int) error {
	if rq.Anonymous {
		c.anonymous.Give(fileQuotaKey(rq.UserID), unwritten)
		return nil
	}
	if spent == 0 {
		return nil
	}
	return c.users.RefundStorageBlocks(ctx, rq.UserID, spent/credit.StorageCredits, spent)
}

func (c *controller) maxUploadMB(anonymous bool) int {
	if anonymous {
		return AnonymousMaxUploadMB
	}
	return c.limits().MaxUploadMB
}

func (c *controller) DeleteFile(ctx context.Context, rq *entity.DeleteFileRequest) error {
	if !ValidPath(rq.Path) {
		return ErrInvalidPath
	}
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return err
	}
	prefix := folderPrefix(f)
	if err := c.storage.Delete(ctx, prefix+rq.Path); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	objects, err := c.storage.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}
	var size int64
	for _, o := range objects {
		size += o.Size
	}
	if err := c.folders.UpdateFolderSize(ctx, f.ID, size); err != nil {
		return fmt.Errorf("record folder size: %w", err)
	}
	return nil
}

func (c *controller) OpenFile(ctx context.Context, rq *entity.OpenFileRequest) (*entity.OpenFileResponse, error) {
	if !ValidPath(rq.Path) {
		return nil, ErrInvalidPath
	}
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	return c.open(ctx, f, rq.Path)
}

func (c *controller) OpenPublicFile(ctx context.Context, rq *entity.OpenPublicFileRequest) (*entity.OpenFileResponse, error) {
	p := rq.Path
	if p == "" || strings.HasSuffix(p, "/") {
		p += _indexFile
	}
	if !ValidPath(p) {
		return nil, ErrNotFound
	}
	// A folder host names no owner; the path form does.
	f, err := c.liveFolder(ctx, rq.FolderID)
	if err != nil {
		return nil, err
	}
	if rq.UserID != 0 && f.UserID != rq.UserID {
		return nil, ErrNotFound
	}
	if err := c.checkPaid(ctx, f); err != nil {
		return nil, err
	}
	return c.open(ctx, f, p)
}

// checkPaid returns ErrSuspended for a folder whose owner's balance is
// below zero. Anonymous folders are paid for with their expiry.
func (c *controller) checkPaid(ctx context.Context, f model.Folder) error {
	if f.ExpiresAt != nil {
		return nil
	}
	u, err := c.users.FindUserByID(ctx, f.UserID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find owner: %w", err)
	}
	if u.Credits < 0 {
		return ErrSuspended
	}
	return nil
}

func (c *controller) DeleteExpiredFolders(ctx context.Context) (*entity.DeleteExpiredFoldersResponse, error) {
	folders, err := c.folders.ListExpiredFolders(ctx, c.now(), _sweepBatch)
	if err != nil {
		return &entity.DeleteExpiredFoldersResponse{}, fmt.Errorf("list expired folders: %w", err)
	}
	deleted := 0
	var errs []error
	// One failing folder must not hold up the others.
	for _, f := range folders {
		if err := c.deleteFolder(ctx, f); err != nil {
			errs = append(errs, fmt.Errorf("folder %d: %w", f.ID, err))
			continue
		}
		deleted++
	}
	return &entity.DeleteExpiredFoldersResponse{Deleted: deleted}, errors.Join(errs...)
}

// ValidPath reports whether p is a clean relative file path: slash
// separated, at most MaxDirDepth directories deep, no empty, "." or ".."
// segments, no backslashes or control characters, within the length
// limits. Storage keys are always u<user>/<folder>/<p>, so a valid p can
// never reach outside its folder.
func ValidPath(p string) bool {
	if p == "" || len(p) > _maxPathLength || !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return false
		}
	}
	segs := strings.Split(p, "/")
	if len(segs) > MaxDirDepth+1 {
		return false
	}
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." || len(seg) > _maxSegmentLength || strings.HasPrefix(seg, _reservedPrefix) {
			return false
		}
	}
	return true
}

func (c *controller) validateUploads(files []entity.FileUpload, maxUploadMB int) error {
	if len(files) == 0 {
		return ErrNoFiles
	}
	limit := int64(maxUploadMB) << 20
	var total int64
	seen := make(map[string]bool, len(files))
	for _, u := range files {
		if !ValidPath(u.Path) {
			return fmt.Errorf("%w: %q", ErrInvalidPath, u.Path)
		}
		if seen[u.Path] {
			return fmt.Errorf("%w: %q", ErrDuplicatePath, u.Path)
		}
		seen[u.Path] = true
		total += int64(len(u.Data))
	}
	if total > limit {
		return fmt.Errorf("%w: limit is %d MB", ErrTooLarge, limit>>20)
	}
	return nil
}

// liveFolder finds a folder that has not expired yet. Expired folders are
// gone to users even before the sweep removes them.
func (c *controller) liveFolder(ctx context.Context, id int64) (model.Folder, error) {
	f, err := c.folders.FindFolderByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return model.Folder{}, ErrNotFound
	}
	if err != nil {
		return model.Folder{}, fmt.Errorf("find folder: %w", err)
	}
	if f.ExpiresAt != nil && !c.now().Before(*f.ExpiresAt) {
		return model.Folder{}, ErrNotFound
	}
	return f, nil
}

func (c *controller) ownedFolder(ctx context.Context, userID, id int64) (model.Folder, error) {
	f, err := c.liveFolder(ctx, id)
	if err != nil {
		return model.Folder{}, err
	}
	if f.UserID != userID {
		return model.Folder{}, ErrNotFound
	}
	return f, nil
}

func (c *controller) open(ctx context.Context, f model.Folder, p string) (*entity.OpenFileResponse, error) {
	body, obj, err := c.storage.Get(ctx, folderPrefix(f)+p)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	contentType := mime.TypeByExtension(path.Ext(p))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return &entity.OpenFileResponse{Body: body, Size: obj.Size, ContentType: contentType, ModTime: obj.ModTime}, nil
}

func (c *controller) deleteFolder(ctx context.Context, f model.Folder) error {
	// The domains go first, so no host serves a folder being deleted.
	if err := c.domains.DeleteDomainsByFolder(ctx, f.ID); err != nil {
		return fmt.Errorf("delete domains: %w", err)
	}
	if err := c.storage.DeletePrefix(ctx, folderPrefix(f)); err != nil {
		return fmt.Errorf("delete files: %w", err)
	}
	if err := c.folders.DeleteFolder(ctx, f.ID); err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	return nil
}

func (c *controller) folderEntity(f model.Folder) entity.Folder {
	e := mappermodel.FromFolderModelToFolderEntity(f)
	e.CreatedAt, e.ExpiresAt = e.CreatedAt.UTC(), e.ExpiresAt.UTC()
	e.URL = c.fileURL(f, "")
	e.DefaultTitle = idgen.FormatFolderID(f.ID)
	return e
}

// fileURL is the public URL of a file, or of the folder for an empty path:
// on the folder's own host with subdomains on, else below the app's URL.
func (c *controller) fileURL(f model.Folder, p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	rel := strings.Join(segs, "/")
	if s := c.limits(); s.Subdomains {
		return c.scheme + "://" + FolderHost(f.ID) + "." + s.PublicDomain + "/" + rel
	}
	return c.baseURL + PublicPathPrefix + idgen.FormatBase36(f.UserID) + "/" + idgen.FormatFolderID(f.ID) + "/" + rel
}

// FolderHost is the leftmost hostname label a folder is served under,
// "f" and the folder ID.
func FolderHost(folderID int64) string {
	return "f" + idgen.FormatFolderID(folderID)
}

// folderPrefix is where a folder's files live in storage.
func folderPrefix(f model.Folder) string {
	return "u" + idgen.FormatBase36(f.UserID) + "/" + idgen.FormatFolderID(f.ID) + "/"
}

// fileQuotaKey and folderQuotaKey are an anonymous user's quota keys.
func fileQuotaKey(userID int64) string { return "files:" + idgen.FormatBase36(userID) }

func folderQuotaKey(userID int64) string { return "folders:" + idgen.FormatBase36(userID) }
