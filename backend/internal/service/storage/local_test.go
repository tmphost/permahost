// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLocal(t *testing.T) (*local, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "files")
	s, err := NewLocal(root)
	require.NoError(t, err)
	return s.(*local), root
}

func TestNewLocal_RootIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err := NewLocal(filepath.Join(file, "sub"))
	assert.Error(t, err)
}

func TestLocal_PutGetListDelete(t *testing.T) {
	ctx := context.Background()
	s, _ := newLocal(t)

	require.NoError(t, s.Put(ctx, "u1/f1/site/index.html", []byte("<h1>hi</h1>")))
	require.NoError(t, s.Put(ctx, "u1/f1/a.txt", []byte("a")))
	require.NoError(t, s.Put(ctx, "u1/f1/a.txt", []byte("replaced")))
	require.NoError(t, s.Put(ctx, "u1/f2/b.txt", []byte("b")))

	rc, obj, err := s.Get(ctx, "u1/f1/a.txt")
	require.NoError(t, err)
	body, _ := io.ReadAll(rc)
	require.NoError(t, rc.Close())
	assert.Equal(t, "replaced", string(body))
	assert.Equal(t, int64(8), obj.Size)
	assert.Equal(t, "u1/f1/a.txt", obj.Key)
	assert.False(t, obj.ModTime.IsZero())

	objs, err := s.List(ctx, "u1/f1/")
	require.NoError(t, err)
	require.Len(t, objs, 2)
	assert.Equal(t, "u1/f1/a.txt", objs[0].Key)
	assert.Equal(t, "u1/f1/site/index.html", objs[1].Key)

	require.NoError(t, s.Delete(ctx, "u1/f1/a.txt"))
	require.NoError(t, s.Delete(ctx, "u1/f1/a.txt"), "deleting a missing object is fine")
	_, _, err = s.Get(ctx, "u1/f1/a.txt")
	assert.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, s.DeletePrefix(ctx, "u1/f1/"))
	objs, err = s.List(ctx, "u1/f1/")
	require.NoError(t, err)
	assert.Empty(t, objs)
	objs, err = s.List(ctx, "u1/f2/")
	require.NoError(t, err)
	assert.Len(t, objs, 1, "other folders are untouched")
}

func TestLocal_InvalidKeys(t *testing.T) {
	ctx := context.Background()
	s, _ := newLocal(t)
	assert.ErrorIs(t, s.Put(ctx, "../x", nil), ErrInvalidKey)
	_, _, err := s.Get(ctx, "a//b")
	assert.ErrorIs(t, err, ErrInvalidKey)
	_, err = s.List(ctx, "u1")
	assert.ErrorIs(t, err, ErrInvalidKey)
	assert.ErrorIs(t, s.Delete(ctx, "/abs"), ErrInvalidKey)
	assert.ErrorIs(t, s.DeletePrefix(ctx, "u1/../"), ErrInvalidKey)
}

func TestLocal_GetDirectoryIsNotFound(t *testing.T) {
	ctx := context.Background()
	s, _ := newLocal(t)
	require.NoError(t, s.Put(ctx, "u1/f1/dir/x", []byte("x")))
	_, _, err := s.Get(ctx, "u1/f1/dir")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestLocal_GetOpenError(t *testing.T) {
	ctx := context.Background()
	s, _ := newLocal(t)
	require.NoError(t, s.Put(ctx, "u1/file", []byte("x")))
	// A file used as a directory fails with ENOTDIR, not "not found".
	_, _, err := s.Get(ctx, "u1/file/child")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
}

func TestLocal_PutErrors(t *testing.T) {
	ctx := context.Background()
	s, root := newLocal(t)

	require.NoError(t, s.Put(ctx, "u1/file", []byte("x")))
	assert.Error(t, s.Put(ctx, "u1/file/child", nil), "parent is a file")

	// The target is a non-empty directory, so the rename fails and the temp
	// file is cleaned up.
	require.NoError(t, s.Put(ctx, "u1/dir/x", []byte("x")))
	assert.Error(t, s.Put(ctx, "u1/dir", []byte("y")))
	entries, _ := os.ReadDir(filepath.Join(root, "u1"))
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".upload-")
	}

	ro := filepath.Join(root, "ro")
	require.NoError(t, os.MkdirAll(ro, 0o500))
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	assert.Error(t, s.Put(ctx, "ro/x", nil), "temp file cannot be created")
}

func TestLocal_ListSkipsTempFilesAndFailsOnUnreadableDirs(t *testing.T) {
	ctx := context.Background()
	s, root := newLocal(t)
	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("a")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "u1", "f1", ".upload-123"), nil, 0o600))
	objs, err := s.List(ctx, "u1/f1/")
	require.NoError(t, err)
	assert.Len(t, objs, 1)

	locked := filepath.Join(root, "u1", "f1", "locked")
	require.NoError(t, os.MkdirAll(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	_, err = s.List(ctx, "u1/f1/")
	assert.Error(t, err)
}

func TestLocal_DeleteError(t *testing.T) {
	ctx := context.Background()
	s, _ := newLocal(t)
	require.NoError(t, s.Put(ctx, "u1/dir/x", []byte("x")))
	assert.Error(t, s.Delete(ctx, "u1/dir"), "a non-empty directory is not removed")
}
