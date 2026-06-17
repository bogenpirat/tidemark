//go:build darwin

// Package main — macOS (AppKit/Cocoa) platform behaviors for Tidemark.
//
// This mirrors platform_windows.go for macOS. Gio surfaces the native NSView via
// app.AppKitViewEvent; from it we reach the NSWindow to implement:
//
//   - Right-clicks in window-drag regions → context menu. Gio implements the
//     ActionMove drag with -[NSWindow performWindowDragWithEvent:], which it
//     calls for *any* mouse button whose press lands on a drag region (see
//     os_macos.go gio_onMouse). That swallows right-clicks before they reach the
//     app as pointer events — the exact analogue of the Win32 WM_NCRBUTTONDOWN
//     problem (constraints.md #12). We install a process-local NSEvent monitor
//     for right-mouse-down on the main window, convert to client pixels, hand the
//     point to the UI, and consume the event so no drag starts.
//
//   - Window position save/restore. The position is cached on the main thread via
//     NSWindowDidMove/DidResize observers and read lock-free by the event loop.
//
// IMPORTANT — main-thread deadlock (the macOS analogue of constraints.md #1):
// while the event-loop goroutine handles a FrameEvent, the main OS thread is
// blocked inside Gio's gio_onDraw → eventLoop.deliverEvent select, NOT servicing
// the main dispatch queue. Therefore the event loop must NEVER call
// dispatch_sync(dispatch_get_main_queue(), …): the block would never run and both
// sides would hang. We avoid this by reading position from a cache (updated by
// observers) and by moving the window with dispatch_async (fire-and-forget).
package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// Go callbacks (defined via //export in this package).
extern void tmRightClick(int x, int y);
extern void tmWindowMoved(int x, int y);

// gMainView is the NSView of the main (graph) window. It is only ever assigned
// and read on the main thread (inside tm_install and the monitor/observer
// blocks), so it needs no synchronization. AppKit retains the view for the life
// of the window.
static CFTypeRef gMainView = NULL;
static id gRightClickMonitor = NULL;
static id gMoveObserver = NULL;
static id gResizeObserver = NULL;

// tm_primary_screen_height returns the height (in points) of the primary screen
// (screens[0], whose bottom-left is the Cocoa global origin), used to flip
// between Cocoa's bottom-left and our top-left coordinate convention.
static double tm_primary_screen_height(void) {
	NSArray<NSScreen *> *screens = [NSScreen screens];
	if (screens.count == 0) {
		return 0;
	}
	return screens[0].frame.size.height;
}

// tm_store_pos computes the window's top-left corner in physical pixels (origin
// at the top-left of the primary screen, Y down — matching the Win32 convention)
// and pushes it to the Go-side cache. Must run on the main thread.
static void tm_store_pos(NSWindow *w) {
	if (w == nil) {
		return;
	}
	NSRect f = [w frame];
	CGFloat scale = w.backingScaleFactor;
	double screenH = tm_primary_screen_height();
	int x = (int)(f.origin.x * scale);
	int y = (int)((screenH - (f.origin.y + f.size.height)) * scale);
	tmWindowMoved(x, y);
}

// tm_install wires up the right-click monitor, the position-cache observers, and
// seeds the cache with the current position. Safe to call from any goroutine; the
// work is marshaled to the main thread.
static void tm_install(uintptr_t viewHandle) {
	dispatch_block_t work = ^{
		gMainView = (CFTypeRef)viewHandle;
		NSView *view = (__bridge NSView *)(CFTypeRef)viewHandle;
		NSWindow *window = view.window;

		if (gRightClickMonitor == NULL) {
			gRightClickMonitor = [NSEvent
				addLocalMonitorForEventsMatchingMask:NSEventMaskRightMouseDown
				handler:^NSEvent *(NSEvent *event) {
					if (gMainView == NULL) {
						return event;
					}
					NSView *v = (__bridge NSView *)gMainView;
					if (v.window == nil || [event window] != v.window) {
						return event; // not our window (e.g. settings dialog)
					}
					NSPoint p = [v convertPoint:[event locationInWindow] fromView:nil];
					CGFloat h = v.bounds.size.height;
					CGFloat s = v.window.backingScaleFactor;
					// Gio's coordinate space is top-left origin, physical pixels.
					tmRightClick((int)(p.x * s), (int)((h - p.y) * s));
					return nil; // consume: no drag, no system menu
				}];
		}

		if (gMoveObserver == NULL && window != nil) {
			NSOperationQueue *mainQ = [NSOperationQueue mainQueue];
			NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
			gMoveObserver = [nc addObserverForName:NSWindowDidMoveNotification
				object:window queue:mainQ usingBlock:^(NSNotification *note) {
					tm_store_pos((NSWindow *)note.object);
				}];
			gResizeObserver = [nc addObserverForName:NSWindowDidResizeNotification
				object:window queue:mainQ usingBlock:^(NSNotification *note) {
					tm_store_pos((NSWindow *)note.object);
				}];
		}

		tm_store_pos(window); // seed the cache
	};
	if ([NSThread isMainThread]) {
		work();
	} else {
		dispatch_async(dispatch_get_main_queue(), work);
	}
}

// tm_set_window_pos moves the window so its top-left corner is at (x, y) physical
// pixels (top-left primary-screen origin, Y down). Uses dispatch_async so it
// never blocks the caller (see the deadlock note above).
static void tm_set_window_pos(uintptr_t viewHandle, int x, int y) {
	dispatch_block_t work = ^{
		if (viewHandle == 0) {
			return;
		}
		NSView *v = (__bridge NSView *)(CFTypeRef)viewHandle;
		NSWindow *w = v.window;
		if (w == nil) {
			return;
		}
		CGFloat scale = w.backingScaleFactor;
		double screenH = tm_primary_screen_height();
		double xpt = (double)x / scale;
		double yTopLeft = (double)y / scale;
		// setFrameTopLeftPoint takes the top-left corner in Cocoa screen
		// coordinates (Y up from the bottom of the primary screen).
		NSPoint topLeft = NSMakePoint(xpt, screenH - yTopLeft);
		[w setFrameTopLeftPoint:topLeft];
	};
	if ([NSThread isMainThread]) {
		work();
	} else {
		dispatch_async(dispatch_get_main_queue(), work);
	}
}
*/
import "C"

import (
	"image"
	"os"
	"sync"
	"sync/atomic"

	"gioui.org/app"
	giofont "gioui.org/font"
	gioopentype "gioui.org/font/opentype"
	"gioui.org/io/event"
)

var (
	// atomicWin holds the Gio window for use from the right-click monitor.
	atomicWin atomic.Pointer[app.Window]

	rightClickMu    sync.Mutex
	rightClickReady bool
	rightClickPos   image.Point

	posMu    sync.Mutex
	posX     int
	posY     int
	posValid bool

	setupOnce       sync.Once
	initialMoveOnce sync.Once
	initialMoveSet  bool
	initialMoveX    int
	initialMoveY    int
)

// SetInitialWindowPos records a top-left screen position (physical pixels) to
// apply to the window once its native handle is available. Call before the event
// loop starts; a no-op until then.
func SetInitialWindowPos(x, y int) {
	initialMoveX, initialMoveY = x, y
	initialMoveSet = true
}

// GetWindowPosition returns the window's last-known top-left screen position in
// physical pixels, as cached by the NSWindowDidMove/DidResize observers. ok is
// false until the cache has been seeded. This never touches the main thread, so
// it is safe to call from the event loop during a FrameEvent.
func GetWindowPosition() (x int, y int, ok bool) {
	posMu.Lock()
	defer posMu.Unlock()
	return posX, posY, posValid
}

// TakeRightClick returns and clears any pending right-click captured by the
// native monitor. Called from the event-loop goroutine before each Layout call.
func TakeRightClick() (bool, image.Point) {
	rightClickMu.Lock()
	defer rightClickMu.Unlock()
	if rightClickReady {
		rightClickReady = false
		return true, rightClickPos
	}
	return false, image.Point{}
}

//export tmRightClick
func tmRightClick(x, y C.int) {
	rightClickMu.Lock()
	rightClickReady = true
	rightClickPos = image.Pt(int(x), int(y))
	rightClickMu.Unlock()
	if w := atomicWin.Load(); w != nil {
		w.Invalidate()
	}
}

//export tmWindowMoved
func tmWindowMoved(x, y C.int) {
	posMu.Lock()
	posX, posY, posValid = int(x), int(y), true
	posMu.Unlock()
}

func onPlatformEvent(win *app.Window, e event.Event) {
	ev, ok := e.(app.AppKitViewEvent)
	if !ok || !ev.Valid() {
		return
	}
	atomicWin.Store(win)

	setupOnce.Do(func() {
		C.tm_install(C.uintptr_t(ev.View))
	})

	if initialMoveSet {
		initialMoveOnce.Do(func() {
			C.tm_set_window_pos(C.uintptr_t(ev.View), C.int(initialMoveX), C.int(initialMoveY))
		})
	}
}

// loadSymbolFontFaces returns extra font faces providing Unicode symbol/emoji
// glyphs (notably U+1F4A1 💡, used by the theme-toggle button). Best-effort: it
// tries a few macOS system fonts and returns nil if none are available. Note
// that macOS ships U+1F4A1 only in color-emoji fonts, which Gio's opentype
// renderer cannot reliably rasterize, so the toggle glyph may still render as a
// box — see README.md. The button remains fully functional regardless.
func loadSymbolFontFaces() []giofont.FontFace {
	candidates := []string{
		"/System/Library/Fonts/Apple Symbols.ttf",
		"/Library/Fonts/Arial Unicode.ttf",
		"/System/Library/Fonts/Apple Color Emoji.ttc",
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		faces, err := gioopentype.ParseCollection(data)
		if err != nil {
			continue
		}
		return faces
	}
	return nil
}
