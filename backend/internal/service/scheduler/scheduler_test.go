// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_RunsInUTC(t *testing.T) {
	s := New().(*service)
	assert.Equal(t, time.UTC, s.cron.Location())

	ran := make(chan struct{}, 1)
	require.NoError(t, s.AddFunc("@every 10ms", func() {
		select {
		case ran <- struct{}{}:
		default:
		}
	}))
	s.Start()
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("job did not run")
	}
	s.Stop()
}

func TestScheduler_DailyMidnightSpec(t *testing.T) {
	s := New().(*service)
	require.NoError(t, s.AddFunc("0 0 * * *", func() {}))
	entry := s.cron.Entries()[0]
	from := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), entry.Schedule.Next(from))
}

func TestScheduler_InvalidSpec(t *testing.T) {
	assert.Error(t, New().AddFunc("not a spec", func() {}))
}
