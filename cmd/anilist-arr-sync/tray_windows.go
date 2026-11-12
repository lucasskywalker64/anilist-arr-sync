package main

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var trayUser32 = windows.NewLazySystemDLL("user32.dll")
var trayShell32 = windows.NewLazySystemDLL("shell32.dll")

type trayClass struct {
	size, style                        uint32
	callback                           uintptr
	classExtra, windowExtra            int32
	instance, icon, cursor, background uintptr
	menu, name                         *uint16
	smallIcon                          uintptr
}

type trayMessage struct {
	window         uintptr
	message        uint32
	wparam, lparam uintptr
	time           uint32
	x, y           int32
	private        uint32
}

type trayIcon struct {
	size               uint32
	window             uintptr
	id, flags, message uint32
	icon               uintptr
	tip                [128]uint16
	state, stateMask   uint32
	info               [256]uint16
	version            uint32
	title              [64]uint16
	infoFlags          uint32
	guid               windows.GUID
	balloon            uintptr
}

// startTray hosts the icon on its own Windows message-loop thread. The same
// signed executable runs the service, tray, and detached update worker.
func startTray(url func() string, quit func()) (func(), error) {
	type result struct {
		window uintptr
		err    error
	}
	ready := make(chan result, 1)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		const callbackMessage = 0x8001
		defWindow := trayUser32.NewProc("DefWindowProcW")
		destroy := trayUser32.NewProc("DestroyWindow")
		notify := trayShell32.NewProc("Shell_NotifyIconW")
		var icon trayIcon
		callback := windows.NewCallback(func(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
			switch message {
			case callbackMessage:
				if lparam == 0x0203 { // WM_LBUTTONDBLCLK
					_ = openBrowser(url())
				}
				if lparam == 0x0205 { // WM_RBUTTONUP
					menu, _, _ := trayUser32.NewProc("CreatePopupMenu").Call()
					openText, _ := windows.UTF16PtrFromString("Open dashboard")
					quitText, _ := windows.UTF16PtrFromString("Quit")
					_, _, _ = trayUser32.NewProc("AppendMenuW").Call(menu, 0, 1, uintptr(unsafe.Pointer(openText)))
					_, _, _ = trayUser32.NewProc("AppendMenuW").Call(menu, 0, 2, uintptr(unsafe.Pointer(quitText)))
					var point struct{ x, y int32 }
					_, _, _ = trayUser32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
					_, _, _ = trayUser32.NewProc("SetForegroundWindow").Call(hwnd)
					choice, _, _ := trayUser32.NewProc("TrackPopupMenu").Call(menu, 0x0100|0x0002, uintptr(point.x), uintptr(point.y), 0, hwnd, 0)
					_, _, _ = trayUser32.NewProc("DestroyMenu").Call(menu)
					if choice == 1 {
						_ = openBrowser(url())
					}
					if choice == 2 {
						quit()
					}
				}
				return 0
			case 0x0010: // WM_CLOSE, including Restart Manager shutdown
				quit()
				return 0
			case 0x0011: // WM_QUERYENDSESSION
				return 1
			case 0x0016: // WM_ENDSESSION
				if wparam != 0 {
					quit()
				}
				return 0
			case 0x0002: // WM_DESTROY
				_, _, _ = notify.Call(2, uintptr(unsafe.Pointer(&icon)))
				_, _, _ = trayUser32.NewProc("PostQuitMessage").Call(0)
				return 0
			}
			value, _, _ := defWindow.Call(hwnd, uintptr(message), wparam, lparam)
			return value
		})
		name, _ := windows.UTF16PtrFromString("AniListArrSyncTray")
		var instance windows.Handle
		if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
			ready <- result{err: err}
			return
		}
		class := trayClass{callback: callback, instance: uintptr(instance), name: name}
		class.size = uint32(unsafe.Sizeof(class))
		atom, _, err := trayUser32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
		if atom == 0 {
			ready <- result{err: fmt.Errorf("register tray window: %w", err)}
			return
		}
		defer func() {
			_, _, _ = trayUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(name)), uintptr(instance))
		}()
		window, _, err := trayUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, 0, uintptr(instance), 0)
		if window == 0 {
			ready <- result{err: fmt.Errorf("create tray window: %w", err)}
			return
		}
		defer func() { _, _, _ = destroy.Call(window) }()
		appIcon, _, _ := trayUser32.NewProc("LoadIconW").Call(0, 32512)
		icon = trayIcon{window: window, id: 1, flags: 1 | 2 | 4, message: callbackMessage, icon: appIcon}
		icon.size = uint32(unsafe.Sizeof(icon))
		copy(icon.tip[:], windows.StringToUTF16("AniList-Arr-Sync"))
		ok, _, err := notify.Call(0, uintptr(unsafe.Pointer(&icon)))
		if ok == 0 {
			ready <- result{err: fmt.Errorf("add tray icon: %w", err)}
			return
		}
		ready <- result{window: window}
		var message trayMessage
		for {
			ok, _, _ := trayUser32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(ok) <= 0 {
				break
			}
			if message.message == 0x8002 {
				break
			}
			_, _, _ = trayUser32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
			_, _, _ = trayUser32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
		}
	}()
	r := <-ready
	if r.err != nil {
		return nil, r.err
	}
	return func() {
		_, _, _ = trayUser32.NewProc("PostMessageW").Call(r.window, 0x8002, 0, 0)
		<-done
	}, nil
}
