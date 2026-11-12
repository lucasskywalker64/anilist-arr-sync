//go:build !windows

package main

import "errors"

func startTray(_ string, _ func()) (func(), error) {
	return nil, errors.New("--tray is supported only on Windows")
}
