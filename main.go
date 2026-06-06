package main

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"ntg/internal/buffer"
	"ntg/internal/config"
	"ntg/internal/model"
	snmpservice "ntg/internal/snmp"
	"ntg/internal/ui"

	"gioui.org/app"
	"gioui.org/font/gofont"
	gioopentype "gioui.org/font/opentype"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

const (
	defaultWindowWidthDp  = 1000
	defaultWindowHeightDp = 500
	maxBufferSeconds      = 7200 // ring buffer capacity; enough for any realistic screen width
)

func main() {
	setupLogging()

	if len(os.Args) < 2 {
		slog.Error("no config file specified", "usage", "ntg.exe <config.json>")
		os.Exit(1)
	}
	configFilePath := os.Args[1]

	slog.Info("loading configuration", "path", configFilePath)
	appConfig, loadError := config.LoadConfig(configFilePath)
	if loadError != nil {
		slog.Error("failed to load configuration", "err", loadError)
		os.Exit(1)
	}
	slog.Info("configuration loaded",
		"host", appConfig.Host,
		"port", appConfig.Port,
		"interfaceIndex", appConfig.InterfaceIndex,
		"timeoutMs", appConfig.TimeoutMs,
	)

	dataBuffer := buffer.New[model.DataPoint](maxBufferSeconds)
	appState := &ui.AppState{
		DataBuffer:   dataBuffer,
		CurrentTheme: &ui.DarkTheme,
		HostLabel:    appConfig.Host,
		IsDarkTheme:  true,
	}

	ctx, cancelContext := context.WithCancel(context.Background())
	snmpOutputChannel := make(chan model.DataPoint, 10)

	snmpService := snmpservice.NewService(appConfig)
	go snmpService.Start(ctx, snmpOutputChannel)

	initialWidthDp := appConfig.WindowWidthDp
	initialHeightDp := appConfig.WindowHeightDp
	if initialWidthDp <= 0 {
		initialWidthDp = defaultWindowWidthDp
	}
	if initialHeightDp <= 0 {
		initialHeightDp = defaultWindowHeightDp
	}

	windowTitle := "NTG — " + appConfig.Host
	window := new(app.Window)
	window.Option(
		app.Title(windowTitle),
		app.Size(unit.Dp(initialWidthDp), unit.Dp(initialHeightDp)),
		app.Decorated(false),
	)

	var pendingMu sync.Mutex
	var pendingPoints []model.DataPoint

	go func() {
		for dataPoint := range snmpOutputChannel {
			pendingMu.Lock()
			pendingPoints = append(pendingPoints, dataPoint)
			pendingMu.Unlock()
			window.Invalidate()
		}
	}()

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

	var ops op.Ops
	for {
		windowEvent := window.Event()
		onPlatformEvent(windowEvent)

		switch typedEvent := windowEvent.(type) {
		case app.DestroyEvent:
			slog.Info("window closed, shutting down")
			cancelContext()
			if hasFrame {
				appConfig.WindowWidthDp = float32(lastFrameEvent.Metric.PxToDp(lastFrameEvent.Size.X))
				appConfig.WindowHeightDp = float32(lastFrameEvent.Metric.PxToDp(lastFrameEvent.Size.Y))
				if saveErr := config.SaveConfig(configFilePath, appConfig); saveErr != nil {
					slog.Error("failed to save config", "err", saveErr)
				}
			}
			if typedEvent.Err != nil {
				slog.Error("window destroyed with error", "err", typedEvent.Err)
				os.Exit(1)
			}
			os.Exit(0)

		case app.FrameEvent:
			lastFrameEvent = typedEvent
			hasFrame = true
			pendingMu.Lock()
			incomingPoints := pendingPoints
			pendingPoints = nil
			pendingMu.Unlock()

			for _, dataPoint := range incomingPoints {
				dataBuffer.Push(dataPoint)
				slog.Debug("data point pushed to buffer",
					"timestampMs", dataPoint.TimestampMs,
					"isError", dataPoint.IsError)
			}

			gtx := app.NewContext(&ops, typedEvent)
			rootLayout.Layout(gtx)
			typedEvent.Frame(&ops)
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
