//go:build windows

package main

import (
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

const (
	gwlStyle        = ^(uintptr(16) - 1) // GWL_STYLE     = -16
	gwlpWndProc     = ^(uintptr(4) - 1)  // GWLP_WNDPROC  = -4
	wsMaximizeBox   = uintptr(0x00010000)
	wmNcRButtonDown = 0x00A4

	swpNoSize     = uintptr(0x0001) // SWP_NOSIZE
	swpNoZOrder   = uintptr(0x0004) // SWP_NOZORDER
	swpNoActivate = uintptr(0x0010) // SWP_NOACTIVATE
)

var (
	modUser32           = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongW  = modUser32.NewProc("GetWindowLongW")
	procGetWindowLongPW = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongW  = modUser32.NewProc("SetWindowLongW")
	procSetWindowLongPW = modUser32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW = modUser32.NewProc("CallWindowProcW")
	procScreenToClient  = modUser32.NewProc("ScreenToClient")
	procGetWindowRect   = modUser32.NewProc("GetWindowRect")
	procSetWindowPos    = modUser32.NewProc("SetWindowPos")
)

// atomicWin holds the Gio window for use from the WndProc goroutine.
var atomicWin atomic.Pointer[app.Window]

// atomicHWND holds the native window handle once the Win32ViewEvent arrives,
// so the main goroutine can query/restore the window position.
var atomicHWND atomic.Uintptr

// initialPos records a screen position to apply once the native handle exists.
var (
	initialMoveOnce sync.Once
	initialMoveX    int
	initialMoveY    int
	initialMoveSet  bool
)

// winRect mirrors the Win32 RECT struct for GetWindowRect.
type winRect struct{ left, top, right, bottom int32 }

// SetInitialWindowPos records a top-left screen position (physical pixels) to
// apply to the window as soon as its native handle is available. Call before the
// event loop starts; a no-op until then.
func SetInitialWindowPos(x, y int) {
	initialMoveX, initialMoveY = x, y
	initialMoveSet = true
}

// GetWindowPosition returns the window's current top-left screen position in
// physical pixels. ok is false if the native handle is not yet available.
func GetWindowPosition() (x int, y int, ok bool) {
	hwnd := atomicHWND.Load()
	if hwnd == 0 {
		return 0, 0, false
	}
	var r winRect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return 0, 0, false
	}
	return int(r.left), int(r.top), true
}

// moveWindow positions the window's top-left at (x, y) screen pixels without
// changing its size, z-order, or activation state.
func moveWindow(hwnd uintptr, x, y int) {
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0,
		swpNoSize|swpNoZOrder|swpNoActivate)
}

var (
	installOnce  sync.Once
	origWndProc  uintptr
	wndProcCB    uintptr
	rightClickMu sync.Mutex
	rightClickReady bool
	rightClickPos   image.Point
)

// TakeRightClick returns and clears any pending right-click from the Win32 non-client
// area. Called from the main goroutine before each Layout call.
func TakeRightClick() (bool, image.Point) {
	rightClickMu.Lock()
	defer rightClickMu.Unlock()
	if rightClickReady {
		rightClickReady = false
		return true, rightClickPos
	}
	return false, image.Point{}
}

// winPoint mirrors the Win32 POINT struct for use with ScreenToClient.
type winPoint struct{ x, y int32 }

func customWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msg == wmNcRButtonDown {
		// lParam encodes screen coordinates as two signed 16-bit values.
		pt := winPoint{
			x: int32(int16(lParam)),
			y: int32(int16(lParam >> 16)),
		}
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))

		rightClickMu.Lock()
		rightClickReady = true
		rightClickPos = image.Pt(int(pt.x), int(pt.y))
		rightClickMu.Unlock()

		// Trigger a new frame so the context menu appears immediately.
		if w := atomicWin.Load(); w != nil {
			w.Invalidate()
		}
		return 0 // prevent Win32's default system-menu handling
	}
	r, _, _ := procCallWindowProcW.Call(origWndProc, hwnd, msg, wParam, lParam)
	return r
}

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

func setWndProc(hwnd, proc uintptr) uintptr {
	if runtime.GOARCH == "386" {
		v, _, _ := procSetWindowLongW.Call(hwnd, gwlpWndProc, proc)
		return v
	}
	v, _, _ := procSetWindowLongPW.Call(hwnd, gwlpWndProc, proc)
	return v
}

func onPlatformEvent(win *app.Window, e event.Event) {
	ev, ok := e.(app.Win32ViewEvent)
	if !ok || !ev.Valid() {
		return
	}
	hwnd := ev.HWND
	atomicWin.Store(win)
	atomicHWND.Store(hwnd)
	// Win32 APIs that send WM_STYLECHANGED (SetWindowLongPtr for GWL_STYLE) must run
	// on a separate goroutine — calling them from the main goroutine deadlocks because
	// SendMessage blocks the caller while the Win32 thread waits to post a FrameEvent
	// to a channel nobody is draining. See .agents/constraints.md constraint #1.
	go func() {
		style := getWindowLong(hwnd)
		setWindowLong(hwnd, style&^wsMaximizeBox)

		// Subclass the WndProc once to intercept WM_NCRBUTTONDOWN. ActionMove regions
		// return HTCAPTION from WM_NCHITTEST, so right-clicks there arrive as
		// WM_NCRBUTTONDOWN (non-client), which Gio does not route as pointer events.
		// SetWindowLongPtr for GWLP_WNDPROC does not send WM_STYLECHANGED, so it is
		// safe here in the same goroutine.
		installOnce.Do(func() {
			wndProcCB = syscall.NewCallback(customWndProc)
			origWndProc = setWndProc(hwnd, wndProcCB)
		})

		// Restore the saved position once, after the handle exists.
		if initialMoveSet {
			initialMoveOnce.Do(func() {
				moveWindow(hwnd, initialMoveX, initialMoveY)
			})
		}
	}()
}
