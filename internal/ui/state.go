package ui

import (
	"image"

	"ntg/internal/buffer"
	"ntg/internal/model"
)

// AppState holds all mutable UI state. It is owned by the main goroutine and
// must only be accessed from the Gio event loop.
type AppState struct {
	DataBuffer         *buffer.RingBuffer[model.DataPoint]
	CurrentTheme       *Theme
	HostLabel          string
	IsDarkTheme        bool
	GraphWidthPx       int // plot area pixel width; updated each frame by layout
	ContextMenuVisible bool
	ContextMenuPos     image.Point
	ExitRequested      bool
}

// ToggleTheme switches between DarkTheme and LightTheme.
func (appState *AppState) ToggleTheme() {
	appState.IsDarkTheme = !appState.IsDarkTheme
	if appState.IsDarkTheme {
		appState.CurrentTheme = &DarkTheme
	} else {
		appState.CurrentTheme = &LightTheme
	}
}
