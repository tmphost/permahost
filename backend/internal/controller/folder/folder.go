// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package folder implements temporary file hosting: users upload files
// into folders that expire after a TTL, and every file is served at a
// public URL until then.
//
// Files live in storage under u<userID>/<folderID>/<path>; the database
// only records each folder's owner and expiry. A folder is served below
// the app, at /files/<userID>/<folderID>/, or with subdomains on, on its
// own origin, f<folderID>.<public domain>. Both IDs are base36 because
// hostnames are case-insensitive; folder IDs are random and always 11
// characters (idgen.FormatFolderID). Each file write costs one
// credit per started BillingUnit of its size per started BillingBlock of
// the folder's remaining lifetime.
//
// Anonymous web visitors (idgen.AnonymousUserID) pay no credits. They have
// lower limits instead, and a per-IP quota of file writes and new folders.
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

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/quota"
	"github.com/tmphost/tmphost/backend/internal/service/storage"
)

const (
	// DefaultTTL is a folder's lifetime when none is asked for.
	DefaultTTL = 30 * time.Minute
	// BillingBlock is the lifetime one credit pays for, per BillingUnit.
	BillingBlock = 30 * time.Minute
	// BillingUnit is the file size one credit pays for, per BillingBlock.
	BillingUnit = 1 << 20
	// DefaultMaxTTLMinutes is the longest TTL unless root changes it.
	DefaultMaxTTLMinutes = 7200 * 60
	// DefaultMaxUploadMB caps an upload request unless root changes it.
	DefaultMaxUploadMB = 16
	// SweepSchedule is the cron spec of the job that deletes expired
	// folders.
	SweepSchedule = "* * * * *"
	// MaxDirDepth is how many directories deep a file can be in a folder.
	MaxDirDepth = 5
	// PublicPathPrefix is where folders are served on the app origin when
	// subdomains are off: <base URL>/files/<userID>/<folderID>/<path>.
	PublicPathPrefix = "/files/"

	// DefaultAnonymousMaxTTLMinutes is the longest lifetime of an
	// anonymous folder unless root changes it.
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
		// DeleteExpiredFolders removes a batch of expired folders, files
		// first, so a failure leaves the record for the next run.
		DeleteExpiredFolders(ctx context.Context) (*entity.DeleteExpiredFoldersResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Folders repository.FolderRepository
		Users   repository.UserRepository
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

// Cost is the credits writing a file of size bytes into a folder with
// remaining lifetime left costs: one per started BillingUnit per started
// BillingBlock. Empty files and expiring folders still cost one of each.
func Cost(size int64, remaining time.Duration) int {
	units := int((size + BillingUnit - 1) / BillingUnit)
	blocks := int((remaining + BillingBlock - 1) / BillingBlock)
	return max(units, 1) * max(blocks, 1)
}

func (c *controller) GetLimits(_ context.Context, rq *entity.GetLimitsRequest) *entity.GetLimitsResponse {
	maxTTL, maxUploadMB := c.maxTTLMinutes(rq.Anonymous), c.maxUploadMB(rq.Anonymous)
	rs := &entity.GetLimitsResponse{
		DefaultTTLMinutes:   min(int(DefaultTTL/time.Minute), maxTTL),
		MaxTTLMinutes:       maxTTL,
		MaxUploadBytes:      int64(maxUploadMB) << 20,
		BillingBlockMinutes: int(BillingBlock / time.Minute),
		BillingUnitBytes:    BillingUnit,
		Anonymous:           rq.Anonymous,
	}
	if rq.Anonymous {
		rs.FileOps, rs.FileOpsWindowMinutes = AnonymousOps, int(AnonymousWindow/time.Minute)
	}
	return rs
}

func (c *controller) CreateFolder(ctx context.Context, rq *entity.CreateFolderRequest) (*entity.CreateFolderResponse, error) {
	maxTTL := c.maxTTLMinutes(rq.Anonymous)
	ttl := time.Duration(rq.TTLMinutes) * time.Minute
	if rq.TTLMinutes == 0 {
		ttl = min(DefaultTTL, time.Duration(maxTTL)*time.Minute)
	}
	if ttl < time.Minute || ttl > time.Duration(maxTTL)*time.Minute {
		return nil, fmt.Errorf("%w: must be between 1 and %d minutes", ErrInvalidTTL, maxTTL)
	}
	if rq.Anonymous && !c.anonymous.Take(folderQuotaKey(rq.UserID), 1) {
		return nil, fmt.Errorf("%w: at most %d new folders per %d minutes", ErrQuotaExceeded, AnonymousOps, int(AnonymousWindow/time.Minute))
	}
	now := c.now().UTC()
	f, err := c.folders.CreateFolder(ctx, model.Folder{
		ID:        c.newID(),
		CreatedAt: now,
		UserID:    rq.UserID,
		ExpiresAt: now.Add(ttl),
	})
	if err != nil {
		if rq.Anonymous {
			c.anonymous.Give(folderQuotaKey(rq.UserID), 1)
		}
		return nil, fmt.Errorf("create folder: %w", err)
	}
	return &entity.CreateFolderResponse{Folder: c.folderEntity(f)}, nil
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
	now := c.now()
	costs := make([]int, len(rq.Files))
	total := 0
	if rq.Anonymous {
		if !c.anonymous.Take(fileQuotaKey(rq.UserID), len(rq.Files)) {
			return nil, fmt.Errorf("%w: at most %d files per %d minutes", ErrQuotaExceeded, AnonymousOps, int(AnonymousWindow/time.Minute))
		}
	} else {
		for i, u := range rq.Files {
			costs[i] = Cost(int64(len(u.Data)), f.ExpiresAt.Sub(now))
			total += costs[i]
		}
		err = c.users.SpendCredits(ctx, rq.UserID, total)
		if errors.Is(err, repository.ErrInsufficientCredits) {
			return nil, fmt.Errorf("%w: %d needed", ErrInsufficientCredits, total)
		}
		if err != nil {
			return nil, fmt.Errorf("spend credits: %w", err)
		}
	}

	files := make([]entity.File, 0, len(rq.Files))
	for i, u := range rq.Files {
		if err := c.storage.Put(ctx, folderPrefix(f)+u.Path, u.Data); err != nil {
			// The files not written are not paid for.
			if rerr := c.refund(ctx, rq, costs[i:]); rerr != nil {
				err = errors.Join(err, fmt.Errorf("refund credits: %w", rerr))
			}
			return nil, fmt.Errorf("write %s: %w", u.Path, err)
		}
		files = append(files, entity.File{Path: u.Path, Size: int64(len(u.Data)), UpdatedAt: now.UTC(), URL: c.fileURL(f, u.Path)})
	}
	return &entity.PutFilesResponse{Files: files, CreditsSpent: total}, nil
}

// refund gives back what the unwritten files of rq, costing costs, were
// paid with.
func (c *controller) refund(ctx context.Context, rq *entity.PutFilesRequest, costs []int) error {
	if rq.Anonymous {
		c.anonymous.Give(fileQuotaKey(rq.UserID), len(costs))
		return nil
	}
	sum := 0
	for _, n := range costs {
		sum += n
	}
	return c.users.RefundCredits(ctx, rq.UserID, sum)
}

func (c *controller) maxTTLMinutes(anonymous bool) int {
	if anonymous {
		return c.limits().AnonymousMaxTTLMinutes
	}
	return c.limits().MaxTTLMinutes
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
	if err := c.storage.Delete(ctx, folderPrefix(f)+rq.Path); err != nil {
		return fmt.Errorf("delete file: %w", err)
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
	return c.open(ctx, f, p)
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
	if !c.now().Before(f.ExpiresAt) {
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
