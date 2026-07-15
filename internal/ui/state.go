package ui

import (
	"image"

	"tidemark/internal/buffer"
	"tidemark/internal/model"
)

// HostState holds the per-target mutable UI state: its rolling data buffer,
// display label, and the current plot width in pixels. Owned by the main
// goroutine and only accessed from the Gio event loop.
type HostState struct {
	DataBuffer   *buffer.RingBuffer[model.DataPoint]
	HostLabel    string
	GraphWidthPx int // plot area pixel width; updated each frame by layout
}

// AppState holds the shared, program-wide mutable UI state. It is owned by the
// main goroutine and must only be accessed from the Gio event loop.
type AppState struct {
	// Hosts holds one HostState per monitored target, rendered stacked top to
	// bottom in slice order.
	Hosts        []*HostState
	CurrentTheme *Theme
	IsDarkTheme  bool

	ContextMenuVisible bool
	ContextMenuPos     image.Point
	// ContextMenuHostIndex is the host row the context menu was opened over.
	ContextMenuHostIndex int

	// HoverPos is the mouse position in window coordinates, fed each frame
	// from the platform layer (Gio never sees moves over drag regions).
	// HoverValid is false when the mouse is outside the window.
	HoverPos   image.Point
	HoverValid bool

	ExitRequested     bool
	SettingsRequested bool
	// SettingsHostIndex is the host whose settings dialog should open.
	SettingsHostIndex int
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
