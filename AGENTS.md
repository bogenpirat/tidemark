# Tidemark — Agent Guide

Tidemark is a Windows desktop app that polls hosts every second — via SNMP v1/v2c or via SSH (any Linux host, reading kernel interface counters) — and plots live upload/download throughput on a scrolling graph. It is designed to run as 3 simultaneous instances for 3 separate hosts.

## Quick facts

| Item | Value |
|------|-------|
| Language | Go 1.26 |
| GUI framework | Gio v0.10.0 (immediate-mode GPU, Direct3D on Windows) |
| SNMP library | github.com/gosnmp/gosnmp v1.43.2 |
| SSH library | golang.org/x/crypto/ssh |
| Entry point | `main.go` |
| Launch syntax | `tidemark.exe [-hostkey] <config.json>` (`-hostkey` prints ssh host key fingerprints and exits) |
| Build | `mage` (release) or `mage debug` — runs `go generate` then `go build`; see Building below |

## Building

Builds are orchestrated with [mage](https://magefile.org). Run `mage -l` to list targets.

| Command | Output |
|---------|--------|
| `mage` / `mage release` | `go generate ./...` then optimized windowless build (`-s -w -H windowsgui`) → `tidemark.exe` |
| `mage debug` | `go generate ./...` then unoptimized build with console attached → `tidemark.exe` |
| `mage generate` | regenerate `resource_windows.syso` only (icon + version metadata) |
| `mage clean` | remove `tidemark.exe` and `resource_windows.syso` |

First-time setup (one-off, installs the build tools into `$(go env GOPATH)/bin`):

```
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
go install github.com/magefile/mage@latest
```

The app icon and version info are embedded via a Windows resource: `go generate` runs
`goversioninfo`, which reads `versioninfo.json` (pointing at `tidemark.ico`) and emits
`resource_windows.syso`. The Go toolchain links any `*.syso` in the package dir into
Windows builds automatically, so the plain `go build` commands above need no extra flags.
The `.syso` is a build artifact (gitignored); source art lives in `assets/`.

## Detailed documentation

All detailed documentation lives in `.agents/`:

| File | Contents |
|------|----------|
| `.agents/architecture.md` | Data flow, goroutine model, event loop, concurrency rules |
| `.agents/codebase.md` | Every file and package: purpose, key functions, constants |
| `.agents/gio-guide.md` | How Gio works in this codebase: drawing, input routing, window options |
| `.agents/constraints.md` | Hard constraints, known gotchas, things that caused bugs, what NOT to do |

## Project layout

```
tidemark/
├── main.go                        # Entry point, event loop, polling-service wiring
├── platform_windows.go            # Win32-specific init (strips WS_MAXIMIZEBOX)
├── platform.go                    # No-op stub for non-Windows builds
├── magefile.go                    # Build targets (tagged //go:build mage)
├── resource_windows.go            # //go:generate goversioninfo directive
├── versioninfo.json               # Icon path + version metadata for the .exe
├── tidemark.ico                   # App icon embedded into the binary
├── assets/                        # Source artwork (SVGs)
├── go.mod / go.sum
├── empoknor.json                  # Example config (real test host)
└── internal/
    ├── config/config.go           # AppConfig/HostConfig structs, JSON load/save
    ├── model/datapoint.go         # DataPoint type
    ├── buffer/ringbuffer.go       # Generic fixed-capacity ring buffer
    ├── counter/counter.go         # Shared wrap-aware counter delta computation
    ├── snmp/
    │   └── service.go             # SNMP polling goroutine
    ├── sshpoll/
    │   └── service.go             # SSH polling goroutine (Linux hosts, sysfs counters)
    ├── units/units.go             # Byte-rate formatting and axis scaling
    └── ui/
        ├── theme.go               # Theme struct, DarkTheme, LightTheme
        ├── state.go               # AppState, ToggleTheme
        ├── layout.go              # RootLayout: splits window into graph + stats panel, registers drag regions
        ├── graph.go               # Graph widget: all chart drawing logic
        └── statspanel.go          # StatsPanel widget: current/max/avg + toggle button
```
