package updater

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type fixtureService struct {
	control        *fixtureControl
	actions        []mgr.RecoveryAction
	reset          uint32
	nonCrash       bool
	state          svc.State
	unsafeStart    bool
	stops          int
	pending        bool
	pendingQueries int
}

func (s *fixtureService) RecoveryActions() ([]mgr.RecoveryAction, error)   { return s.actions, nil }
func (s *fixtureService) ResetPeriod() (uint32, error)                     { return s.reset, nil }
func (s *fixtureService) RecoveryActionsOnNonCrashFailures() (bool, error) { return s.nonCrash, nil }
func (s *fixtureService) SetRecoveryActions(a []mgr.RecoveryAction, r uint32) error {
	if len(a) == 0 {
		return errors.New("empty actions require the reset API")
	}
	s.actions = a
	s.reset = r
	return nil
}
func (s *fixtureService) ResetRecoveryActions() error {
	s.actions = nil
	s.reset = 0
	return nil
}
func (s *fixtureService) SetRecoveryActionsOnNonCrashFailures(b bool) error {
	s.nonCrash = b
	return nil
}
func (s *fixtureService) Query() (svc.Status, error) {
	if s.state == svc.StartPending {
		s.pendingQueries++
		if s.pendingQueries > 1 {
			s.state = svc.Running
		}
	}
	return svc.Status{State: s.state}, nil
}
func (s *fixtureService) Control(_ svc.Cmd) (svc.Status, error) {
	if s.state == svc.StartPending {
		return svc.Status{State: s.state}, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL
	}
	s.stops++
	s.state = svc.Stopped
	err := s.control.Stop(context.Background())
	return svc.Status{State: svc.Stopped}, err
}
func (s *fixtureService) Start(...string) error {
	if s.nonCrash || len(s.actions) != 1 || s.actions[0].Type != mgr.NoAction {
		s.unsafeStart = true
	}
	s.state = svc.Running
	if s.pending && s.control.starts == 0 {
		s.state = svc.StartPending
	}
	return s.control.Start()
}

func TestWindowsServiceReplacementAndRollback(t *testing.T) {
	for _, failure := range []string{"", "startup", "commit", "pending", "no recovery"} {
		name := failure
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			p := fixturePlan(t)
			f := &fixtureControl{plan: p, failure: failure}
			if failure == "pending" {
				f.failure = "commit"
			}
			actions := []mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: time.Minute}, {Type: mgr.RunCommand, Delay: 2 * time.Minute}}
			if failure == "no recovery" {
				actions = nil
				f.failure = "startup"
			}
			service := &fixtureService{control: f, actions: actions, reset: 86400, nonCrash: true, state: svc.Running}
			expectedReset := uint32(86400)
			if failure == "no recovery" {
				expectedReset = 0
				service.reset = 0
			}
			service.pending = failure == "pending"
			closed := false
			controller := &serviceProcess{service: service, close: func() { closed = true }}
			if err := controller.Prepare(); err != nil {
				t.Fatal(err)
			}
			if err := controller.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": f.health.Version, "commit": f.health.Commit, "instanceId": f.instance})
			}))
			defer srv.Close()
			p.HealthURL = srv.URL
			err := execute(context.Background(), p, controller, 150*time.Millisecond, srv.Client())
			if (err == nil) != (failure == "") {
				t.Fatalf("service update: %v", err)
			}
			if f.db != nil {
				_ = f.db.Close()
			}
			if !closed || !reflect.DeepEqual(service.actions, actions) || service.reset != expectedReset || !service.nonCrash {
				t.Fatal("service recovery configuration was not restored")
			}
			if service.unsafeStart {
				t.Fatal("service recovery raced update or rollback")
			}
			wantStops := 1
			if failure != "" {
				wantStops = 2
			}
			if service.stops != wantStops {
				t.Fatalf("service stop count=%d", service.stops)
			}
			checkRestoration(t, p, failure == "")
		})
	}
}
