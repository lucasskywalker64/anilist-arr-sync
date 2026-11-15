//go:build !windows

package main

func runPlatformService() (bool, error) { return false, nil }
