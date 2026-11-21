package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
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
	var open func() error
	switch {
	case len(os.Args) == 4 && os.Args[1] == "--open-service-dashboard":
		open = func() error { return openServiceDashboard(os.Args[2], os.Args[3], true) }
	case len(os.Args) == 4 && os.Args[1] == "--open-installed-dashboard":
		open = func() error { return openServiceDashboard(os.Args[3], os.Args[2], false) }
	case len(os.Args) == 3 && os.Args[1] == "--open-installed-dashboard":
		open = func() error {
			pid, err := trayDashboardProcessID(os.Args[2])
			if err != nil {
				return err
			}
			return openInstalledDashboard(os.Args[2], pid)
		}
	}
	if open != nil {
		err := open()
		if err != nil && !errors.Is(err, windows.ERROR_CANCELLED) {
			message, _ := windows.UTF16PtrFromString(err.Error())
			title, _ := windows.UTF16PtrFromString("AniList-Arr-Sync")
			_, _ = windows.MessageBox(0, message, title, windows.MB_OK|windows.MB_ICONERROR)
		}
		return true, err
	}
	return winservice.Run(func(ctx context.Context, ready func()) error {
		return runInstance(ctx, os.Args[1:], io.Discard, ready, true)
	})
}

func openServiceDashboard(name, configPath string, start bool) error {
	pid, err := dashboardServiceProcessID(name, start)
	if err != nil {
		if start {
			return fmt.Errorf("start the AniList-Arr-Sync service: %w", err)
		}
		return fmt.Errorf("query the AniList-Arr-Sync service: %w", err)
	}
	return openInstalledDashboard(configPath, pid)
}

func openInstalledDashboard(configPath string, pid uint32) error {
	if pid != 0 {
		data, err := os.ReadFile(dashboardEndpointPath(configPath, pid))
		if err != nil {
			return fmt.Errorf("read the running app's dashboard URL: %w", err)
		}
		endpoint, err := url.ParseRequestURI(string(data))
		if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" || endpoint.User != nil {
			return errors.New("invalid dashboard URL from the running app")
		}
		return launchBrowser(endpoint.String())
	}
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return launchBrowser(dashboardURL(*config.NewDefault()))
	}
	if err != nil {
		return fmt.Errorf("read dashboard settings: %w", err)
	}
	cfg, err := config.ParseXML(data)
	if err != nil {
		return fmt.Errorf("read dashboard settings: %w", err)
	}
	return launchBrowser(dashboardURL(*cfg))
}

func dashboardServiceProcessID(name string, start bool) (uint32, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseServiceHandle(scm) }()
	serviceName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	handle, err := windows.OpenService(scm, serviceName, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return 0, err
	}
	service := mgr.Service{Handle: handle}
	defer func() { _ = service.Close() }()

	deadline := time.Now().Add(35 * time.Second)
	startRequested := false
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return 0, err
		}
		if status.State == svc.Running {
			return status.ProcessId, nil
		}
		if !start {
			return 0, nil
		}
		if status.State == svc.Stopped && !startRequested {
			if err := requestDashboardServiceStart(scm, serviceName, name); err != nil {
				return 0, err
			}
			startRequested = true
			deadline = time.Now().Add(35 * time.Second)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, errors.New("the service did not become ready within 35 seconds")
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
