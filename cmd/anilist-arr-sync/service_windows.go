package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/config"
	"github.com/lucasskywalker64/anilist-arr-sync/internal/winservice"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func runPlatformService() (bool, error) {
	if len(os.Args) == 4 && os.Args[1] == "--open-service-dashboard" {
		err := openServiceDashboard(os.Args[2], os.Args[3])
		if err != nil && !errors.Is(err, windows.ERROR_CANCELLED) {
			message, _ := windows.UTF16PtrFromString(err.Error())
			title, _ := windows.UTF16PtrFromString("AniList-Arr-Sync")
			_, _ = windows.MessageBox(0, message, title, windows.MB_OK|windows.MB_ICONERROR)
		}
		return true, err
	}
	return winservice.Run(func(ctx context.Context, ready func()) error {
		return runWithReady(ctx, os.Args[1:], io.Discard, ready)
	})
}

func openServiceDashboard(name, configPath string) error {
	if err := waitForDashboardService(name); err != nil {
		return fmt.Errorf("start the AniList-Arr-Sync service: %w", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read dashboard settings: %w", err)
	}
	cfg, err := config.ParseXML(data)
	if err != nil {
		return fmt.Errorf("read dashboard settings: %w", err)
	}
	return openBrowser(dashboardURL(*cfg))
}

func waitForDashboardService(name string) error {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseServiceHandle(scm) }()
	serviceName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	handle, err := windows.OpenService(scm, serviceName, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return err
	}
	service := mgr.Service{Handle: handle}
	defer func() { _ = service.Close() }()

	deadline := time.Now().Add(35 * time.Second)
	startRequested := false
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == svc.Running {
			return nil // The service reports Running once its HTTP listener is ready.
		}
		if status.State == svc.Stopped && !startRequested {
			if err := requestDashboardServiceStart(scm, serviceName, name); err != nil {
				return err
			}
			startRequested = true
			deadline = time.Now().Add(35 * time.Second)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the service did not become ready within 35 seconds")
}

func requestDashboardServiceStart(scm windows.Handle, serviceName *uint16, name string) error {
	handle, err := windows.OpenService(scm, serviceName, windows.SERVICE_START)
	if err == nil {
		defer func() { _ = windows.CloseServiceHandle(handle) }()
		err = windows.StartService(handle, 0, nil)
		if errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return nil
		}
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return err
	}
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	sc, _ := windows.UTF16PtrFromString(filepath.Join(dir, "sc.exe"))
	args, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{"start", name}))
	return windows.ShellExecute(0, verb, sc, args, nil, windows.SW_HIDE)
}
