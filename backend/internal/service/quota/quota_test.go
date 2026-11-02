// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package quota

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newQuota() (*service, *clock) {
	c := &clock{t: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}
	return New(Params{Limit: 5, Window: 30 * time.Minute, Now: c.now}).(*service), c
}

func TestNew_DefaultClock(t *testing.T) {
	s := New(Params{Limit: 1, Window: time.Minute}).(*service)
	assert.WithinDuration(t, time.Now(), s.now(), time.Second)
}

func TestTake(t *testing.T) {
	s, c := newQuota()
	assert.True(t, s.Take("a", 3))
	assert.False(t, s.Take("a", 3), "only 2 left")
	assert.True(t, s.Take("a", 2))
	assert.False(t, s.Take("a", 1))
	assert.True(t, s.Take("b", 5), "keys have their own budget")
	assert.False(t, s.Take("c", 6), "never more than the limit")
	assert.False(t, s.Take("c", -1))

	c.t = c.t.Add(29 * time.Minute)
	assert.False(t, s.Take("a", 1), "same window")
	c.t = c.t.Add(time.Minute)
	assert.True(t, s.Take("a", 5), "a new window starts over")
}

func TestGive(t *testing.T) {
	s, c := newQuota()
	s.Give("none", 1)
	assert.True(t, s.Take("a", 5))
	s.Give("a", 2)
	assert.True(t, s.Take("a", 2))
	assert.False(t, s.Take("a", 1))
	s.Give("a", 10)
	assert.True(t, s.Take("a", 5), "never below zero")

	c.t = c.t.Add(30 * time.Minute)
	s.Give("a", 5)
	assert.True(t, s.Take("a", 5))
	assert.False(t, s.Take("a", 1), "a give after the window ends is dropped")
}

func TestSweep(t *testing.T) {
	s, c := newQuota()
	assert.True(t, s.Take("old", 1))
	c.t = c.t.Add(10 * time.Minute)
	assert.True(t, s.Take("recent", 1))
	assert.Len(t, s.buckets, 2)

	c.t = c.t.Add(25 * time.Minute)
	assert.True(t, s.Take("new", 1))
	assert.Len(t, s.buckets, 2, "the past window's bucket is gone")
	assert.NotContains(t, s.buckets, "old")

	c.t = c.t.Add(time.Minute)
	assert.True(t, s.Take("new2", 1))
	assert.Contains(t, s.buckets, "recent", "sweeps at most once a window")
}
