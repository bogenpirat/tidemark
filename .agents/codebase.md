# Codebase Reference

## Root package (`main`)

### `main.go`

Entry point. Owns the Gio event loop. Key responsibilities:
- Parse flags (`-hostkey`) and the config file path via the `flag` package
- `-hostkey` mode: `printHostKeys` fetches each ssh host's key fingerprint via `sshpoll.FetchHostKey` and exits (no window). Calls `AttachParentConsole()` first so output is visible from a terminal despite the windowsgui subsystem
- Load config, create ring buffer, create AppState
- Launch one polling goroutine per host via `startHostService` (dispatches on `HostConfig.Protocol`: `ssh` → `sshpoll.NewService`, otherwise `snmpservice.NewService`) plus a bridge goroutine
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

**`AttachParentConsole()`** — attaches the process to the parent's console (`AttachConsole(ATTACH_PARENT_PROCESS)`) and rebinds `os.Stdout`/`os.Stderr` to `CONOUT$`, but only when those handles are invalid — streams the shell already redirected to a pipe or file are left alone. Used by CLI modes (`-hostkey`) in the console-less windowsgui release build. Must run before `setupLogging` (the slog handler captures `os.Stdout` at creation).

**`atomicWin atomic.Pointer[app.Window]`** — stored on the first `Win32ViewEvent` so `customWndProc` (running on the Win32 thread) can call `win.Invalidate()`.

Constants: `gwlStyle` (-16), `gwlpWndProc` (-4), `wsMaximizeBox`, `wmNcRButtonDown` (0x00A4).

### `platform.go` (`//go:build !windows`)

No-op stubs:
- `func onPlatformEvent(win *app.Window, e event.Event) {}`
- `func TakeRightClick() (bool, image.Point) { return false, image.Point{} }`
- `func AttachParentConsole() {}`

---

## `internal/config`

### `config.go`

**`HostConfig` struct** (one per element of `AppConfig.Hosts`) — all fields map 1:1 to JSON keys. Which fields apply depends on `Protocol` (`snmp1`, `snmp2c`, or `ssh`; constants `config.ProtocolSNMP1/SNMP2c/SSH`, helper `IsSNMP()`):
| Field | Type | JSON | Default |
|-------|------|------|---------|
| Host | string | `host` | required |
| Protocol | string | `protocol` | `snmp2c` |
| Port | uint16 | `port` | 161 (SNMP) / 22 (SSH) |
| Community | string | `community` | required for SNMP |
| DownloadOID | string | `downloadOID` | `1.3.6.1.2.1.31.1.1.1.6.1` (SNMP only) |
| UploadOID | string | `uploadOID` | `1.3.6.1.2.1.31.1.1.1.10.1` (SNMP only) |
| Username | string | `username` | `root` (SSH only) |
| KeyFile | string | `keyFile` | required for SSH (private key path) |
| Interface | string | `interface` | required for SSH (remote NIC name) |
| HostKey | string | `hostKey` | empty = accept any (SSH only, SHA256 fingerprint) |
| TimeoutMs | int | `timeoutMs` | 3000 |
| Retries | int | `retries` | 1 |

**`AppConfig` struct** — window/theme fields:
| Field | Type | JSON | Default |
|-------|------|------|---------|
| WindowWidthDp | float32 | `windowWidthDp,omitempty` | 0 (uses default) |
| WindowHeightDp | float32 | `windowHeightDp,omitempty` | 0 (uses default) |
| WindowX | *int | `windowX,omitempty` | nil (OS places window) |
| WindowY | *int | `windowY,omitempty` | nil (OS places window) |
| DarkTheme | *bool | `darkTheme,omitempty` | nil (dark) |
| Hosts | []HostConfig | `hosts` | required |

Window geometry is runtime-managed: size (dp) and top-left position (`WindowX`/`WindowY`, physical screen px) are written back on exit and restored on launch. Position uses Win32 `GetWindowRect`/`SetWindowPos` (`platform_windows.go`) since Gio has no window-position API; `*int` distinguishes "never saved" (nil) from a valid `0` coordinate.

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

## `internal/counter`

### `counter.go`

`counter.ComputeDelta(prev, curr uint64) float64` — shared by both polling services. If `curr < prev`, assumes wrap and computes the delta modulo 2^32 (when both values fit in 32 bits) or 2^64. Logs a warning on wrap.

---

## `internal/snmp`

If `downloadOID`/`uploadOID` are missing from the config, `LoadConfig` defaults them to the interface-1 high-capacity (64-bit) counters: `1.3.6.1.2.1.31.1.1.1.6.1` (download) and `1.3.6.1.2.1.31.1.1.1.10.1` (upload). The config can override these to use `ifInOctets` / `ifOutOctets` (32-bit) OIDs, or any other interface, instead.

### `service.go`

**`SnmpService`** wraps `*config.HostConfig`. `Start(ctx, out chan<- model.DataPoint)` runs in a goroutine:
1. Opens a gosnmp session (v1 when `Protocol == "snmp1"`, else v2c), connects.
2. Runs a `time.NewTicker(time.Second)` loop.
3. First successful tick: saves baseline counters, emits nothing.
4. Subsequent ticks: computes `counter.ComputeDelta` (handles wrap), sends `DataPoint`.
5. On poll error: sends `DataPoint{IsError: true}` (unless no baseline yet).
6. Context cancel: clean stop.

`extractUint64(pdu)` — accepts `Counter64` (→ uint64), `Counter32`, `Gauge32` (→ uint cast to uint64). Returns error on unexpected type.

---

## `internal/sshpoll`

### `service.go`

**`SshService`** wraps `*config.HostConfig`. Polls any Linux host over SSH; mirrors the SNMP service's shape and error semantics. `Start(ctx, out chan<- model.DataPoint)`:
1. Loads/parses the private key from `KeyFile`, builds an `ssh.ClientConfig` (`golang.org/x/crypto/ssh`). Host key policy: if `HostKey` is set, the server's SHA256 fingerprint must match (compared with or without the `SHA256:` prefix); if empty, any key is accepted and the fingerprint is logged so the user can pin it.
2. Dials `host:port` once and keeps the client alive. Key-load or initial-dial failure: log error + return (same as SNMP connect failure).
3. 1-second ticker loop. Each tick runs `cat /sys/class/net/<iface>/statistics/rx_bytes .../tx_bytes` in a fresh session (a lightweight channel on the live connection, no re-handshake), enforcing `TimeoutMs` as a deadline and honoring `Retries` extra attempts (mirrors gosnmp retransmits).
4. Output parses as two uint64 lines: rx → download, tx → upload. Baseline/delta/error-DataPoint logic is identical to the SNMP service (`counter.ComputeDelta`).
5. Because SSH is connection-oriented, a failed poll closes the client and the next tick re-dials — this preserves SNMP-like "errors graph, then recovery" behavior. Reconnect failures after baseline also emit error DataPoints.
6. Top-talker feature (enabled by a valid `lanSubnet` CIDR): the poll command additionally dumps per-IP conntrack byte totals via awk over `/proc/net/nf_conntrack`; per-tick deltas pick the top downloading/uploading LAN IP. Immediately after each (re)connect, `fetchLeaseNames` reads the dnsmasq DHCP lease table (`/tmp/dhcp.leases`, OpenWrt default) into an ip → hostname map, and `talkerLabel` substitutes the hostname for the IP in the emitted DataPoint (falling back to the bare IP when no lease name exists). All of this is best-effort: failures only cost talker info, never the bandwidth sample.

**`FetchHostKey(*config.HostConfig) (string, error)`** — dials the host with no auth methods, captures the host key in the HostKeyCallback during key exchange, and returns its SHA256 fingerprint. The dial itself is expected to fail (auth) after the key is captured. Backs the `-hostkey` CLI mode.

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

**`RunSettingsDialog(mat, cfg, isDark) DialogResult`** — opens a second `app.Window` titled "Settings" (520×640 dp), runs its own Gio event loop, and blocks until the window is closed. Safe to call from any goroutine. Returns a `DialogResult{Saved bool, Config HostConfig}`.

**`settingsDialog`** — internal struct with `widget.Editor` fields for each `HostConfig` field (name, host, protocol, port, community, dlOID, ulOID, username, keyFile, iface, hostKey, timeoutMs, retries) and two `widget.Clickable` buttons (Save, Cancel). All rows are always visible regardless of protocol.

`Layout(gtx) dialogAction` — renders the form each frame; processes button clicks deferred from the previous frame; returns `dlgSave`, `dlgCancel`, or `dlgNone`.

`validate()` — parses all editor text and returns a populated `HostConfig` plus any error strings. Protocol must be `snmp1`/`snmp2c`/`ssh`. Community and OIDs are only validated for SNMP protocols (OIDs via `isValidOID`: dotted-numeric, ≥ 2 components); keyFile/interface are only required for ssh. Fields belonging to the protocol not in use are carried over unvalidated so switching protocols is non-destructive.

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
