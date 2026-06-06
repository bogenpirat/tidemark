package ui

import (
	"ntg/internal/buffer"
	"ntg/internal/model"
)

// AppState holds all mutable UI state. It is owned by the main goroutine and
// must only be accessed from the Gio event loop.
type AppState struct {
	DataBuffer     *buffer.RingBuffer[model.DataPoint]
	CurrentTheme   *Theme
	HostLabel      string
	HistorySeconds int
	IsDarkTheme    bool
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
