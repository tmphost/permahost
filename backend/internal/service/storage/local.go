// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type local struct {
	root string
}

// NewLocal returns a Service that keeps objects as files under root.
func NewLocal(root string) (Service, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &local{root: root}, nil
}

func (s *local) Put(_ context.Context, key string, data []byte) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	path := s.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Write then rename, so a reader never sees a half-written file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

func (s *local) Get(_ context.Context, key string) (io.ReadCloser, Object, error) {
	if !validKey(key) {
		return nil, Object{}, ErrInvalidKey
	}
	f, err := os.Open(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Object{}, ErrNotFound
	}
	if err != nil {
		return nil, Object{}, err
	}
	// Stat on an open file only fails if the descriptor is broken.
	info, _ := f.Stat()
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, Object{}, ErrNotFound
	}
	return f, Object{Key: key, Size: info.Size(), ModTime: info.ModTime()}, nil
}

func (s *local) List(_ context.Context, prefix string) ([]Object, error) {
	if !validPrefix(prefix) {
		return nil, ErrInvalidKey
	}
	var out []Object
	err := filepath.Walk(s.path(prefix), func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip directories and the temp files of uploads in flight.
		if !info.Mode().IsRegular() || strings.HasPrefix(info.Name(), ".upload-") {
			return nil
		}
		// Walk only yields paths under root.
		rel, _ := filepath.Rel(s.root, path)
		out = append(out, Object{Key: filepath.ToSlash(rel), Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (s *local) Delete(_ context.Context, key string) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	err := os.Remove(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (s *local) DeletePrefix(_ context.Context, prefix string) error {
	if !validPrefix(prefix) {
		return ErrInvalidKey
	}
	return os.RemoveAll(s.path(prefix))
}

func (s *local) path(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(key))
}
