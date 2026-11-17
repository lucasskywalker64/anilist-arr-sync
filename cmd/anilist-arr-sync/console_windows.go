package main

import (
	"context"
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// GUI builds never allocate a console. CLI launches can reuse their parent's
// console, while retaining any inherited pipes or redirected files.
func prepareConsole(cancel context.CancelFunc) func() {
	streams := []struct {
		file   **os.File
		id     uint32
		handle windows.Handle
		valid  bool
	}{
		{file: &os.Stdin, id: windows.STD_INPUT_HANDLE},
		{file: &os.Stdout, id: windows.STD_OUTPUT_HANDLE},
		{file: &os.Stderr, id: windows.STD_ERROR_HANDLE},
	}
	for i := range streams {
		s := &streams[i]
		s.handle = windows.Handle((*s.file).Fd())
		_, err := windows.GetFileType(s.handle)
		s.valid = err == nil && s.handle != 0 && s.handle != windows.InvalidHandle
	}
	api := windows.NewLazySystemDLL("kernel32.dll")
	attached, _, _ := api.NewProc("AttachConsole").Call(uintptr(^uint32(0)))
	if attached == 0 {
		return func() {} // Explorer and SCM have no parent console.
	}
	for _, s := range streams {
		if s.valid {
			_ = windows.SetStdHandle(s.id, s.handle)
		} else if handle, err := windows.GetStdHandle(s.id); err == nil {
			*s.file = os.NewFile(uintptr(handle), (*s.file).Name())
		}
	}
	log.SetOutput(os.Stderr)

	// AttachConsole resets the Go runtime's control-handler table.
	// Keep CLI cancellation and shutdown draining after attaching.
	done := make(chan struct{})
	handler := windows.NewCallback(func(event uint32) uintptr {
		switch event {
		case windows.CTRL_C_EVENT, windows.CTRL_BREAK_EVENT:
			cancel()
		case windows.CTRL_CLOSE_EVENT, windows.CTRL_LOGOFF_EVENT, windows.CTRL_SHUTDOWN_EVENT:
			cancel()
			<-done
		default:
			return 0
		}
		return 1
	})
	control := api.NewProc("SetConsoleCtrlHandler")
	_, _, _ = control.Call(handler, 1)
	return func() {
		close(done)
		_, _, _ = control.Call(handler, 0)
	}
}
