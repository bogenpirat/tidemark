//go:build windows

package main

import (
	"runtime"
	"syscall"

	"gioui.org/app"
	"gioui.org/io/event"
)

const (
	gwlStyle      = ^(uintptr(16) - 1) // -16
	wsMaximizeBox = uintptr(0x00010000)
)

var (
	modUser32           = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongW  = modUser32.NewProc("GetWindowLongW")
	procGetWindowLongPW = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongW  = modUser32.NewProc("SetWindowLongW")
	procSetWindowLongPW = modUser32.NewProc("SetWindowLongPtrW")
)

func getWindowLong(hwnd uintptr) uintptr {
	if runtime.GOARCH == "386" {
		v, _, _ := procGetWindowLongW.Call(hwnd, gwlStyle)
		return v
	}
	v, _, _ := procGetWindowLongPW.Call(hwnd, gwlStyle)
	return v
}

func setWindowLong(hwnd, style uintptr) {
	if runtime.GOARCH == "386" {
		procSetWindowLongW.Call(hwnd, gwlStyle, style)
	} else {
		procSetWindowLongPW.Call(hwnd, gwlStyle, style)
	}
}

func onPlatformEvent(e event.Event) {
	ev, ok := e.(app.Win32ViewEvent)
	if !ok || !ev.Valid() {
		return
	}
	hwnd := ev.HWND
	// SetWindowLongPtrW uses SendMessage internally, which blocks the calling
	// goroutine until Gio's Win32 thread processes WM_STYLECHANGED. Calling it
	// from the main goroutine deadlocks: the main goroutine blocks in SendMessage
	// while the Win32 thread blocks trying to send a FrameEvent into a full
	// channel that nobody is reading. Running it on a separate goroutine keeps
	// the main goroutine free to drain the channel, so the Win32 thread can
	// return to GetMessage and process the message.
	go func() {
		style := getWindowLong(hwnd)
		setWindowLong(hwnd, style&^wsMaximizeBox)
	}()
}
