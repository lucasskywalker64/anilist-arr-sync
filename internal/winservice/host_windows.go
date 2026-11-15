// Package winservice hosts the application under the Windows service dispatcher.
package winservice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows/svc"
)

var serviceName atomic.Value

// Name returns SCM's registered name, or empty for a directly launched process.
func Name() string {
	if name := serviceName.Load(); name != nil {
		return name.(string)
	}
	return ""
}

// Run dispatches a native service when SCM launched this process. The application
// calls ready only after its listener starts, and drains resources when cancelled.
func Run(application func(context.Context, func()) error) (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false, err
	}
	h := &host{application: application}
	// SCM ignores this table name for a SERVICE_WIN32_OWN_PROCESS registration;
	// Execute receives the actual registered name as args[0].
	err = svc.Run("", h)
	return true, errors.Join(err, h.err)
}

type host struct {
	application func(context.Context, func()) error
	err         error
}

func (h *host) Execute(args []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	serviceName.Store(args[0])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	status := svc.Status{State: svc.StartPending, WaitHint: 30000, CheckPoint: 1}
	statuses <- status
	go func() { done <- h.application(ctx, func() { once.Do(func() { close(ready) }) }) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			h.err = err
			if err != nil {
				return true, 1
			}
			return false, 0
		case <-ready:
			ready = nil
			if status.State == svc.StartPending {
				status = svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
				statuses <- status
			}
		case request, ok := <-requests:
			if !ok {
				requests = nil
				cancel()
				continue
			}
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- status
			case svc.Stop, svc.Shutdown:
				status = svc.Status{State: svc.StopPending, WaitHint: 15000, CheckPoint: 1}
				statuses <- status
				cancel()
			}
		case <-ticker.C:
			if status.State == svc.StartPending || status.State == svc.StopPending {
				status.CheckPoint++
				statuses <- status
			}
		}
	}
}
