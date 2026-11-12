package winservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

func TestServiceReadinessAndShutdown(t *testing.T) {
	for _, command := range []svc.Cmd{svc.Stop, svc.Shutdown} {
		t.Run(commandName(command), func(t *testing.T) {
			start := make(chan struct{})
			drained := make(chan struct{})
			cancelled := make(chan struct{})
			h := &host{application: func(ctx context.Context, ready func()) error {
				<-start
				ready()
				<-ctx.Done()
				close(cancelled)
				<-drained
				return nil
			}}
			requests := make(chan svc.ChangeRequest)
			statuses := make(chan svc.Status, 10)
			done := make(chan uint32, 1)
			go func() { _, code := h.Execute([]string{"fixture-service"}, requests, statuses); done <- code }()
			if status := nextStatus(t, statuses); status.State != svc.StartPending {
				t.Fatal("reported running before startup")
			}
			if Name() != "fixture-service" {
				t.Fatal("lost registered service name")
			}
			close(start)
			if status := nextStatus(t, statuses); status.State != svc.Running || status.Accepts != svc.AcceptStop|svc.AcceptShutdown {
				t.Fatalf("running status: %+v", status)
			}
			requests <- svc.ChangeRequest{Cmd: svc.Interrogate}
			if status := nextStatus(t, statuses); status.State != svc.Running {
				t.Fatal("interrogation changed service state")
			}
			requests <- svc.ChangeRequest{Cmd: command}
			if status := nextStatus(t, statuses); status.State != svc.StopPending {
				t.Fatal("missing stop-pending status")
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("SCM stop did not cancel runtime")
			}
			select {
			case <-done:
				t.Fatal("service stopped before resources drained")
			default:
			}
			close(drained)
			select {
			case code := <-done:
				if code != 0 {
					t.Fatalf("clean stop code=%d", code)
				}
			case <-time.After(time.Second):
				t.Fatal("service did not stop")
			}
		})
	}
}

func TestServiceStartupFailure(t *testing.T) {
	failure := errors.New("fixture startup failure")
	h := &host{application: func(context.Context, func()) error { return failure }}
	statuses := make(chan svc.Status, 10)
	specific, code := h.Execute([]string{"fixture-service"}, make(chan svc.ChangeRequest), statuses)
	if !specific || code == 0 || !errors.Is(h.err, failure) {
		t.Fatal("startup failure was reported as success")
	}
	for len(statuses) > 0 {
		if status := <-statuses; status.State == svc.Running {
			t.Fatal("failed startup reported healthy service")
		}
	}
}

func nextStatus(t *testing.T, statuses <-chan svc.Status) svc.Status {
	t.Helper()
	select {
	case s := <-statuses:
		return s
	case <-time.After(time.Second):
		t.Fatal("missing SCM status")
		return svc.Status{}
	}
}

func commandName(command svc.Cmd) string {
	if command == svc.Stop {
		return "stop"
	}
	return "shutdown"
}
