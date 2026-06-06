# Codebase Reference

## Root package (`main`)

### `main.go`

Entry point. Owns the Gio event loop. Key responsibilities:
- Parse `os.Args[1]` as config file path
- Load config, create ring buffer, create AppState
- Launch SNMP goroutine and bridge goroutine
- Build font collection (gofont + Segoe UI Symbol for emoji fallback)
- Create window: `app.Size(...)` from saved config or defaults, `app.Decorated(false)`
- Event loop: `onPlatformEvent`, then switch on `DestroyEvent` / `FrameEvent`
- On `DestroyEvent`: save window dimensions to config via `config.SaveConfig`

Constants:
- `defaultWindowWidthDp = 1000`
- `defaultWindowHeightDp = 500`

### `platform_windows.go` (`//go:build windows`)

Called from `main.go`'s event loop on every event. On `app.Win32ViewEvent` with a valid HWND (fires once after window creation):
- Reads `GWL_STYLE` with `GetWindowLongPtrW` (64-bit) or `GetWindowLongW` (32-bit)
- Clears `WS_MAXIMIZEBOX` bit so double-clicking the caption area doesn't maximize

Constants: `gwlStyle = ^(uintptr(16) - 1)` (-16), `wsMaximizeBox = 0x00010000`

### `platform.go` (`//go:build !windows`)

No-op stub: `func onPlatformEvent(e event.Event) {}`

---

## `internal/config`

### `config.go`

**`AppConfig` struct** — all fields map 1:1 to JSON keys:
| Field | Type | JSON | Default |
|-------|------|------|---------|
| Host | string | `host` | required |
| Community | string | `community` | required |
| Port | uint16 | `port` | 161 |
| InterfaceIndex | int | `interfaceIndex` | 1 |
| DownloadOID | string | `downloadOID` | auto from ifIndex |
| UploadOID | string | `uploadOID` | auto from ifIndex |
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
func BuildOID(base string, index int) string          // appends ".N"
```

If `downloadOID`/`uploadOID` are missing from the config, `LoadConfig` fills them using `OIDIfHCInOctets`/`OIDIfHCOutOctets` + interface index. The config can override these to use `ifInOctets` / `ifOutOctets` (32-bit) OIDs instead.

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
    DataBuffer     *buffer.RingBuffer[model.DataPoint]
    CurrentTheme   *Theme
    HostLabel      string
    HistorySeconds int
    IsDarkTheme    bool
}
func (s *AppState) ToggleTheme()
```

`AppState` is created in `main.go` and passed by pointer to all widgets. Only accessed from the main goroutine.

### `layout.go`

`RootLayout` owns `Graph` and `StatsPanel` sub-widgets.

`Layout(gtx)`:
1. Fills the background.
2. Registers two `system.ActionInputOp(system.ActionMove)` clip regions (drag areas) — see Architecture.
3. Renders `StatsPanel` on the right (`statsPanelWidthDp = 150` dp).
4. Renders `Graph` on the remaining left area.

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
