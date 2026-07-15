//go:build windows

package main

import (
	"image"
	"os"
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
	wmNcMouseMove   = 0x00A0
	wmMouseMove     = 0x0200
	wmNcMouseLeave  = 0x02A2
	wmMouseLeave    = 0x02A3

	tmeLeave     = uint32(0x00000002) // TME_LEAVE
	tmeNonClient = uint32(0x00000010) // TME_NONCLIENT

	swpNoSize     = uintptr(0x0001) // SWP_NOSIZE
	swpNoZOrder   = uintptr(0x0004) // SWP_NOZORDER
	swpNoActivate = uintptr(0x0010) // SWP_NOACTIVATE
)

var (
	modKernel32       = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = modKernel32.NewProc("AttachConsole")

	modUser32           = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongW  = modUser32.NewProc("GetWindowLongW")
	procGetWindowLongPW = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongW  = modUser32.NewProc("SetWindowLongW")
	procSetWindowLongPW = modUser32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW = modUser32.NewProc("CallWindowProcW")
	procScreenToClient  = modUser32.NewProc("ScreenToClient")
	procGetWindowRect   = modUser32.NewProc("GetWindowRect")
	procSetWindowPos    = modUser32.NewProc("SetWindowPos")
	procTrackMouseEvent = modUser32.NewProc("TrackMouseEvent")
)

// AttachParentConsole attaches the process to its parent's console (if any)
// and rebinds stdout/stderr to it. The release build is a windowsgui binary
// that starts without a console, so CLI modes like -hostkey call this to make
// their output visible when launched from a terminal. Best-effort: launched
// from Explorer there is no parent console and the streams stay untouched.
// Streams the shell already redirected to a file or pipe are valid handles and
// are left alone.
func AttachParentConsole() {
	const attachParentProcess = uintptr(0xFFFFFFFF) // ATTACH_PARENT_PROCESS = (DWORD)-1
	if ret, _, _ := procAttachConsole.Call(attachParentProcess); ret == 0 {
		return
	}
	streamInvalid := func(f *os.File) bool {
		return f == nil || f.Fd() == 0 || f.Fd() == uintptr(syscall.InvalidHandle)
	}
	if streamInvalid(os.Stdout) || streamInvalid(os.Stderr) {
		if conOut, openError := os.OpenFile("CONOUT$", os.O_WRONLY, 0); openError == nil {
			if streamInvalid(os.Stdout) {
				os.Stdout = conOut
			}
			if streamInvalid(os.Stderr) {
				os.Stderr = conOut
			}
		}
	}
}

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

// trackMouseEventData mirrors the Win32 TRACKMOUSEEVENT struct.
type trackMouseEventData struct {
	cbSize      uint32
	dwFlags     uint32
	hwndTrack   uintptr
	dwHoverTime uint32
}

var (
	hoverMu    sync.Mutex
	hoverValid bool
	hoverPos   image.Point
)

// HoverPosition returns the last known mouse position in client coordinates,
// or ok=false when the mouse has left the window. Unlike TakeRightClick this
// peeks without clearing: hover persists across frames until a leave event.
// Mouse moves over ActionMove drag regions (HTCAPTION) never reach Gio as
// pointer events, so the graph's hover tooltip is fed from here instead (see
// .agents/constraints.md #12 for the underlying limitation).
func HoverPosition() (image.Point, bool) {
	hoverMu.Lock()
	defer hoverMu.Unlock()
	return hoverPos, hoverValid
}

// updateHover stores a new hover position and requests a frame when it
// actually changed.
func updateHover(pt image.Point) {
	hoverMu.Lock()
	changed := !hoverValid || hoverPos != pt
	hoverValid = true
	hoverPos = pt
	hoverMu.Unlock()
	if changed {
		if w := atomicWin.Load(); w != nil {
			w.Invalidate()
		}
	}
}

// clearHover invalidates the hover state when the mouse leaves the window.
func clearHover() {
	hoverMu.Lock()
	changed := hoverValid
	hoverValid = false
	hoverMu.Unlock()
	if changed {
		if w := atomicWin.Load(); w != nil {
			w.Invalidate()
		}
	}
}

// requestMouseLeaveEvent arms Win32 to send a WM_(NC)MOUSELEAVE when the
// mouse leaves the window, so the hover state can be cleared. Must be
// re-armed after every leave; calling it on each move is the standard
// pattern and is cheap.
func requestMouseLeaveEvent(hwnd uintptr, nonClient bool) {
	flags := tmeLeave
	if nonClient {
		flags |= tmeNonClient
	}
	tme := trackMouseEventData{
		cbSize:    uint32(unsafe.Sizeof(trackMouseEventData{})),
		dwFlags:   flags,
		hwndTrack: hwnd,
	}
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
}

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

	// Hover tracking for the graph tooltip. These messages are observed and
	// passed on to the original WndProc so window dragging keeps working.
	switch msg {
	case wmNcMouseMove:
		// lParam encodes screen coordinates; convert to client space.
		pt := winPoint{
			x: int32(int16(lParam)),
			y: int32(int16(lParam >> 16)),
		}
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		updateHover(image.Pt(int(pt.x), int(pt.y)))
		requestMouseLeaveEvent(hwnd, true)
	case wmMouseMove:
		// lParam already encodes client coordinates.
		updateHover(image.Pt(int(int16(lParam)), int(int16(lParam>>16))))
		requestMouseLeaveEvent(hwnd, false)
	case wmNcMouseLeave, wmMouseLeave:
		clearHover()
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
