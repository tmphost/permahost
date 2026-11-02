// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package quota counts operations per key in fixed time windows, in
// memory. Counts are per process and start over on restart.
package quota

//go:generate mockgen -source=quota.go -destination=mocks/quota.go -package=mocks

import (
	"sync"
	"time"
)

type (
	// Service hands out a fixed number of operations per key per window.
	Service interface {
		// Take spends n operations of key's budget for the current window.
		// It spends nothing and returns false when fewer than n are left.
		Take(key string, n int) bool
		// Give returns n operations taken in the current window, e.g. for
		// work that failed.
		Give(key string, n int)
	}

	// Params configure a quota.
	Params struct {
		// Limit is how many operations a key may spend per window.
		Limit  int
		Window time.Duration
		Now    func() time.Time
	}

	service struct {
		mu        sync.Mutex
		limit     int
		window    time.Duration
		now       func() time.Time
		buckets   map[string]*bucket
		lastSweep time.Time
	}

	bucket struct {
		start time.Time
		used  int
	}
)

// New returns an in-memory quota.
func New(p Params) Service {
	s := &service{limit: p.Limit, window: p.Window, now: p.Now, buckets: map[string]*bucket{}}
	if s.now == nil {
		s.now = time.Now
	}
	s.lastSweep = s.now()
	return s
}

func (s *service) Take(key string, n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweep(now)
	b, ok := s.buckets[key]
	if !ok || !now.Before(b.start.Add(s.window)) {
		b = &bucket{start: now}
		s.buckets[key] = b
	}
	if n < 0 || b.used+n > s.limit {
		return false
	}
	b.used += n
	return true
}

func (s *service) Give(key string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A bucket from an earlier window was already reset or swept.
	if b, ok := s.buckets[key]; ok && s.now().Before(b.start.Add(s.window)) {
		b.used = max(b.used-n, 0)
	}
}

// sweep drops the buckets of past windows, at most once a window, so
// memory follows the keys active lately.
func (s *service) sweep(now time.Time) {
	if now.Before(s.lastSweep.Add(s.window)) {
		return
	}
	s.lastSweep = now
	for key, b := range s.buckets {
		if !now.Before(b.start.Add(s.window)) {
			delete(s.buckets, key)
		}
	}
}
