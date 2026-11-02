// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package folder

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/permahost/backend/internal/controller/credit"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	"github.com/tmphost/permahost/backend/internal/repository"
	mockrepo "github.com/tmphost/permahost/backend/internal/repository/mocks"
	"github.com/tmphost/permahost/backend/internal/service/idgen"
	"github.com/tmphost/permahost/backend/internal/service/quota"
	"github.com/tmphost/permahost/backend/internal/service/storage"
	mockstorage "github.com/tmphost/permahost/backend/internal/service/storage/mocks"
)

var (
	_now    = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
	_host   = "https://f000000000rs.tmp.host/"
	_prefix = "u7/000000000rs/"
)

type env struct {
	ctrl    Controller
	folders *mockrepo.MockFolderRepository
	users   *mockrepo.MockUserRepository
	domains *mockrepo.MockDomainRepository
	storage *mockstorage.MockService
	nextID  int64
	limits  entity.FileSettings
}

func newEnv(t *testing.T) *env {
	t.Helper()
	mc := gomock.NewController(t)
	e := &env{
		folders: mockrepo.NewMockFolderRepository(mc),
		users:   mockrepo.NewMockUserRepository(mc),
		domains: mockrepo.NewMockDomainRepository(mc),
		storage: mockstorage.NewMockService(mc),
		limits:  entity.FileSettings{MaxUploadMB: 1, AnonymousMaxTTLMinutes: 10, Subdomains: true, PublicDomain: "tmp.host"},
	}
	e.ctrl = New(Params{
		Folders:   e.folders,
		Users:     e.users,
		Domains:   e.domains,
		Storage:   e.storage,
		Anonymous: quota.New(quota.Params{Limit: AnonymousOps, Window: AnonymousWindow, Now: func() time.Time { return _now }}),
		NewID:     func() int64 { return e.nextID },
		Limits:    func() entity.FileSettings { return e.limits },
		BaseURL:   "https://permahost.io/base/",
		Now:       func() time.Time { return _now },
	})
	return e
}

// liveFolder expects a lookup of folder 1000, an anonymous folder of
// user 7 that expires in left.
func (e *env) liveFolder(left time.Duration) model.Folder {
	expiresAt := _now.Add(left)
	f := model.Folder{ID: 1000, UserID: 7, CreatedAt: _now.Add(-time.Minute), ExpiresAt: &expiresAt}
	e.folders.EXPECT().FindFolderByID(gomock.Any(), int64(1000)).Return(f, nil)
	return f
}

// folder expects a lookup of folder 1000, user 7's folder that does not
// expire.
func (e *env) folder() model.Folder {
	f := model.Folder{ID: 1000, UserID: 7, CreatedAt: _now.Add(-time.Minute), SizeBytes: 3}
	e.folders.EXPECT().FindFolderByID(gomock.Any(), int64(1000)).Return(f, nil)
	return f
}

// anyStorage lets uploads list folders as empty and record their size.
func (e *env) anyStorage() {
	e.storage.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	e.folders.EXPECT().UpdateFolderSize(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
}

func TestNew_DefaultClock(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.WithinDuration(t, time.Now(), c.now(), time.Minute)
	assert.Equal(t, "http", c.scheme, "without a base URL")
	assert.Len(t, idgen.FormatFolderID(c.newID()), idgen.FolderIDLength, "random folder IDs")
}

func TestFolderURLs_PathMode(t *testing.T) {
	e := newEnv(t)
	e.limits.Subdomains = false
	f := e.folder()
	e.storage.EXPECT().List(gomock.Any(), "u7/000000000rs/").Return([]storage.Object{{Key: "u7/000000000rs/site/a b.html", Size: 1, ModTime: _now}}, nil)
	rs, err := e.ctrl.GetFolder(context.Background(), &entity.GetFolderRequest{UserID: f.UserID, FolderID: f.ID})
	require.NoError(t, err)
	assert.Equal(t, "https://permahost.io/base/files/7/000000000rs/", rs.Folder.URL)
	assert.Equal(t, "https://permahost.io/base/files/7/000000000rs/site/a%20b.html", rs.Files[0].URL)
}

func TestGetLimits(t *testing.T) {
	e := newEnv(t)
	assert.Equal(t, &entity.GetLimitsResponse{MaxUploadBytes: 1 << 20, Pricing: credit.CurrentPricing()},
		e.ctrl.GetLimits(context.Background(), &entity.GetLimitsRequest{}), "an account's folders do not expire")
	assert.Equal(t, &entity.GetLimitsResponse{
		DefaultTTLMinutes: 10, MaxTTLMinutes: 10, MaxUploadBytes: 4 << 20, Pricing: credit.CurrentPricing(),
		Anonymous: true, FileOps: 5, FileOpsWindowMinutes: 30,
	}, e.ctrl.GetLimits(context.Background(), &entity.GetLimitsRequest{Anonymous: true}))
}

func TestCreateFolder_Anonymous(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.nextID = 1000
	for _, m := range []int{-1, 11} {
		_, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Anonymous: true, TTLMinutes: m})
		assert.ErrorIs(t, err, ErrInvalidTTL, "anonymous folders live 1 to 10 minutes")
	}
	e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).
		DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) {
			assert.Equal(t, time.Minute, f.ExpiresAt.Sub(f.CreatedAt))
			return f, nil
		})
	rs, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 9, Anonymous: true, TTLMinutes: 1})
	require.NoError(t, err)
	assert.Equal(t, _now.Add(time.Minute), rs.Folder.ExpiresAt)
	assert.Zero(t, rs.CreditsSpent)

	e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).Return(model.Folder{}, errBoom)
	_, err = e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Anonymous: true})
	assert.ErrorIs(t, err, errBoom)

	e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).
		DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) {
			assert.Equal(t, 10*time.Minute, f.ExpiresAt.Sub(f.CreatedAt), "no TTL asked: the anonymous max")
			return f, nil
		}).Times(AnonymousOps)
	for range AnonymousOps {
		_, err = e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Anonymous: true})
		require.NoError(t, err, "a failed create gives its folder back")
	}
	_, err = e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Anonymous: true})
	assert.ErrorIs(t, err, ErrQuotaExceeded)

	e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).
		DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) { return f, nil })
	_, err = e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 8, Anonymous: true})
	require.NoError(t, err, "each user has their own quota")
}

func TestPutFiles_Anonymous(t *testing.T) {
	ctx := context.Background()
	put := func(e *env, files ...entity.FileUpload) (*entity.PutFilesResponse, error) {
		return e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, Anonymous: true, FolderID: 1000, Files: files})
	}

	t.Run("quota, not credits", func(t *testing.T) {
		e := newEnv(t)
		e.anyStorage()
		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil)
		e.storage.EXPECT().Put(ctx, _prefix+"b", gomock.Any()).Return(nil)
		rs, err := put(e, entity.FileUpload{Path: "a", Data: make([]byte, 2<<20)}, entity.FileUpload{Path: "b", Data: make([]byte, 2<<20)})
		require.NoError(t, err, "4 MB, over the signed-in limit of 1 MB")
		assert.Zero(t, rs.CreditsSpent)
		assert.Len(t, rs.Files, 2)

		e.liveFolder(30 * time.Minute)
		_, err = put(e, entity.FileUpload{Path: "c"}, entity.FileUpload{Path: "d"}, entity.FileUpload{Path: "e"}, entity.FileUpload{Path: "f"})
		assert.ErrorIs(t, err, ErrQuotaExceeded, "2 + 4 is over 5")

		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(3)
		_, err = put(e, entity.FileUpload{Path: "c"}, entity.FileUpload{Path: "d"}, entity.FileUpload{Path: "e"})
		require.NoError(t, err)

		e.liveFolder(30 * time.Minute)
		_, err = put(e, entity.FileUpload{Path: "g"})
		assert.ErrorIs(t, err, ErrQuotaExceeded)
	})
	t.Run("too large", func(t *testing.T) {
		e := newEnv(t)
		_, err := put(e, entity.FileUpload{Path: "a", Data: make([]byte, 4<<20+1)})
		assert.ErrorIs(t, err, ErrTooLarge)
	})
	t.Run("write failure gives the rest back", func(t *testing.T) {
		e := newEnv(t)
		e.anyStorage()
		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil)
		e.storage.EXPECT().Put(ctx, _prefix+"b", gomock.Any()).Return(errBoom)
		_, err := put(e, entity.FileUpload{Path: "a"}, entity.FileUpload{Path: "b"}, entity.FileUpload{Path: "c"})
		assert.ErrorIs(t, err, errBoom)
		assert.NotContains(t, err.Error(), "refund")

		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(4)
		_, err = put(e, entity.FileUpload{Path: "b"}, entity.FileUpload{Path: "c"}, entity.FileUpload{Path: "d"}, entity.FileUpload{Path: "e"})
		require.NoError(t, err, "only the written file counts: 1 + 4")
	})
}

func TestCreateFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.nextID = 1000
	gomock.InOrder(
		e.users.EXPECT().SpendCredits(ctx, int64(7), credit.FolderCredits).Return(nil),
		e.folders.EXPECT().CreateFolder(ctx, model.Folder{ID: 1000, CreatedAt: _now, UserID: 7}).
			DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) { return f, nil }),
	)
	rs, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, TTLMinutes: 5})
	require.NoError(t, err, "a TTL is ignored: an account's folders do not expire")
	assert.Equal(t, entity.Folder{ID: 1000, UserID: 7, CreatedAt: _now, URL: _host, DefaultTitle: "000000000rs"}, rs.Folder)
	assert.Equal(t, credit.FolderCredits, rs.CreditsSpent)
}

func TestCreateFolder_Errors(t *testing.T) {
	ctx := context.Background()
	rq := &entity.CreateFolderRequest{UserID: 7}

	t.Run("insufficient credits", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SpendCredits(ctx, int64(7), credit.FolderCredits).Return(repository.ErrInsufficientCredits)
		_, err := e.ctrl.CreateFolder(ctx, rq)
		assert.ErrorIs(t, err, ErrInsufficientCredits)
		assert.Contains(t, err.Error(), "500 needed")
	})
	t.Run("spend error", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SpendCredits(ctx, int64(7), credit.FolderCredits).Return(errBoom)
		_, err := e.ctrl.CreateFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("create fails: refunded", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SpendCredits(ctx, int64(7), credit.FolderCredits).Return(nil)
		e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).Return(model.Folder{}, errBoom)
		e.users.EXPECT().RefundCredits(ctx, int64(7), credit.FolderCredits).Return(nil)
		_, err := e.ctrl.CreateFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.NotContains(t, err.Error(), "refund")
	})
	t.Run("refund fails too", func(t *testing.T) {
		e := newEnv(t)
		refundErr := errors.New("refund failed")
		e.users.EXPECT().SpendCredits(ctx, int64(7), credit.FolderCredits).Return(nil)
		e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).Return(model.Folder{}, errBoom)
		e.users.EXPECT().RefundCredits(ctx, int64(7), credit.FolderCredits).Return(refundErr)
		_, err := e.ctrl.CreateFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.ErrorIs(t, err, refundErr)
	})
}

func TestListFolders(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.folders.EXPECT().ListFoldersByUser(ctx, int64(7), _now).Return([]model.Folder{
		{ID: 1000, UserID: 7, Title: "Photos", SizeBytes: 9},
		{ID: 1001, UserID: 7},
		{ID: 1002, UserID: 7},
		{ID: 1003, UserID: 7},
	}, nil)
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{{Key: _prefix + "a"}, {Key: _prefix + "b"}}, nil)
	e.storage.EXPECT().List(ctx, "u7/000000000rt/").Return([]storage.Object{{Key: "u7/000000000rt/site/report.pdf"}}, nil)
	e.storage.EXPECT().List(ctx, "u7/000000000ru/").Return(nil, nil)
	e.storage.EXPECT().List(ctx, "u7/000000000rv/").Return(nil, errBoom)
	rs, err := e.ctrl.ListFolders(ctx, &entity.ListFoldersRequest{UserID: 7})
	require.NoError(t, err)
	require.Len(t, rs.Folders, 4)
	assert.Equal(t, _host, rs.Folders[0].URL)
	assert.Equal(t, "Photos", rs.Folders[0].Title)
	assert.Equal(t, int64(9), rs.Folders[0].SizeBytes)
	assert.True(t, rs.Folders[0].ExpiresAt.IsZero(), "does not expire")
	titles := []string{}
	for _, f := range rs.Folders {
		titles = append(titles, f.DefaultTitle)
	}
	assert.Equal(t, []string{"000000000rs", "report.pdf", "000000000ru", "000000000rv"}, titles, "only file's name, else the ID")

	e.folders.EXPECT().ListFoldersByUser(ctx, int64(7), _now).Return(nil, errBoom)
	_, err = e.ctrl.ListFolders(ctx, &entity.ListFoldersRequest{UserID: 7})
	assert.ErrorIs(t, err, errBoom)
}

func TestGetFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.folder()
	mod := _now.In(time.FixedZone("x", 3600))
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{
		{Key: _prefix + "a b.txt", Size: 3, ModTime: mod},
		{Key: _prefix + "site/index.html", Size: 5, ModTime: mod},
	}, nil)
	rs, err := e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	assert.Equal(t, []entity.File{
		{Path: "a b.txt", Size: 3, UpdatedAt: _now, URL: _host + "a%20b.txt"},
		{Path: "site/index.html", Size: 5, UpdatedAt: _now, URL: _host + "site/index.html"},
	}, rs.Files)
	assert.Equal(t, int64(1000), rs.Folder.ID)
	assert.Equal(t, "000000000rs", rs.Folder.DefaultTitle)
}

func TestGetFolder_SingleFileDefaultTitle(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.folder()
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{{Key: _prefix + "notes.txt"}}, nil)
	rs, err := e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	assert.Equal(t, "notes.txt", rs.Folder.DefaultTitle)
}

func TestUpdateFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.folder()
	e.folders.EXPECT().UpdateFolderTitle(ctx, int64(1000), "Holiday 🌴").Return(nil)
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{{Key: _prefix + "beach.jpg"}}, nil)
	rs, err := e.ctrl.UpdateFolder(ctx, &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000, Title: "  Holiday 🌴 \t"})
	require.NoError(t, err)
	assert.Equal(t, "Holiday 🌴", rs.Folder.Title)
	assert.Equal(t, "beach.jpg", rs.Folder.DefaultTitle)
	assert.Equal(t, _host, rs.Folder.URL)

	e.folder()
	e.folders.EXPECT().UpdateFolderTitle(ctx, int64(1000), "").Return(nil)
	e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
	rs, err = e.ctrl.UpdateFolder(ctx, &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000, Title: "   "})
	require.NoError(t, err)
	assert.Empty(t, rs.Folder.Title, "blank clears the title")
	assert.Equal(t, "000000000rs", rs.Folder.DefaultTitle)
}

func TestUpdateFolder_Errors(t *testing.T) {
	ctx := context.Background()
	rq := func(title string) *entity.UpdateFolderRequest {
		return &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000, Title: title}
	}

	t.Run("invalid", func(t *testing.T) {
		e := newEnv(t)
		for _, title := range []string{strings.Repeat("é", MaxTitleLength+1), "a\x00b", "a\nb", "\xff"} {
			_, err := e.ctrl.UpdateFolder(ctx, rq(title))
			assert.ErrorIs(t, err, ErrInvalidTitle, title)
		}
	})
	t.Run("not owner", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		_, err := e.ctrl.UpdateFolder(ctx, &entity.UpdateFolderRequest{UserID: 8, FolderID: 1000, Title: "x"})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("update fails", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.folders.EXPECT().UpdateFolderTitle(ctx, int64(1000), "x").Return(errBoom)
		_, err := e.ctrl.UpdateFolder(ctx, rq("x"))
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("list fails", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.folders.EXPECT().UpdateFolderTitle(ctx, int64(1000), "x").Return(nil)
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, errBoom)
		_, err := e.ctrl.UpdateFolder(ctx, rq("x"))
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestValidTitle(t *testing.T) {
	assert.True(t, ValidTitle(""))
	assert.True(t, ValidTitle(strings.Repeat("ü", MaxTitleLength)))
	assert.False(t, ValidTitle(strings.Repeat("ü", MaxTitleLength+1)))
	assert.False(t, ValidTitle("tab\there"))
	assert.False(t, ValidTitle("\u0085"))
	assert.False(t, ValidTitle("\xc3"))
}

func TestGetFolder_Errors(t *testing.T) {
	ctx := context.Background()
	rq := &entity.GetFolderRequest{UserID: 7, FolderID: 1000}

	t.Run("missing", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, repository.ErrNotFound)
		_, err := e.ctrl.GetFolder(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("db error", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, errBoom)
		_, err := e.ctrl.GetFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("expired", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(0)
		_, err := e.ctrl.GetFolder(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("someone else's", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		_, err := e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 8, FolderID: 1000})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("storage error", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, errBoom)
		_, err := e.ctrl.GetFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestDeleteFolder(t *testing.T) {
	ctx := context.Background()
	rq := &entity.DeleteFolderRequest{UserID: 7, FolderID: 1000}

	t.Run("ok", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		gomock.InOrder(
			e.domains.EXPECT().DeleteDomainsByFolder(ctx, int64(1000)).Return(nil),
			e.storage.EXPECT().DeletePrefix(ctx, _prefix).Return(nil),
			e.folders.EXPECT().DeleteFolder(ctx, int64(1000)).Return(nil),
		)
		require.NoError(t, e.ctrl.DeleteFolder(ctx, rq))
	})
	t.Run("domains error keeps the folder", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.domains.EXPECT().DeleteDomainsByFolder(ctx, int64(1000)).Return(errBoom)
		assert.ErrorIs(t, e.ctrl.DeleteFolder(ctx, rq), errBoom)
	})
	t.Run("not found", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		assert.ErrorIs(t, e.ctrl.DeleteFolder(ctx, &entity.DeleteFolderRequest{UserID: 8, FolderID: 1000}), ErrNotFound)
	})
	t.Run("storage error keeps the record", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.domains.EXPECT().DeleteDomainsByFolder(ctx, int64(1000)).Return(nil)
		e.storage.EXPECT().DeletePrefix(ctx, _prefix).Return(errBoom)
		assert.ErrorIs(t, e.ctrl.DeleteFolder(ctx, rq), errBoom)
	})
	t.Run("db error", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.domains.EXPECT().DeleteDomainsByFolder(ctx, int64(1000)).Return(nil)
		e.storage.EXPECT().DeletePrefix(ctx, _prefix).Return(nil)
		e.folders.EXPECT().DeleteFolder(ctx, int64(1000)).Return(errBoom)
		assert.ErrorIs(t, e.ctrl.DeleteFolder(ctx, rq), errBoom)
	})
}

func TestPutFiles(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.limits.MaxUploadMB = 40
	e.folder()
	big := make([]byte, 20<<20)
	gomock.InOrder(
		e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{{Key: _prefix + "index.html", Size: 3}}, nil),
		// The account stores 10 MB in all, this folder's 3 bytes included;
		// replacing index.html and adding 20 MB takes it into a 2nd block.
		e.folders.EXPECT().GetUserFolderUsage(ctx, int64(7)).Return(2, int64(10<<20), nil),
		e.users.EXPECT().PayStorageBlocks(ctx, int64(7), 2, credit.StorageCredits).Return(500, nil),
		e.storage.EXPECT().Put(ctx, _prefix+"index.html", []byte("<p>hi")).Return(nil),
		e.storage.EXPECT().Put(ctx, _prefix+"css/a.bin", big).Return(nil),
		e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(5+20<<20)).Return(nil),
	)
	rs, err := e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{
		{Path: "index.html", Data: []byte("<p>hi")},
		{Path: "css/a.bin", Data: big},
	}})
	require.NoError(t, err)
	assert.Equal(t, 500, rs.CreditsSpent)
	assert.Equal(t, []entity.File{
		{Path: "index.html", Size: 5, UpdatedAt: _now, URL: _host + "index.html"},
		{Path: "css/a.bin", Size: 20 << 20, UpdatedAt: _now, URL: _host + "css/a.bin"},
	}, rs.Files)
}

// An upload to a path already in the folder is an update: the file is
// replaced and keeps its URL.
func TestPutFiles_ReplacesAFile(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	local, err := storage.NewLocal(t.TempDir())
	require.NoError(t, err)
	ctrl := New(Params{Folders: e.folders, Users: e.users, Storage: local, Limits: func() entity.FileSettings { return e.limits }, Now: func() time.Time { return _now }})
	e.folders.EXPECT().FindFolderByID(gomock.Any(), int64(1000)).Return(model.Folder{ID: 1000, UserID: 7}, nil).Times(4)
	e.folders.EXPECT().GetUserFolderUsage(ctx, int64(7)).Return(1, int64(0), nil).Times(2)
	e.users.EXPECT().PayStorageBlocks(ctx, int64(7), 1, credit.StorageCredits).Return(0, nil).Times(2)
	e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(2)).Return(nil)
	e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(9)).Return(nil)

	put := func(data string) entity.File {
		rs, err := ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "site/index.html", Data: []byte(data)}}})
		require.NoError(t, err)
		return rs.Files[0]
	}
	first, second := put("v1"), put("version 2")
	assert.Equal(t, first.URL, second.URL)
	assert.Equal(t, int64(9), second.Size)

	rs, err := ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	require.Len(t, rs.Files, 1)
	assert.Equal(t, "site/index.html", rs.Files[0].Path)
	file, err := ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "site/index.html"})
	require.NoError(t, err)
	defer func() { _ = file.Body.Close() }()
	body, err := io.ReadAll(file.Body)
	require.NoError(t, err)
	assert.Equal(t, "version 2", string(body))
}

func TestPutFiles_Validation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	put := func(files ...entity.FileUpload) error {
		_, err := e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: files})
		return err
	}
	assert.ErrorIs(t, put(), ErrNoFiles)
	assert.ErrorIs(t, put(entity.FileUpload{Path: "../x"}), ErrInvalidPath)
	assert.ErrorIs(t, put(entity.FileUpload{Path: "a"}, entity.FileUpload{Path: "a"}), ErrDuplicatePath)
	assert.ErrorIs(t, put(entity.FileUpload{Path: "a", Data: make([]byte, 1<<20)}, entity.FileUpload{Path: "b", Data: []byte("x")}), ErrTooLarge)
}

func TestPutFiles_Credits(t *testing.T) {
	ctx := context.Background()
	rq := &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "a"}}}

	t.Run("not found", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		_, err := e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 8, FolderID: 1000, Files: rq.Files})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("list error", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, errBoom)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("usage error", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
		e.folders.EXPECT().GetUserFolderUsage(ctx, int64(7)).Return(0, int64(0), errBoom)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("insufficient", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
		e.folders.EXPECT().GetUserFolderUsage(ctx, int64(7)).Return(1, int64(25<<20), nil)
		e.users.EXPECT().PayStorageBlocks(ctx, int64(7), 1, credit.StorageCredits).Return(0, repository.ErrInsufficientCredits)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, ErrInsufficientCredits)
		assert.Contains(t, err.Error(), "500 credits a month per started 25 MB")
	})
	t.Run("pay error", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
		e.folders.EXPECT().GetUserFolderUsage(ctx, int64(7)).Return(1, int64(0), nil)
		e.users.EXPECT().PayStorageBlocks(ctx, int64(7), 0, credit.StorageCredits).Return(0, errBoom)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("size not recorded", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
		e.folders.EXPECT().GetUserFolderUsage(ctx, int64(7)).Return(1, int64(0), nil)
		e.users.EXPECT().PayStorageBlocks(ctx, int64(7), 0, credit.StorageCredits).Return(0, nil)
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil)
		e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(0)).Return(errBoom)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestPutFiles_WriteFailureRefunds(t *testing.T) {
	ctx := context.Background()
	rq := &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{
		{Path: "a", Data: []byte("aa")}, {Path: "b", Data: []byte("b")},
	}}
	expectPaid := func(e *env, spent int) {
		e.folder()
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
		e.folders.EXPECT().GetUserFolderUsage(ctx, int64(7)).Return(1, int64(25<<20), nil)
		e.users.EXPECT().PayStorageBlocks(ctx, int64(7), 2, credit.StorageCredits).Return(spent, nil)
	}

	t.Run("refunded", func(t *testing.T) {
		e := newEnv(t)
		expectPaid(e, 500)
		gomock.InOrder(
			e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil),
			e.storage.EXPECT().Put(ctx, _prefix+"b", gomock.Any()).Return(errBoom),
			e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(2)).Return(nil),
			e.users.EXPECT().RefundStorageBlocks(ctx, int64(7), 1, 500).Return(nil),
		)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.Contains(t, err.Error(), "write b")
	})
	t.Run("nothing to refund", func(t *testing.T) {
		e := newEnv(t)
		expectPaid(e, 0)
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(errBoom)
		e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(0)).Return(errBoom)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.NotContains(t, err.Error(), "refund")
	})
	t.Run("refund fails too", func(t *testing.T) {
		e := newEnv(t)
		expectPaid(e, 1000)
		refundErr := errors.New("refund failed")
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(errBoom)
		e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(0)).Return(nil)
		e.users.EXPECT().RefundStorageBlocks(ctx, int64(7), 2, 1000).Return(refundErr)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.ErrorIs(t, err, refundErr)
	})
}

func TestDeleteFile(t *testing.T) {
	ctx := context.Background()
	rq := &entity.DeleteFileRequest{UserID: 7, FolderID: 1000, Path: "site/a.txt"}

	e := newEnv(t)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, &entity.DeleteFileRequest{UserID: 7, FolderID: 1000, Path: "/a"}), ErrInvalidPath)

	e.folder()
	gomock.InOrder(
		e.storage.EXPECT().Delete(ctx, _prefix+"site/a.txt").Return(nil),
		e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{{Key: _prefix + "b", Size: 4}, {Key: _prefix + "c", Size: 5}}, nil),
		e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(9)).Return(nil),
	)
	require.NoError(t, e.ctrl.DeleteFile(ctx, rq))

	e.folder()
	e.storage.EXPECT().Delete(ctx, _prefix+"site/a.txt").Return(errBoom)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, rq), errBoom)

	e.folder()
	e.storage.EXPECT().Delete(ctx, _prefix+"site/a.txt").Return(nil)
	e.storage.EXPECT().List(ctx, _prefix).Return(nil, errBoom)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, rq), errBoom)

	e.folder()
	e.storage.EXPECT().Delete(ctx, _prefix+"site/a.txt").Return(nil)
	e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
	e.folders.EXPECT().UpdateFolderSize(ctx, int64(1000), int64(0)).Return(errBoom)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, rq), errBoom)

	e.liveFolder(0)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, rq), ErrNotFound)
}

func TestOpenPublicFile(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct{ path, key, contentType string }{
		"file":         {"site/app.js", "site/app.js", "text/javascript; charset=utf-8"},
		"root index":   {"", "index.html", "text/html; charset=utf-8"},
		"dir index":    {"site/", "site/index.html", "text/html; charset=utf-8"},
		"unknown ext":  {"blob.zzz", "blob.zzz", "application/octet-stream"},
		"no extension": {"LICENSE", "LICENSE", "application/octet-stream"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.folder()
			e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{ID: 7}, nil)
			body := io.NopCloser(strings.NewReader("x"))
			e.storage.EXPECT().Get(ctx, _prefix+tc.key).Return(body, storage.Object{Size: 1, ModTime: _now}, nil)
			rs, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: tc.path})
			require.NoError(t, err)
			assert.Equal(t, &entity.OpenFileResponse{Body: body, Size: 1, ContentType: tc.contentType, ModTime: _now}, rs)
		})
	}
	t.Run("anonymous folder: no owner to look up", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Minute)
		body := io.NopCloser(strings.NewReader("x"))
		e.storage.EXPECT().Get(ctx, _prefix+"index.html").Return(body, storage.Object{Size: 1, ModTime: _now}, nil)
		rs, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000})
		require.NoError(t, err)
		assert.Equal(t, body, rs.Body)
	})
}

func TestOpenPublicFile_Errors(t *testing.T) {
	ctx := context.Background()
	rq := &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: "a.txt"}

	e := newEnv(t)
	_, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: "../a"})
	assert.ErrorIs(t, err, ErrNotFound)

	e.folder()
	_, err = e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{UserID: 8, FolderID: 1000, Path: "a.txt"})
	assert.ErrorIs(t, err, ErrNotFound, "the path names another owner")

	e.liveFolder(0)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, ErrNotFound, "expired")

	e.folder()
	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, repository.ErrNotFound)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, ErrNotFound, "owner gone")

	e.folder()
	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, errBoom)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, errBoom)

	e.folder()
	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{ID: 7, Credits: -1}, nil)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, ErrSuspended, "the owner's balance is below zero")

	e.folder()
	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{ID: 7}, nil)
	e.storage.EXPECT().Get(ctx, _prefix+"a.txt").Return(nil, storage.Object{}, storage.ErrNotFound)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, ErrNotFound)

	e.folder()
	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{ID: 7}, nil)
	e.storage.EXPECT().Get(ctx, _prefix+"a.txt").Return(nil, storage.Object{}, errBoom)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, errBoom)
}

func TestOpenFile(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, err := e.ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "dir/"})
	assert.ErrorIs(t, err, ErrInvalidPath, "no index fallback for downloads")

	e.folder()
	_, err = e.ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 8, FolderID: 1000, Path: "a.txt"})
	assert.ErrorIs(t, err, ErrNotFound)

	e.folder()
	body := io.NopCloser(strings.NewReader("x"))
	e.storage.EXPECT().Get(ctx, _prefix+"dir/a.txt").Return(body, storage.Object{Size: 1, ModTime: _now}, nil)
	rs, err := e.ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "dir/a.txt"})
	require.NoError(t, err)
	assert.Equal(t, &entity.OpenFileResponse{Body: body, Size: 1, ContentType: "text/plain; charset=utf-8", ModTime: _now}, rs)
}

func TestFolderHost(t *testing.T) {
	assert.Equal(t, "f000000000rs", FolderHost(1000))
}

func TestDeleteExpiredFolders(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.domains.EXPECT().DeleteDomainsByFolder(ctx, gomock.Any()).Return(nil).Times(3)
	e.folders.EXPECT().ListExpiredFolders(ctx, _now, _sweepBatch).Return([]model.Folder{
		{ID: 1, UserID: 7}, {ID: 2, UserID: 8}, {ID: 3, UserID: 9},
	}, nil)
	e.storage.EXPECT().DeletePrefix(ctx, "u7/00000000001/").Return(nil)
	e.folders.EXPECT().DeleteFolder(ctx, int64(1)).Return(nil)
	e.storage.EXPECT().DeletePrefix(ctx, "u8/00000000002/").Return(errBoom)
	e.storage.EXPECT().DeletePrefix(ctx, "u9/00000000003/").Return(nil)
	e.folders.EXPECT().DeleteFolder(ctx, int64(3)).Return(nil)

	rs, err := e.ctrl.DeleteExpiredFolders(ctx)
	assert.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "folder 2")
	assert.Equal(t, 2, rs.Deleted, "a failure does not stop the batch")

	e.folders.EXPECT().ListExpiredFolders(ctx, _now, _sweepBatch).Return(nil, errBoom)
	rs, err = e.ctrl.DeleteExpiredFolders(ctx)
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, 0, rs.Deleted)

	e.folders.EXPECT().ListExpiredFolders(ctx, _now, _sweepBatch).Return(nil, nil)
	rs, err = e.ctrl.DeleteExpiredFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, rs.Deleted)
}

func TestValidPath(t *testing.T) {
	for _, p := range []string{"a", "index.html", "site/css/a b.css", ".well-known/x", "ünïcode/文件.txt", strings.Repeat("a", 255), "1/2/3/4/5/f"} {
		assert.True(t, ValidPath(p), p)
	}
	for _, p := range []string{
		"", "/a", "a/", "a//b", "./a", "a/../b", "..", "a\\b", "a\x00b", "a\nb", "a\x7fb",
		"\xff", strings.Repeat("a", 256), strings.Repeat("a/", 256) + "a", ".upload-1", "x/.upload-tmp", "1/2/3/4/5/6/f",
	} {
		assert.False(t, ValidPath(p), p)
	}
}
