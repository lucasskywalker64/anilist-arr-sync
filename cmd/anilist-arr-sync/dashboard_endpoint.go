package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

func dashboardEndpointPath(configPath string, pid uint32) string {
	return configPath + ".dashboard-" + strconv.FormatUint(uint64(pid), 10)
}

func publishDashboardEndpoint(path, endpoint string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".dashboard-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, writeErr := file.WriteString(endpoint)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
