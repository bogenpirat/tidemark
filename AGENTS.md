# NTG — Agent Guide

NTG is a Windows desktop app that polls an SNMP v2c host every second and plots live upload/download throughput on a scrolling graph. It is designed to run as 3 simultaneous instances for 3 separate hosts.

## Quick facts

| Item | Value |
|------|-------|
| Language | Go 1.26 |
| GUI framework | Gio v0.10.0 (immediate-mode GPU, Direct3D on Windows) |
| SNMP library | github.com/gosnmp/gosnmp v1.43.2 |
| Entry point | `main.go` |
| Launch syntax | `ntg.exe <config.json>` |
| Build | `go build -o ntg.exe .` |

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
ntg/
├── main.go                        # Entry point, event loop, SNMP wiring
├── platform_windows.go            # Win32-specific init (strips WS_MAXIMIZEBOX)
├── platform.go                    # No-op stub for non-Windows builds
├── go.mod / go.sum
├── empoknor.json                  # Example config (real test host)
└── internal/
    ├── config/config.go           # AppConfig struct, JSON load/save
    ├── model/datapoint.go         # DataPoint type
    ├── buffer/ringbuffer.go       # Generic fixed-capacity ring buffer
    ├── snmp/
    │   └── service.go             # SNMP polling goroutine
    ├── units/units.go             # Byte-rate formatting and axis scaling
    └── ui/
        ├── theme.go               # Theme struct, DarkTheme, LightTheme
        ├── state.go               # AppState, ToggleTheme
        ├── layout.go              # RootLayout: splits window into graph + stats panel, registers drag regions
        ├── graph.go               # Graph widget: all chart drawing logic
        └── statspanel.go          # StatsPanel widget: current/max/avg + toggle button
```
