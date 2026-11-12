// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server/db"
)

// Worker derives users whose raw data changed (LISTEN/NOTIFY plus a periodic sweep as safety net).
type Worker struct {
	D        *Deriver
	DB       *db.DB
	Sweep    time.Duration // default 60 s
	Debounce time.Duration // default 250 ms
	Workers  int           // default 2
	// OnReindexed (optional) is called after each finished Reindex with the raw rows re-derived.
	OnReindexed func(userID string, rows int)

	readyOnce sync.Once
	readyCh   chan struct{}
	initOnce  sync.Once
}

func (w *Worker) init() {
	w.initOnce.Do(func() { w.readyCh = make(chan struct{}) })
}

// Ready is closed once LISTEN is active and the first sweep and the startup version check finished.
func (w *Worker) Ready() <-chan struct{} {
	w.init()
	return w.readyCh
}

// Run blocks until ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	w.init()
	sweep, debounce, workers := w.Sweep, w.Debounce, w.Workers
	if sweep <= 0 {
		sweep = 60 * time.Second
	}
	if debounce <= 0 {
		debounce = 250 * time.Millisecond
	}
	if workers <= 0 {
		workers = 2
	}
	var mu sync.Mutex
	dirty := map[string]time.Time{} // user -> earliest start
	running := map[string]bool{}
	wake := make(chan struct{}, 1)
	mark := func(u string) {
		mu.Lock()
		if _, ok := dirty[u]; !ok {
			dirty[u] = time.Now().Add(debounce)
		}
		mu.Unlock()
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	doSweep := func() {
		var ids []string
		err := w.DB.WithSystem(ctx, func(tx pgx.Tx) error {
			r, err := tx.Query(ctx, `SELECT derive_pending_users()::text`)
			if err != nil {
				return err
			}
			defer r.Close()
			for r.Next() {
				var id string
				if err := r.Scan(&id); err != nil {
					return err
				}
				ids = append(ids, id)
			}
			return r.Err()
		})
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("derive sweep failed", "err", err)
			}
			return
		}
		for _, id := range ids {
			mark(id)
		}
	}

	var wg sync.WaitGroup
	listening := make(chan struct{})
	var listenOnce sync.Once
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.DB.Listen(ctx, "braid_ingest", func() { listenOnce.Do(func() { close(listening) }) }, mark)
	}()
	versionDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(versionDone)
		w.versionCheck(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-listening:
		case <-ctx.Done():
			return
		}
		doSweep() // after LISTEN is active, so nothing falls between sweep and listen
		select {
		case <-versionDone:
		case <-ctx.Done():
			return
		}
		w.readyOnce.Do(func() { close(w.readyCh) })
	}()

	sem := make(chan struct{}, workers)
	tick := time.NewTicker(sweep)
	defer tick.Stop()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
loop:
	for {
		// start every due user that is not running and fits in the pool
		mu.Lock()
		next := time.Time{}
		for u, at := range dirty {
			if running[u] {
				continue
			}
			if now := time.Now(); now.Before(at) {
				if next.IsZero() || at.Before(next) {
					next = at
				}
				continue
			}
			select {
			case sem <- struct{}{}:
			default:
				continue
			}
			delete(dirty, u)
			running[u] = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				w.deriveOne(ctx, u)
				mu.Lock()
				delete(running, u)
				mu.Unlock()
				<-sem
				select {
				case wake <- struct{}{}:
				default:
				}
			}()
		}
		mu.Unlock()
		if next.IsZero() {
			next = time.Now().Add(time.Hour)
		}
		timer.Reset(time.Until(next))
		select {
		case <-ctx.Done():
			break loop
		case <-tick.C:
			doSweep()
		case <-wake:
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	wg.Wait()
	return nil
}

func (w *Worker) deriveOne(ctx context.Context, userID string) {
	rows, err := w.D.DeriveUser(ctx, userID)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("derive failed", "user", userID, "err", err) // retried at the next sweep
		}
		return
	}
	if rows > 0 {
		w.announce(ctx, userID)
	}
}

func (w *Worker) announce(ctx context.Context, userID string) {
	err := w.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT pg_notify('braid_derived', $1)`, userID)
		return err
	})
	if err != nil && ctx.Err() == nil {
		slog.Error("derive notify failed", "user", userID, "err", err)
	}
}

func (w *Worker) allUsers(ctx context.Context) (ids []string, err error) {
	err = w.DB.WithSystem(ctx, func(tx pgx.Tx) error {
		r, err := tx.Query(ctx, `SELECT derive_all_users()::text`)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var id string
			if err := r.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return r.Err()
	})
	return
}

// versionCheck rebuilds users whose derive_state carries another parser version.
func (w *Worker) versionCheck(ctx context.Context) {
	ids, err := w.allUsers(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("derive version check failed", "err", err)
		}
		return
	}
	for _, id := range ids {
		var ver *int
		var rebuilding bool
		err := w.DB.WithUser(ctx, id, func(tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `SELECT parser_version, rebuilding FROM derive_state WHERE user_id = $1`, id).Scan(&ver, &rebuilding)
			if errors.Is(err, pgx.ErrNoRows) {
				_, err = tx.Exec(ctx, `INSERT INTO derive_state(user_id, parser_version) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, ParserVersion)
			}
			return err
		})
		if err == nil && ver != nil && (*ver != ParserVersion || rebuilding) { // rebuilding: a rebuild was interrupted
			err = w.Reindex(ctx, id)
		}
		if err != nil && ctx.Err() == nil {
			slog.Error("derive version check failed", "user", id, "err", err)
		}
	}
}

// Reindex discards and re-derives everything of one user from the raw data.
func (w *Worker) Reindex(ctx context.Context, userID string) error {
	err := w.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO derive_state(user_id, parser_version, rebuilding) VALUES ($1, $2, true)
		  ON CONFLICT (user_id) DO UPDATE SET parser_version = $2, rebuilding = true, updated_at = now()`, userID, ParserVersion)
		return err
	})
	if err != nil {
		return err
	}
	if err := w.D.Reset(ctx, userID); err != nil {
		return err
	}
	total := 0
	for {
		n, err := w.D.DeriveUser(ctx, userID)
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		total += n
	}
	err = w.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE derive_state SET rebuilding = false, updated_at = now() WHERE user_id = $1`, userID)
		return err
	})
	if err != nil {
		return err
	}
	w.announce(ctx, userID)
	if w.OnReindexed != nil {
		w.OnReindexed(userID, total)
	}
	return nil
}

// ReindexAll reindexes all users, one after the other.
func (w *Worker) ReindexAll(ctx context.Context) error {
	ids, err := w.allUsers(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := w.Reindex(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
