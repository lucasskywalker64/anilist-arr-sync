// Package scheduler runs recurring synchronization, backup, and update checks.
package scheduler

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/backup"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/orchestrator"
)

// TaskStatus reports the last execution start and next scheduled execution.
// Zero timestamps indicate a task has not run or is not scheduled.
type TaskStatus struct {
	Last time.Time `json:"last"`
	Next time.Time `json:"next"`
}

// Status is a thread-safe snapshot of all scheduled tasks.
type Status struct {
	Sync   TaskStatus `json:"sync"`
	Backup TaskStatus `json:"backup"`
	Update TaskStatus `json:"update"`
}

type task struct {
	name     string
	run      func(context.Context) error
	interval time.Duration
	ticker   *time.Ticker
	running  bool
	initial  bool
	status   TaskStatus
}

// Scheduler owns task tickers and cancels active work when stopped.
type Scheduler struct {
	mu     sync.Mutex
	tasks  [3]task
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
}

// New constructs an idle scheduler. cfg must have passed config.Validate.
// A nil dependency disables its task. Update checking is supplied by the updater.
func New(cfg config.Config, orch *orchestrator.Orchestrator, backups *backup.Engine, checkUpdates func(context.Context) error) *Scheduler {
	s := &Scheduler{wake: make(chan struct{}, 1)}
	if orch != nil {
		s.tasks[0] = task{
			name: "sync", interval: time.Duration(cfg.SyncIntervalHours) * time.Hour,
			run: func(ctx context.Context) error {
				result, err := orch.StartSync(ctx, orchestrator.TriggerScheduled)
				if err != nil {
					return err
				}
				s.mu.Lock()
				s.tasks[0].status.Last = time.Now()
				s.mu.Unlock()
				return (<-result).Err
			},
		}
	}
	if backups != nil {
		s.tasks[1] = task{
			name: "backup", interval: time.Duration(cfg.BackupInterval) * 24 * time.Hour,
			run: func(ctx context.Context) error {
				_, err := backups.Create(ctx, backup.TypeScheduled)
				return err
			},
		}
	}
	s.tasks[2] = task{name: "update check", run: checkUpdates}
	if cfg.UpdateAutomatically {
		s.tasks[2].interval = 24 * time.Hour
	}
	return s
}

// UpdateConfig resets only tasks whose intervals changed. cfg must be validated.
// Changed backup intervals take effect from now, in units of 24 hours.
func (s *Scheduler) UpdateConfig(cfg config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	intervals := [3]time.Duration{
		time.Duration(cfg.SyncIntervalHours) * time.Hour,
		time.Duration(cfg.BackupInterval) * 24 * time.Hour,
	}
	if cfg.UpdateAutomatically {
		intervals[2] = 24 * time.Hour
	}
	for i, interval := range intervals {
		t := &s.tasks[i]
		if t.interval == interval || t.run == nil {
			continue
		}
		t.interval = interval
		if s.cancel != nil && s.ctx.Err() == nil {
			if interval == 0 {
				t.ticker.Stop()
				t.ticker = nil
				t.status.Next = time.Time{}
				continue
			}
			t.status.Next = time.Now().Add(interval)
			t.initial = false
			if t.ticker != nil {
				t.ticker.Stop()
			}
			t.ticker = time.NewTicker(interval)
		}
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Start begins background scheduling and returns immediately. Repeated calls
// while running do nothing. Cancellation stops scheduling and active tasks.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil || ctx.Err() != nil {
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.ctx = ctx
	s.done = make(chan struct{})
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.run != nil && t.interval > 0 {
			now := time.Now()
			delay := t.interval
			t.status.Next = now.Add(t.interval)
			t.initial = i == 1
			if t.initial {
				local := now.Local()
				t.status.Next = time.Date(local.Year(), local.Month(), local.Day(), 3, 0, 0, 0, local.Location())
				if !t.status.Next.After(now) {
					t.status.Next = t.status.Next.AddDate(0, 0, 1)
				}
				delay = time.Until(t.status.Next)
			}
			t.ticker = time.NewTicker(delay)
		}
	}
	go s.loop(ctx)
}

// Stop cancels scheduling and waits for active tasks to release their resources.
// Task callbacks must honor context cancellation.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// Status returns a consistent snapshot without exposing mutable scheduler state.
func (s *Scheduler) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Sync: s.tasks[0].status, Backup: s.tasks[1].status, Update: s.tasks[2].status}
}

func (s *Scheduler) loop(ctx context.Context) {
	var workers sync.WaitGroup
	defer func() {
		s.mu.Lock()
		for i := range s.tasks {
			t := &s.tasks[i]
			if t.ticker != nil {
				t.ticker.Stop()
				t.ticker = nil
			}
			t.status.Next = time.Time{}
		}
		s.mu.Unlock()
		workers.Wait()
		s.mu.Lock()
		close(s.done)
		s.cancel = nil
		s.mu.Unlock()
	}()
	for {
		s.mu.Lock()
		var ticks [3]<-chan time.Time
		var tickers [3]*time.Ticker
		for i := range s.tasks {
			tickers[i] = s.tasks[i].ticker
			if tickers[i] != nil {
				ticks[i] = tickers[i].C
			}
		}
		s.mu.Unlock()
		var index int
		var tick time.Time
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			continue
		case tick = <-ticks[0]:
			index = 0
		case tick = <-ticks[1]:
			index = 1
		case tick = <-ticks[2]:
			index = 2
		}
		s.mu.Lock()
		t := &s.tasks[index]
		// A configuration update may have replaced the ticker after select received.
		if ctx.Err() != nil || t.ticker == nil || t.ticker != tickers[index] {
			s.mu.Unlock()
			continue
		}
		t.status.Next = tick.Add(t.interval)
		if t.initial {
			t.status.Next = time.Now().Add(t.interval)
			t.ticker.Reset(t.interval)
			t.initial = false
		}
		if t.running {
			log.Printf("scheduler: skipping %s tick: task is already in progress", t.name)
			s.mu.Unlock()
			continue
		}
		t.running = true
		workers.Add(1)
		s.mu.Unlock()
		go func() {
			defer workers.Done()
			if index != 0 {
				s.mu.Lock()
				t.status.Last = time.Now()
				s.mu.Unlock()
			}
			err := t.run(ctx)
			if errors.Is(err, orchestrator.ErrSyncInProgress) {
				log.Printf("scheduler: skipping sync tick: %v", err)
			} else if err != nil && ctx.Err() == nil {
				log.Printf("scheduler: %s failed: %v", t.name, err)
			}
			s.mu.Lock()
			t.running = false
			s.mu.Unlock()
		}()
	}
}
