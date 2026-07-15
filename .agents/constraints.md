# Constraints and Gotchas

Hard-won knowledge from this project's development. Read before making changes.

---

## 1. Never call SendMessage-using Win32 APIs from the main goroutine

**What happened (twice)**:
- A window-position feature called `SetWindowPos` from `Win32ViewEvent` → app froze on launch.
- A WS_MAXIMIZEBOX-stripping feature called `SetWindowLongPtrW` from the main goroutine (first from `Win32ViewEvent`, then from the `FrameEvent` handler after a mistaken "fix") → app froze on launch.

**Why — the deadlock mechanism**:
`SetWindowPos`, `SetWindowLongPtrW` (with `GWL_STYLE`), and many other Win32 APIs send `WM_STYLECHANGED`, `WM_WINDOWPOSCHANGED`, etc. via `SendMessage` — a *synchronous* cross-thread call that **blocks the calling OS thread** until Gio's Win32 thread processes the message. If the main goroutine is the caller:

1. Main goroutine blocks inside `SendMessage`, waiting for the Win32 thread.
2. Win32 thread tries to enqueue a `FrameEvent` into Gio's Go channel.
3. Channel is full because nobody is reading it (main goroutine is stuck).
4. Win32 thread blocks on the channel write — it never reaches `GetMessage`.
5. `SendMessage` never gets processed → permanent deadlock.

Moving the call to a `FrameEvent` handler does NOT help — the main goroutine is still the caller.

**Fix**: Run the Win32 API call on a **separate goroutine**. The main goroutine stays free to drain the event channel; the Win32 thread can return to `GetMessage` and process the message; the goroutine unblocks. No sleep needed.

```go
func onPlatformEvent(e event.Event) {
    ev, ok := e.(app.Win32ViewEvent)
    if !ok || !ev.Valid() { return }
    hwnd := ev.HWND
    go func() {
        style := getWindowLong(hwnd)
        setWindowLong(hwnd, style&^wsMaximizeBox)
    }()
}
```

**Rule**: Any Win32 API that internally uses `SendMessage` must be called from a dedicated goroutine, never from the main Gio event-loop goroutine. `GetWindowLong` (read-only, no messages) is safe anywhere.

---

## 2. system.ActionInputOp(ActionMove) must not overlap interactive widgets

**What happened**: Registering `ActionMove` for the full window caused `ActionAt` to always return `ActionMove` even when hovering over the toggle button, because `ActionAt` skips regular click-handler nodes (they have no `action` field) and finds the `ActionMove` region below.

**Rule**: `ActionMove` regions must be explicitly cut out around any interactive widget. Tidemark excludes the button row by computing `buttonRowTop = totalHeight - gtx.Dp(toggleButtonHeightDp) - gtx.Dp(12)` and registering two separate rects that don't cover that strip.

**Important**: The `12` in the formula above is the inner padding (`innerPadding = gtx.Dp(12)`) used in `statspanel.go`. If the button position ever changes, this formula must be updated in `layout.go` to match.

---

## 3. Double-click on HTCAPTION maximizes by default

**What happened**: Using `system.ActionInputOp(ActionMove)` makes Gio return `HTCAPTION` from `WM_NCHITTEST`. Win32's default handling of a double-click on `HTCAPTION` is to toggle maximize.

**Fix**: Strip `WS_MAXIMIZEBOX` from `GWL_STYLE` on a separate goroutine started from the `Win32ViewEvent` handler (see constraint #1 for why a goroutine is required). This is done in `platform_windows.go → onPlatformEvent`.

**Side effect**: The maximize button (if it existed) would also be hidden, but since decorations are removed (`app.Decorated(false)`), this is irrelevant.

---

## 4. gofont does not render emoji

**What happened**: Setting the toggle button label to `"💡"` showed a blank square because gofont (Go Regular) has no emoji glyphs.

**Fix**: Load `C:\Windows\Fonts\seguisym.ttf` (Segoe UI Symbol) and append its faces to the font collection before creating the shaper. Segoe UI Symbol has monochrome outlines for Unicode symbols including U+1F4A1 (💡). The loading is best-effort — if the file is missing, the app still runs (button shows a square).

Do NOT try to use `seguiemj.ttf` (Segoe UI Emoji) for this. That font uses COLR/CPAL color tables; Gio's opentype package only has automatic PNG bitmap decoding, making color emoji rendering unreliable.

---

## 5. Window position saving caused persistent UI freeze

A feature to save and restore window X/Y position (in addition to width/height) was attempted and caused the UI to freeze. The feature was fully reverted. Only window dimensions (`windowWidthDp`, `windowHeightDp`) are saved — no position.

**Do not attempt to save/restore window position** without first solving the `SetWindowPos`-in-Win32ViewEvent problem (constraint #1 above) and testing thoroughly.

---

## 6. Ring buffer is NOT goroutine-safe

`buffer.RingBuffer` has no internal locking. It must only be accessed from the main goroutine. The bridge goroutine writes to `pendingPoints` (protected by `pendingMu`), and the main goroutine drains `pendingPoints` into the ring buffer during `FrameEvent`. Never call `dataBuffer.Push` or `dataBuffer.Snapshot` from the bridge or SNMP goroutines.

---

## 7. The SNMP baseline tick emits nothing

On the very first successful poll, `service.go` records the counter values as a baseline and emits no `DataPoint`. This means the graph always starts with one missing second. This is intentional — without a baseline, there's no delta to compute.

---

## 8. Config file is re-written on every clean close

`SaveConfig` re-writes the entire JSON file with `json.MarshalIndent`. This means:
- Unknown fields in the original JSON are dropped (Go's JSON marshaling only writes struct fields)
- The indentation changes to tabs regardless of the original formatting
- `windowWidthDp` and `windowHeightDp` are added/updated with the current window size

Fields with `omitempty` (`windowWidthDp`, `windowHeightDp`) are omitted if zero, but since they're set on every clean close they will normally be present.

---

## 9. app.Size accepts untyped integer constants

`unit.Dp` is `type Dp float32`. `app.Size(1000, 500)` works because untyped integer constants implicitly convert to `float32`. However, computed values from config must be explicitly wrapped: `app.Size(unit.Dp(w), unit.Dp(h))`.

---

## 10. The drag area boundary uses layout constants from statspanel.go

`layout.go` hardcodes `gtx.Dp(12)` as the inner padding when computing where the button row starts. This `12` comes from the local `innerPadding := gtx.Dp(12)` in `StatsPanel.Layout`. These two must stay in sync. If the inner padding in `statspanel.go` changes, `layout.go` must be updated to match, or the button will partially overlap the drag region.

---

## 12. Right-clicks in HTCAPTION regions require WndProc subclassing

`ActionMove` regions return `HTCAPTION` from `WM_NCHITTEST`. Win32 routes right-clicks in `HTCAPTION` areas as `WM_NCRBUTTONDOWN` (non-client button down), not `WM_RBUTTONDOWN`. Gio only processes client-area messages and never exposes `WM_NCRBUTTONDOWN` as a pointer event, so `pointer.Filter` and right-click gesture detection do not work in drag regions.

**Fix**: subclass the WndProc via `SetWindowLongPtrW(hwnd, GWLP_WNDPROC, callback)` in the same goroutine that strips `WS_MAXIMIZEBOX`. `GWLP_WNDPROC` does not send `WM_STYLECHANGED` so there is no deadlock risk (constraint #1 applies only to `GWL_STYLE`). The custom WndProc intercepts `WM_NCRBUTTONDOWN`, converts screen→client coordinates with `ScreenToClient`, stores the result behind a mutex, invalidates the window, and returns 0 (suppressing Win32's default system-menu). The main goroutine calls `TakeRightClick()` before each `Layout` to consume this result.

**Do not** try to detect right-clicks in drag regions using Gio pointer events — they will never fire for `WM_NCRBUTTONDOWN`.

## 13. Mouse hover in HTCAPTION regions comes from WM_NCMOUSEMOVE, and leave events need TrackMouseEvent

Like right-clicks (constraint #12), mouse *moves* over `ActionMove` drag regions arrive
as non-client messages (`WM_NCMOUSEMOVE`) that Gio never exposes as pointer events. The
graph's hover tooltip is therefore fed from the WndProc subclass in
`platform_windows.go`: `WM_NCMOUSEMOVE`/`WM_MOUSEMOVE` update a mutex-guarded position
(`HoverPosition()` peeks it each frame from `main.go`), and the messages are **passed
through** to the original WndProc so dragging keeps working.

**Gotcha**: Windows does not send `WM_NCMOUSELEAVE`/`WM_MOUSELEAVE` unless you arm them
with `TrackMouseEvent` — and for the non-client variant the `TME_NONCLIENT` flag is
required. The arm is one-shot: it must be re-requested after every leave, so
`requestMouseLeaveEvent` is called on every move message. Without this, the tooltip
would stick around after the mouse exits the window.

## 11. Build constraints for platform-specific code

`platform_windows.go` uses `//go:build windows` and references `app.Win32ViewEvent` which only exists in `gioui.org/app/os_windows.go`. Without this constraint the package won't compile on non-Windows. The companion `platform.go` uses `//go:build !windows` to provide a no-op stub, keeping the cross-platform build graph valid.
