package main

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"tidemark/internal/buffer"
	"tidemark/internal/config"
	"tidemark/internal/model"
	snmpservice "tidemark/internal/snmp"
	"tidemark/internal/ui"

	"gioui.org/app"
	"gioui.org/font/gofont"
	gioopentype "gioui.org/font/opentype"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

const (
	defaultWindowWidthDp = 1000
	defaultRowHeightDp   = 250 // default per-host band height when none is saved
	maxBufferSeconds     = 7200 // ring buffer capacity; enough for any realistic screen width
)

// hostRuntime bundles the per-host data buffer, UI state, and SNMP plumbing.
// Each host's polling goroutine has its own cancel func so a single host can be
// restarted (after a settings edit) without disturbing the others.
type hostRuntime struct {
	state   *ui.HostState
	buffer  *buffer.RingBuffer[model.DataPoint]
	outChan chan model.DataPoint
	cancel  context.CancelFunc

	pendingMu     sync.Mutex
	pendingPoints []model.DataPoint
}

func main() {
	setupLogging()

	if len(os.Args) < 2 {
		slog.Error("no config file specified", "usage", "tidemark.exe <config.json>")
		os.Exit(1)
	}
	configFilePath := os.Args[1]

	slog.Info("loading configuration", "path", configFilePath)
	appConfig, loadError := config.LoadConfig(configFilePath)
	if loadError != nil {
		slog.Error("failed to load configuration", "err", loadError)
		os.Exit(1)
	}
	slog.Info("configuration loaded", "hosts", len(appConfig.Hosts))

	isDarkTheme := true
	if appConfig.DarkTheme != nil {
		isDarkTheme = *appConfig.DarkTheme
	}
	currentTheme := &ui.DarkTheme
	if !isDarkTheme {
		currentTheme = &ui.LightTheme
	}

	appState := &ui.AppState{
		CurrentTheme: currentTheme,
		IsDarkTheme:  isDarkTheme,
	}

	window := new(app.Window)

	// Build a runtime for each configured host: buffer, UI state, an SNMP
	// output channel, a bridge goroutine, and an initial polling goroutine.
	runtimes := make([]*hostRuntime, len(appConfig.Hosts))
	for hostIndex := range appConfig.Hosts {
		dataBuffer := buffer.New[model.DataPoint](maxBufferSeconds)
		runtime := &hostRuntime{
			state:   &ui.HostState{DataBuffer: dataBuffer, HostLabel: appConfig.Hosts[hostIndex].DisplayName()},
			buffer:  dataBuffer,
			outChan: make(chan model.DataPoint, 10),
		}
		runtimes[hostIndex] = runtime
		appState.Hosts = append(appState.Hosts, runtime.state)

		// Bridge: SNMP output → pending slice → repaint. The channel is shared
		// across service restarts, so it is never closed.
		go func(runtime *hostRuntime) {
			for dataPoint := range runtime.outChan {
				runtime.pendingMu.Lock()
				runtime.pendingPoints = append(runtime.pendingPoints, dataPoint)
				runtime.pendingMu.Unlock()
				window.Invalidate()
			}
		}(runtime)

		ctx, cancel := context.WithCancel(context.Background())
		runtime.cancel = cancel
		go snmpservice.NewService(&appConfig.Hosts[hostIndex]).Start(ctx, runtime.outChan)
	}

	initialWidthDp := appConfig.WindowWidthDp
	initialHeightDp := appConfig.WindowHeightDp
	if initialWidthDp <= 0 {
		initialWidthDp = defaultWindowWidthDp
	}
	if initialHeightDp <= 0 {
		initialHeightDp = float32(defaultRowHeightDp * len(appConfig.Hosts))
	}

	window.Option(
		app.Title("Tidemark"),
		app.Size(unit.Dp(initialWidthDp), unit.Dp(initialHeightDp)),
		app.Decorated(false),
	)

	// Restore the last saved window position (applied once the native handle exists).
	if appConfig.WindowX != nil && appConfig.WindowY != nil {
		SetInitialWindowPos(*appConfig.WindowX, *appConfig.WindowY)
	}

	fontCollection := gofont.Collection()
	if symData, err := os.ReadFile(`C:\Windows\Fonts\seguisym.ttf`); err == nil {
		if symFaces, err := gioopentype.ParseCollection(symData); err == nil {
			fontCollection = append(fontCollection, symFaces...)
		}
	}

	matTheme := material.NewTheme()
	matTheme.Shaper = text.NewShaper(text.WithCollection(fontCollection))
	rootLayout := ui.NewRootLayout(appState, matTheme)

	var lastFrameEvent app.FrameEvent
	hasFrame := false

	// Last known top-left screen position, refreshed each frame so it survives a
	// DestroyEvent (where the native handle may already be gone).
	var lastPosX, lastPosY int
	var hasPos bool

	persistConfig := func() {
		if hasFrame {
			appConfig.WindowWidthDp = float32(lastFrameEvent.Metric.PxToDp(lastFrameEvent.Size.X))
			appConfig.WindowHeightDp = float32(lastFrameEvent.Metric.PxToDp(lastFrameEvent.Size.Y))
		}
		if hasPos {
			appConfig.WindowX = &lastPosX
			appConfig.WindowY = &lastPosY
		}
		darkTheme := appState.IsDarkTheme
		appConfig.DarkTheme = &darkTheme
		if saveErr := config.SaveConfig(configFilePath, appConfig); saveErr != nil {
			slog.Error("failed to save config", "err", saveErr)
		}
	}

	stopAllHosts := func() {
		for _, runtime := range runtimes {
			if runtime.cancel != nil {
				runtime.cancel()
			}
		}
	}

	dialogResultChan := make(chan ui.DialogResult, 1)
	var dialogOpen bool
	var dialogHostIndex int

	var ops op.Ops
	for {
		windowEvent := window.Event()
		onPlatformEvent(window, windowEvent)

		switch typedEvent := windowEvent.(type) {
		case app.DestroyEvent:
			slog.Info("window closed, shutting down")
			stopAllHosts()
			persistConfig()
			if typedEvent.Err != nil {
				slog.Error("window destroyed with error", "err", typedEvent.Err)
				os.Exit(1)
			}
			os.Exit(0)

		case app.FrameEvent:
			lastFrameEvent = typedEvent
			hasFrame = true
			if x, y, ok := GetWindowPosition(); ok {
				lastPosX, lastPosY, hasPos = x, y, true
			}

			// Apply any saved config from a closed settings dialog.
			select {
			case result := <-dialogResultChan:
				dialogOpen = false
				if result.Saved && dialogHostIndex >= 0 && dialogHostIndex < len(runtimes) {
					appConfig.Hosts[dialogHostIndex] = result.Config
					runtime := runtimes[dialogHostIndex]
					runtime.state.HostLabel = result.Config.DisplayName()
					if saveErr := config.SaveConfig(configFilePath, appConfig); saveErr != nil {
						slog.Error("failed to save config", "err", saveErr)
					}
					// Restart this host's SNMP service with the new settings and a
					// fresh buffer; other hosts keep running untouched.
					runtime.cancel()
					ctx, cancel := context.WithCancel(context.Background())
					runtime.cancel = cancel
					newBuffer := buffer.New[model.DataPoint](maxBufferSeconds)
					runtime.buffer = newBuffer
					runtime.state.DataBuffer = newBuffer
					go snmpservice.NewService(&appConfig.Hosts[dialogHostIndex]).Start(ctx, runtime.outChan)
				}
			default:
			}

			// Drain each host's pending points into its buffer.
			for _, runtime := range runtimes {
				runtime.pendingMu.Lock()
				incomingPoints := runtime.pendingPoints
				runtime.pendingPoints = nil
				runtime.pendingMu.Unlock()
				for _, dataPoint := range incomingPoints {
					runtime.buffer.Push(dataPoint)
				}
			}

			if ok, pos := TakeRightClick(); ok {
				appState.ContextMenuVisible = true
				appState.ContextMenuPos = pos
			}

			gtx := app.NewContext(&ops, typedEvent)
			rootLayout.Layout(gtx)
			typedEvent.Frame(&ops)

			if appState.SettingsRequested && !dialogOpen {
				appState.SettingsRequested = false
				dialogOpen = true
				dialogHostIndex = appState.SettingsHostIndex
				hostCfg := appConfig.Hosts[dialogHostIndex]
				isDark := appState.IsDarkTheme
				go func() {
					result := ui.RunSettingsDialog(matTheme, hostCfg, isDark)
					dialogResultChan <- result
					window.Invalidate()
				}()
			}

			if appState.ExitRequested {
				slog.Info("exit via context menu, shutting down")
				stopAllHosts()
				persistConfig()
				os.Exit(0)
			}
		}
	}
}

func setupLogging() {
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(handler))
}
