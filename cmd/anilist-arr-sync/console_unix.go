//go:build !windows

package main

import "context"

func prepareConsole(context.CancelFunc) func() { return func() {} }
