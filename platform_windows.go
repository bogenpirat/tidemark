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
	style := getWindowLong(ev.HWND)
	setWindowLong(ev.HWND, style&^wsMaximizeBox)
}
