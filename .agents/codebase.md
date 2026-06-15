# Codebase Reference

## Root package (`main`)

### `main.go`

Entry point. Owns the Gio event loop. Key responsibilities:
- Parse `os.Args[1]` as config file path
- Load config, create ring buffer, create AppState
- Launch SNMP goroutine and bridge goroutine
- Build font collection (gofont + Segoe UI Symbol for emoji fallback)
- Create window: `app.Size(...)` from saved config or defaults, `app.Decorated(false)`
- Event loop: `onPlatformEvent(window, event)`, then switch on `DestroyEvent` / `FrameEvent`
- On `DestroyEvent`: save window dimensions to config via `config.SaveConfig`
- On `FrameEvent`: drain `dialogResultChan` (apply saved config if settings dialog closed), poll `TakeRightClick()` to show context menu, call `rootLayout.Layout`, then handle `appState.SettingsRequested` (launch dialog goroutine) and `appState.ExitRequested` (save config and `os.Exit(0)`)

Settings dialog integration:
- `dialogResultChan chan ui.DialogResult` (buffered, cap 1) — the dialog goroutine sends its result here when its window closes
- `dialogOpen bool` — prevents opening a second dialog while one is already running
- Dialog goroutine: calls `ui.RunSettingsDialog(matTheme, cfg, isDark)`, sends result to `dialogResultChan`, calls `window.Invalidate()`

Constants:
- `defaultWindowWidthDp = 1000`
- `defaultWindowHeightDp = 500`

### `platform_windows.go` (`//go:build windows`)

Called from `main.go`'s event loop on every event. On `app.Win32ViewEvent` with a valid HWND (fires once after window creation), spawns a goroutine that:
1. Reads `GWL_STYLE` and clears `WS_MAXIMIZEBOX` so double-clicking the caption area doesn't maximize.
2. Calls `installOnce.Do` to subclass the WndProc (see below).

**WndProc subclassing** — `ActionMove` regions return `HTCAPTION` from `WM_NCHITTEST`, so right-clicks in those regions arrive as `WM_NCRBUTTONDOWN` (non-client), which Gio never routes as a pointer event. `customWndProc` intercepts `WM_NCRBUTTONDOWN`, converts screen→client coordinates via `ScreenToClient`, stores the result in `rightClickPos` (guarded by `rightClickMu`), invalidates the window, and returns 0 to suppress Win32's default system-menu. All other messages are forwarded to the original WndProc via `CallWindowProcW`.

**`TakeRightClick() (bool, image.Point)`** — called from the main goroutine before each `Layout` call. Returns and clears any pending right-click position.

**`atomicWin atomic.Pointer[app.Window]`** — stored on the first `Win32ViewEvent` so `customWndProc` (running on the Win32 thread) can call `win.Invalidate()`.

Constants: `gwlStyle` (-16), `gwlpWndProc` (-4), `wsMaximizeBox`, `wmNcRButtonDown` (0x00A4).

### `platform.go` (`//go:build !windows`)

No-op stubs:
- `func onPlatformEvent(win *app.Window, e event.Event) {}`
- `func TakeRightClick() (bool, image.Point) { return false, image.Point{} }`

---

## `internal/config`

### `config.go`

**`AppConfig` struct** — all fields map 1:1 to JSON keys:
| Field | Type | JSON | Default |
|-------|------|------|---------|
| Host | string | `host` | required |
| Community | string | `community` | required |
| Port | uint16 | `port` | 161 |
| DownloadOID | string | `downloadOID` | `1.3.6.1.2.1.31.1.1.1.6.1` |
| UploadOID | string | `uploadOID` | `1.3.6.1.2.1.31.1.1.1.10.1` |
| TimeoutMs | int | `timeoutMs` | 3000 |
| Retries | int | `retries` | 1 |
| HistorySeconds | int | `historySeconds` | 600 (max 3600) |
| WindowWidthDp | float32 | `windowWidthDp,omitempty` | 0 (uses default) |
| WindowHeightDp | float32 | `windowHeightDp,omitempty` | 0 (uses default) |

**`LoadConfig(filePath string) (*AppConfig, error)`** — reads file, unmarshals, applies defaults, validates required fields.

**`SaveConfig(filePath string, cfg *AppConfig) error`** — marshals with `json.MarshalIndent` (tab indent), writes file. Called on window close.

---

## `internal/model`

### `datapoint.go`

```go
type DataPoint struct {
    TimestampMs         int64
    DownloadBytesPerSec float64
    UploadBytesPerSec   float64
    IsError             bool
    ErrorMessage        string
}
```

`IsError = true` means a poll failed; the `BytesPerSec` fields are zero. Error data points render as purple vertical bars in the graph.

---

## `internal/buffer`

### `ringbuffer.go`

Generic fixed-capacity circular buffer. NOT goroutine-safe — callers must synchronize. Methods:
- `New[T any](capacity int) *RingBuffer[T]`
- `Push(item T)` — overwrites oldest when full
- `Snapshot() []T` — returns copy, oldest-first
- `Len() int`

Capacity is set to `AppConfig.HistorySeconds` (default 600 entries = 10 minutes at 1 poll/sec).

---

## `internal/snmp`

### `oids.go`

```go
const OIDIfHCInOctets  = "1.3.6.1.2.1.31.1.1.1.6"   // download (64-bit)
const OIDIfHCOutOctets = "1.3.6.1.2.1.31.1.1.1.10"  // upload (64-bit)
```

If `downloadOID`/`uploadOID` are missing from the config, `LoadConfig` defaults them to the interface-1 high-capacity counters (`OIDIfHCInOctets`/`OIDIfHCOutOctets` + `.1`). The config can override these to use `ifInOctets` / `ifOutOctets` (32-bit) OIDs, or any other interface, instead.

### `service.go`

**`SnmpService`** wraps `*config.AppConfig`. `Start(ctx, out chan<- model.DataPoint)` runs in a goroutine:
1. Opens a gosnmp v2c session, connects.
2. Runs a `time.NewTicker(time.Second)` loop.
3. First successful tick: saves baseline counters, emits nothing.
4. Subsequent ticks: computes `computeCounterDelta` (handles 64-bit wrap), sends `DataPoint`.
5. On poll error: sends `DataPoint{IsError: true}` (unless no baseline yet).
6. Context cancel: clean stop.

`computeCounterDelta(prev, curr uint64) float64` — if `curr < prev`, assumes wrap and computes `(MaxUint64 - prev) + curr + 1`. Logs a warning on wrap.

`extractUint64(pdu)` — accepts `Counter64` (→ uint64), `Counter32`, `Gauge32` (→ uint cast to uint64). Returns error on unexpected type.

---

## `internal/units`

### `units.go`

| Function | Description |
|----------|-------------|
| `GetScaleUnit(maxBps float64) ScaleUnit` | Picks KiB/MiB/GiB/TiB divisor for Y axis |
| `FormatBytesPerSec(bps float64) string` | `"12.34 MiB/s"` auto-unit |
| `FormatBytesPerSecInUnit(bps, unit) string` | Fixed unit, no unit label (for stats panel rows) |
| `NiceAxisMax(maxInUnit float64) (niceMax, step float64)` | Rounds up to next clean interval, targets 4–6 notches |

---

## `internal/ui`

All UI code shares the `ui` package. Gio is immediate-mode: every `FrameEvent` re-renders everything from scratch.

### `theme.go`

`Theme` struct with 14 `color.NRGBA` fields. Two package-level vars: `DarkTheme`, `LightTheme`.

Fields: `Background`, `GraphBackground`, `DownloadFill`, `UploadFill`, `OverlapFill`, `ErrorBar`, `AxisText`, `GridLine`, `PanelText`, `PanelBackground`, `BorderColor`, `ButtonText`, `ButtonFace`, `DownloadLabel`, `UploadLabel`.

### `state.go`

```go
type AppState struct {
    DataBuffer         *buffer.RingBuffer[model.DataPoint]
    CurrentTheme       *Theme
    HostLabel          string
    IsDarkTheme        bool
    GraphWidthPx       int        // plot area pixel width; updated each frame by layout
    ContextMenuVisible bool
    ContextMenuPos     image.Point
    ExitRequested      bool
    SettingsRequested  bool
}
func (s *AppState) ToggleTheme()
```

`AppState` is created in `main.go` and passed by pointer to all widgets. Only accessed from the main goroutine.

### `layout.go`

`RootLayout` owns `Graph` and `StatsPanel` sub-widgets, plus three internal fields used for input handling:
- `backdropTag struct{}` — event tag for the full-window backdrop that dismisses the context menu
- `settingsItem widget.Clickable` — context menu "Settings" item
- `exitItem widget.Clickable` — context menu "Exit" item

`Layout(gtx)`:
1. Drains deferred input events from the previous frame:
   - `pointer.Filter{Target: &backdropTag}` — any press outside the menu dismisses it
   - `settingsItem.Clicked` → sets `AppState.SettingsRequested = true`
   - `exitItem.Clicked` → sets `AppState.ExitRequested = true`
   - `key.Filter{Name: key.NameEscape}` — Escape key sets `AppState.ExitRequested = true`
2. Fills the background.
3. Registers two `system.ActionInputOp(system.ActionMove)` drag regions — see Architecture.
4. Renders `StatsPanel` on the right (`statsPanelWidthDp = 150` dp).
5. Renders `Graph` on the remaining left area.
6. If `AppState.ContextMenuVisible`: registers the full-window backdrop (highest z-order, dismisses menu on click), then draws the context menu via `drawContextMenu`.

The drag-region bottom boundary uses constants from `statspanel.go` directly (same package):
```go
buttonRowTop := totalHeight - gtx.Dp(toggleButtonHeightDp) - gtx.Dp(12)
```

### `graph.go`

`Graph` widget. `Layout(gtx)`:
1. Takes `DataBuffer.Snapshot()`.
2. Computes Y scale: `GetScaleUnit` → `NiceAxisMax`.
3. Draws graph background, Y axis (gridlines + labels), X axis (time markers).
4. Calls `buildPixelPoints` to project data to pixel coordinates.
5. Calls `drawDataAreas`: draws download (red), upload (green), overlap (yellow) filled polygons, then error bars (purple 1px verticals).
6. Draws host label at top-center.
7. Draws 4-sided border around plot area.

Plot geometry constants (in dp):
- `yAxisLabelWidthDp = 72` — left margin for Y labels
- `xAxisLabelHeightDp = 20` — bottom margin for X labels
- `topPaddingDp = 22` — top margin for host label
- `rightPaddingDp = 8`
- `tickLengthDp = 4`

X-axis label spacing: `chooseXAxisInterval` selects the smallest interval from `niceMinuteIntervals` such that adjacent labels are at least `minXLabelSpacingPx = 36` pixels apart.

Drawing helpers (all in `graph.go`):
- `fillRect(ops, color, rect)` — solid rectangle
- `drawHLine` / `drawVLine` — 1px lines
- `drawPositionedLabel` — clips a `material.Label` to a rect at absolute position
- `drawPolygonRun` — fills a closed polygon using `clip.Path`

### `contextmenu.go`

`drawContextMenu(gtx, theme, matTheme, settingsItem, exitItem, pos image.Point)` — renders a two-item right-click menu (Settings, Exit) at the given client position. Clamps the menu rect to stay within the window bounds. Draws a bordered box using `PanelBackground`/`BorderColor`, then calls `drawMenuItem` for each entry.

`drawMenuItem` — renders one menu item as a `widget.Clickable` with hover highlight (`ButtonFace`) and a left-padded label.

Constants: `menuWidthDp = 120`, `menuItemHeightDp = 24`, `menuPaddingXDp = 8`.

### `dialog.go`

**`RunSettingsDialog(mat, cfg, isDark) DialogResult`** — opens a second `app.Window` titled "Settings" (520×460 dp), runs its own Gio event loop, and blocks until the window is closed. Safe to call from any goroutine. Returns a `DialogResult{Saved bool, Config AppConfig}`.

**`settingsDialog`** — internal struct with `widget.Editor` fields for each `AppConfig` field (host, community, port, snmpVersion, dlOID, ulOID, timeoutMs, retries) and two `widget.Clickable` buttons (Save, Cancel).

`Layout(gtx) dialogAction` — renders the form each frame; processes button clicks deferred from the previous frame; returns `dlgSave`, `dlgCancel`, or `dlgNone`.

`validate()` — parses all editor text and returns a populated `AppConfig` plus any error strings. OIDs are validated by `isValidOID` (dotted-numeric, ≥ 2 components).

When Save is clicked: validates, shows errors inline if invalid, otherwise sets `d.closing = true`, populates `result`, and calls `win.Perform(system.ActionClose)`.

Constants (all in dp): `dlgLabelWidthDp=140`, `dlgFieldHeightDp=26`, `dlgFieldPadDp=4`, `dlgRowGapDp=7`, `dlgOuterPadDp=16`, `dlgBtnHeightDp=30`, `dlgBtnWidthDp=88`, `dlgBtnGapDp=8`.

### `statspanel.go`

`StatsPanel` widget. `Layout(gtx)`:
1. Processes theme toggle clicks (`ThemeButton.Clicked`).
2. Calls `computeStats(snapshot)` → `computedStats` (current/max/avg for download and upload).
3. Renders two stat sections ("Download ▼", "Upload ▲") with `renderStatSection`.
4. Renders the theme toggle button via `drawThemeToggleButton`.

Constants:
- `statsPanelWidthDp = 150`
- `toggleButtonHeightDp = 28`
- `toggleButtonWidthDp = 40`

The toggle button shows `"💡"` (rendered using Segoe UI Symbol font loaded in `main.go`). It is 40dp wide, centered in the panel, pinned to the bottom. The `innerPadding` is `gtx.Dp(12)` (hard-coded inside `Layout`).

`computeStats` iterates oldest-first; the LAST non-error point becomes "current". Points with `IsError = true` are skipped for all stat calculations.
