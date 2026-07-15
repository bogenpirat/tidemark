# Architecture

## What the program does

Reads one JSON config file, opens an undecorated borderless window, polls each configured host once per second — via SNMP v1/v2c or via SSH (reading `/sys/class/net/<iface>/statistics/{rx,tx}_bytes` on any Linux host) — and renders a live scrolling graph of download (red) and upload (green) throughput. A narrow stats panel on the right shows current/max/avg and a theme-toggle button. Right-clicking anywhere opens a context menu (Settings / Exit). Settings opens a second Gio window for editing the config. Escape exits immediately. Intended to run as 3 side-by-side instances for 3 hosts.

## Goroutines

```
main goroutine
  └── Gio event loop (window.Event())
        ├── app.FrameEvent  → drains pendingPoints, pushes to ring buffer, renders
        └── app.DestroyEvent → cancel context, save config, exit

polling goroutine, one per host (startHostService → go snmpService.Start OR go sshService.Start,
                                 selected by HostConfig.Protocol)
  └── polls every 1 second, sends model.DataPoint to the host's output channel (buffered, cap 10)

bridge goroutine (go func)
  └── reads the output channel, appends to pendingPoints slice, calls window.Invalidate()
```

**Concurrency rule**: `pendingPoints` is the only shared state. It is protected by `pendingMu` (sync.Mutex). The ring buffer (`DataBuffer`) is only accessed from the main goroutine (written in FrameEvent, read by Gio layout calls in the same goroutine). No other sharing.

## Data flow

```
SNMP host                                  Linux host (ssh)
  → gosnmp.Get (in polling goroutine)        → session.Output("cat …/rx_bytes …/tx_bytes")
  → counter.ComputeDelta (bytes/sec from raw counter diff)
  → model.DataPoint  →  output channel  →  pendingPoints  (bridge goroutine)
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

**Right-clicks in drag regions**: because these regions return `HTCAPTION`, right-clicks arrive as `WM_NCRBUTTONDOWN` (non-client), which Gio never surfaces as a pointer event. `platform_windows.go` subclasses the WndProc (`GWLP_WNDPROC`) to intercept `WM_NCRBUTTONDOWN`, converts screen→client coordinates, and stores the result for `TakeRightClick()` to consume on the next frame.

## Context menu

When `TakeRightClick()` returns a position, `main.go` sets `AppState.ContextMenuVisible = true` and `AppState.ContextMenuPos`. On the next `Layout` call, `layout.go` draws the menu (Settings / Exit items) above a full-window backdrop. Any click outside the menu items hits the backdrop and clears `ContextMenuVisible`. Clicks on items set `SettingsRequested` or `ExitRequested` on `AppState`.

## Settings dialog

When `AppState.SettingsRequested` is set, `main.go` launches a goroutine that calls `ui.RunSettingsDialog(...)`, which opens a second `app.Window` with its own event loop. When that window closes, the goroutine sends a `DialogResult` to `dialogResultChan` (buffered, cap 1) and calls `window.Invalidate()` on the main window. On the next `FrameEvent`, `main.go` drains the channel and applies the new config (updating `appState.HostLabel` and saving the file).

## Keyboard exit

`layout.go` registers a `key.Filter{Name: key.NameEscape}` input handler each frame. On Escape press it sets `AppState.ExitRequested = true`. `main.go` handles this in the `FrameEvent` branch: saves config and calls `os.Exit(0)`.
