package main

import (
	"context"
	"io"
	"os"

	"github.com/lucasskywalker64/anilist-arr-sync/internal/winservice"
)

func runPlatformService() (bool, error) {
	return winservice.Run(func(ctx context.Context, ready func()) error {
		return runWithReady(ctx, os.Args[1:], io.Discard, ready)
	})
}
