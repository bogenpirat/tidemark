# Architecture

## What the program does

Reads one JSON config file, opens an undecorated borderless window, polls an SNMP v2c host once per second, and renders a live scrolling graph of download (red) and upload (green) throughput. A narrow stats panel on the right shows current/max/avg and a theme-toggle button. Intended to run as 3 side-by-side instances for 3 hosts.

## Goroutines

```
main goroutine
  └── Gio event loop (window.Event())
        ├── app.FrameEvent  → drains pendingPoints, pushes to ring buffer, renders
        └── app.DestroyEvent → cancel context, save config, exit

snmp goroutine (go snmpService.Start)
  └── polls every 1 second, sends model.DataPoint to snmpOutputChannel (buffered, cap 10)

bridge goroutine (go func)
  └── reads snmpOutputChannel, appends to pendingPoints slice, calls window.Invalidate()
```

**Concurrency rule**: `pendingPoints` is the only shared state. It is protected by `pendingMu` (sync.Mutex). The ring buffer (`DataBuffer`) is only accessed from the main goroutine (written in FrameEvent, read by Gio layout calls in the same goroutine). No other sharing.

## Data flow

```
SNMP host
  → gosnmp.Get (in snmp goroutine)
  → computeCounterDelta (bytes/sec from raw counter diff)
  → model.DataPoint  →  snmpOutputChannel  →  pendingPoints  (bridge goroutine)
                                                     ↓
                                              window.Invalidate()
                                                     ↓
                                           app.FrameEvent fires
                                                     ↓
                               dataBuffer.Push(dataPoint)  (main goroutine)
                                                     ↓
                              graph.AppState.DataBuffer.Snapshot()
                                                     ↓
                                        Graph.Layout(gtx) renders
```

## Window lifecycle

1. `main()` loads config, creates `app.Window` with saved size (or 1000×500 default), sets `app.Decorated(false)`.
2. First `app.Win32ViewEvent` fires (HWND ready): `onPlatformEvent` strips `WS_MAXIMIZEBOX` so double-click on drag areas doesn't maximize.
3. `app.FrameEvent` fires on each render cycle: data is drained and the frame is drawn.
4. `app.DestroyEvent`: context cancelled, window dimensions saved to config JSON, process exits.

## Config persistence

On close, `FrameEvent.Size` (pixels) is converted to dp via `FrameEvent.Metric.PxToDp()` and written back to the JSON config as `windowWidthDp` / `windowHeightDp`. On next launch those values are used for `app.Size(...)`.

## Drag regions

`layout.go` registers `system.ActionInputOp(system.ActionMove)` for two non-overlapping rectangles before drawing any widgets:
- The full graph area (left side, full height)
- The stats panel from top down to just above the button row

Gio responds to Win32 `WM_NCHITTEST` with `HTCAPTION` for those rectangles, giving native OS drag behavior. The button row at the bottom of the stats panel is intentionally excluded — `ActionAt` never returns `ActionMove` there, so button clicks are unaffected.
