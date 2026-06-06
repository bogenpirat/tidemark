# Gio Guide for NTG

Gio version: **v0.10.0**

## Immediate-mode rendering

Gio is immediate-mode: there is no retained widget tree. Every `app.FrameEvent` re-executes all Layout calls, rebuilds op streams from scratch, and submits them to the GPU. State is stored in Go variables (AppState, widget.Clickable, etc.) between frames — not in any Gio object.

## The event loop

```go
for {
    windowEvent := window.Event()   // blocks until next event
    onPlatformEvent(windowEvent)    // Win32-specific setup (our code)
    switch typedEvent := windowEvent.(type) {
    case app.DestroyEvent:  // window closed
    case app.FrameEvent:    // render frame
    }
}
```

`window.Event()` returns an `event.Event` (from `gioui.org/io/event`). Important event types:
- `app.FrameEvent` — time to render; contains `Size image.Point`, `Metric unit.Metric`, and `Frame(*op.Ops)` to submit
- `app.DestroyEvent` — window closed, has `Err error`
- `app.Win32ViewEvent` — Windows only; contains `HWND uintptr`, fires once after window creation and once (empty) on destruction

## Rendering a frame

```go
case app.FrameEvent:
    gtx := app.NewContext(&ops, typedEvent)   // creates layout context
    rootLayout.Layout(gtx)                    // fills ops with draw commands
    typedEvent.Frame(&ops)                    // submits to GPU
    ops.Reset()                               // implicitly done by NewContext next frame
```

`op.Ops` is reused across frames. `app.NewContext` resets it implicitly on each call.

## Layout context (`layout.Context`)

`gtx` (layout.Context) carries:
- `gtx.Ops` — the op stream being built
- `gtx.Constraints` — min/max size the widget should fill
- `gtx.Metric` — unit.Metric for dp/sp↔px conversion
- `gtx.Now` — current time
- `gtx.Event(filters...)` — polls input events for registered tags
- `gtx.Dp(v unit.Dp) int` — converts dp to pixels
- `gtx.Source` — input source for gesture.* updates

## Coordinate system

Origin (0,0) is top-left. Y increases downward. Units:
- `int` pixels: used for clip rects and layout math
- `unit.Dp` (= float32): device-independent pixels, 1dp ≈ 1px at 96 DPI
- `unit.Sp` (= float32): scale-independent pixels for text
- `f32.Point`: float32 pixel coordinates for path drawing

Convert dp→px: `gtx.Dp(unit.Dp(n))` or `metric.Dp(unit.Dp(n))`  
Convert px→dp: `metric.PxToDp(px int) unit.Dp`

## Drawing primitives (op/clip, op/paint)

```go
// Filled rectangle
paint.FillShape(gtx.Ops, color, clip.Rect(image.Rect(x1,y1,x2,y2)).Op())

// Clipping + offset for a sub-widget
stack := clip.Rect(rect).Push(gtx.Ops)
offsetStack := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
// ... draw within clipped+offset space ...
offsetStack.Pop()
stack.Pop()

// Free-form polygon
var p clip.Path
p.Begin(ops)
p.MoveTo(f32.Pt(x, y))
p.LineTo(f32.Pt(x2, y2))
p.Close()
paint.FillShape(ops, color, clip.Outline{Path: p.End()}.Op())
```

Order matters: ops added later in the stream are "above" (higher z-order) earlier ones.

## Text rendering

```go
// Using material.Label (standard approach in NTG)
subGtx.Constraints = layout.Exact(image.Pt(width, height))
lbl := material.Label(matTheme, unit.Sp(11), "text")
lbl.Color = color.NRGBA{...}
lbl.Alignment = text.Middle  // or text.Start, text.End
lbl.Layout(subGtx)
```

The theme's `Shaper` must be set before use. NTG sets it in `main.go`:
```go
fontCollection := gofont.Collection()
// append Segoe UI Symbol for emoji (💡) support
if data, err := os.ReadFile(`C:\Windows\Fonts\seguisym.ttf`); err == nil {
    if faces, err := gioopentype.ParseCollection(data); err == nil {
        fontCollection = append(fontCollection, faces...)
    }
}
matTheme.Shaper = text.NewShaper(text.WithCollection(fontCollection))
```

The gofont collection alone does NOT render emoji. Segoe UI Symbol provides monochrome Unicode symbols including U+1F4A1 (💡).

## Window options

Set via `window.Option(...)` — can be called before the event loop starts or at any time:

```go
window.Option(
    app.Title("NTG — host"),
    app.Size(unit.Dp(w), unit.Dp(h)),   // initial size in dp
    app.Decorated(false),                // remove OS title bar/border
)
```

`unit.Dp` is `type Dp float32`. Untyped integer constants like `1000` convert implicitly.

## Input routing and drag regions

`system.ActionInputOp(system.ActionMove).Add(gtx.Ops)` marks the current clip area as a drag region. Gio responds to Win32 `WM_NCHITTEST` with `HTCAPTION` for those areas, giving native OS window-move behavior.

**Critical**: `ActionAt` (used by Gio internally to answer `WM_NCHITTEST`) walks the hit tree from highest to lowest z-order. It returns the FIRST area with a non-zero `action` field. Regular click handlers (buttons, etc.) have no `action` field and are skipped. Therefore, if an `ActionMove` region overlaps the button's area, clicking the button will ALSO start a window drag.

**The fix NTG uses**: the `ActionMove` rects in `layout.go` are computed to NOT cover the button row at the bottom of the stats panel. There is no overlap, so `ActionAt` never returns `ActionMove` for the button area.

```go
// layout.go — drag region calculation
buttonRowTop := totalHeight - gtx.Dp(toggleButtonHeightDp) - gtx.Dp(12) // innerPadding
// Graph area: full drag
clip.Rect(image.Rect(0, 0, graphWidth, totalHeight)).Push(gtx.Ops)
system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
// Stats panel top: drag (excludes button row)
clip.Rect(image.Rect(statsLeft, 0, totalWidth, buttonRowTop)).Push(gtx.Ops)
system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
```

## pointer.PassOp

`pointer.PassOp{}.Push(gtx.Ops)` / `.Pop()` puts subsequent `event.Op` registrations into "pass-through" mode — they receive events but do not block siblings. Not used in NTG currently (the drag approach doesn't need it).

## Widget.Clickable

```go
// In Layout:
for button.Clicked(gtx) {
    // handle click
}
// Then render:
btn := material.Button(matTheme, &button, "label")
btn.Layout(gtx)
```

`Clicked` must be called before `Layout` each frame; it processes queued gesture events. `material.Button` sets up the hit area via `widget.Clickable.layout`, which uses `clip.Rect` + `event.Op` without `PassOp` (blocking by default).

## Avoiding Gio deadlocks (Win32)

**Do NOT call Win32 functions that send messages (SetWindowPos, SetWindowLong for position, etc.) from within `Win32ViewEvent` handlers.** The HWND is being set up at that point and Win32's message pump may re-enter or block.

Safe: read the HWND from `Win32ViewEvent`, store it, use it after the first `FrameEvent`.  
Safe: call `GetWindowLong`/`SetWindowLong` for style bits (not position) from `Win32ViewEvent` — NTG does this to strip `WS_MAXIMIZEBOX` and it works reliably.  
Unsafe: `SetWindowPos` from within `Win32ViewEvent`.
