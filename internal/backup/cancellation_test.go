package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type cancellingWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancellingWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	w.cancel()
	return n, err
}

func TestArchiveCopyStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	destination := &cancellingWriter{cancel: cancel}
	source := io.MultiReader(strings.NewReader("first"), strings.NewReader("second"))
	n, err := io.Copy(contextWriter{ctx: ctx, dst: destination}, source)
	if !errors.Is(err, context.Canceled) || n != 5 || destination.String() != "first" {
		t.Fatalf("copy continued after cancellation: n=%d, data=%q, err=%v", n, destination.String(), err)
	}
}
