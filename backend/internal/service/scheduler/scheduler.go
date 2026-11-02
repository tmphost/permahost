// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package scheduler runs recurring jobs on cron schedules, in UTC.
package scheduler

//go:generate mockgen -source=scheduler.go -destination=mocks/scheduler.go -package=mocks

import (
	"time"

	"github.com/robfig/cron/v3"
)

type (
	// Service schedules jobs.
	Service interface {
		// AddFunc runs fn on a standard 5-field cron spec, evaluated in UTC.
		AddFunc(spec string, fn func()) error
		// Start begins running jobs in the background.
		Start()
		// Stop halts scheduling and waits for running jobs to finish.
		Stop()
	}

	service struct {
		cron *cron.Cron
	}
)

// New returns a scheduler. A job that is still running when its next run
// comes due is skipped, so slow jobs never overlap.
func New() Service {
	return &service{cron: cron.New(
		cron.WithLocation(time.UTC),
		cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)),
	)}
}

func (s *service) AddFunc(spec string, fn func()) error {
	_, err := s.cron.AddFunc(spec, fn)
	return err
}

func (s *service) Start() { s.cron.Start() }

func (s *service) Stop() { <-s.cron.Stop().Done() }
