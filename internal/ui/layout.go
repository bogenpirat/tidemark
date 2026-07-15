package ui

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type RootLayout struct {
	AppState     *AppState
	MatTheme     *material.Theme
	Graph        *Graph
	StatsPanel   *StatsPanel
	ThemeButton  widget.Clickable
	backdropTag  struct{}
	settingsItem widget.Clickable
	exitItem     widget.Clickable
}

func NewRootLayout(appState *AppState, matTheme *material.Theme) *RootLayout {
	return &RootLayout{
		AppState:   appState,
		MatTheme:   matTheme,
		Graph:      &Graph{AppState: appState, MatTheme: matTheme},
		StatsPanel: &StatsPanel{AppState: appState, MatTheme: matTheme},
	}
}

func (rootLayout *RootLayout) Layout(gtx layout.Context) layout.Dimensions {
	appState := rootLayout.AppState

	// Process events deferred from previous frame's handlers.
	for {
		ev, ok := gtx.Source.Event(pointer.Filter{Target: &rootLayout.backdropTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := ev.(pointer.Event); ok {
			appState.ContextMenuVisible = false
		}
	}
	for rootLayout.settingsItem.Clicked(gtx) {
		appState.ContextMenuVisible = false
		appState.SettingsRequested = true
		appState.SettingsHostIndex = appState.ContextMenuHostIndex
	}
	for rootLayout.exitItem.Clicked(gtx) {
		appState.ContextMenuVisible = false
		appState.ExitRequested = true
	}
	for rootLayout.ThemeButton.Clicked(gtx) {
		appState.ToggleTheme()
	}
	for {
		ev, ok := gtx.Source.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			appState.ExitRequested = true
		}
	}

	currentTheme := appState.CurrentTheme
	totalWidth := gtx.Constraints.Max.X
	totalHeight := gtx.Constraints.Max.Y
	fillRect(gtx.Ops, currentTheme.Background, image.Rect(0, 0, totalWidth, totalHeight))
	if totalWidth <= 0 || totalHeight <= 0 {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	numHosts := len(appState.Hosts)
	if numHosts == 0 {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	statsPanelWidth := gtx.Dp(statsPanelWidthDp)
	statsLeft := totalWidth - statsPanelWidth
	graphWidth := statsLeft

	// Each host gets an equal-height horizontal band, stacked top to bottom.
	rowHeight := totalHeight / numHosts
	if rowHeight <= 0 {
		rowHeight = totalHeight
	}

	// Map a pending right-click to the host row it landed in, so the context
	// menu's Settings entry edits that host.
	if appState.ContextMenuVisible {
		hostIndex := appState.ContextMenuPos.Y / rowHeight
		if hostIndex < 0 {
			hostIndex = 0
		}
		if hostIndex >= numHosts {
			hostIndex = numHosts - 1
		}
		appState.ContextMenuHostIndex = hostIndex
	}

	// The single theme toggle lives at the window's bottom-right; the drag
	// region of the last row stops above it so it stays clickable (see
	// .agents/constraints.md #2).
	buttonRowTop := totalHeight - gtx.Dp(toggleButtonHeightDp) - gtx.Dp(12)

	// Compute the plot area width once; it is identical for every row.
	plotWidth := graphWidth - gtx.Dp(yAxisLabelWidthDp) - gtx.Dp(rightPaddingDp)

	for hostIndex, host := range appState.Hosts {
		rowTop := hostIndex * rowHeight
		rowH := rowHeight
		if hostIndex == numHosts-1 {
			rowH = totalHeight - rowTop // last row absorbs the rounding remainder
		}
		if rowH <= 0 {
			continue
		}

		if plotWidth > 0 && plotWidth != host.GraphWidthPx {
			host.GraphWidthPx = plotWidth
		}

		// Register drag regions for this row. The graph area covers the full row
		// height; the stats area covers down to the row bottom, except in the
		// last row where it stops above the shared toggle button.
		if graphWidth > 0 {
			stack := clip.Rect(image.Rect(0, rowTop, graphWidth, rowTop+rowH)).Push(gtx.Ops)
			system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
			stack.Pop()
		}
		statsDragBottom := rowTop + rowH
		if hostIndex == numHosts-1 && statsDragBottom > buttonRowTop {
			statsDragBottom = buttonRowTop
		}
		if statsLeft > 0 && statsDragBottom > rowTop {
			stack := clip.Rect(image.Rect(statsLeft, rowTop, totalWidth, statsDragBottom)).Push(gtx.Ops)
			system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
			stack.Pop()
		}

		if statsLeft > 0 {
			statsOffsetStack := op.Offset(image.Pt(statsLeft, rowTop)).Push(gtx.Ops)
			statsClipStack := clip.Rect(image.Rect(0, 0, statsPanelWidth, rowH)).Push(gtx.Ops)
			statsGtx := gtx
			statsGtx.Constraints = layout.Exact(image.Pt(statsPanelWidth, rowH))
			rootLayout.StatsPanel.Layout(statsGtx, host)
			statsClipStack.Pop()
			statsOffsetStack.Pop()
		}
		if graphWidth > 0 {
			// Hover position relative to this row's graph origin; only valid when
			// the mouse is inside this row's graph area and no menu is open.
			hoverPos := image.Pt(appState.HoverPos.X, appState.HoverPos.Y-rowTop)
			hoverValid := appState.HoverValid && !appState.ContextMenuVisible &&
				appState.HoverPos.X >= 0 && appState.HoverPos.X < graphWidth &&
				appState.HoverPos.Y >= rowTop && appState.HoverPos.Y < rowTop+rowH

			graphOffsetStack := op.Offset(image.Pt(0, rowTop)).Push(gtx.Ops)
			graphClipStack := clip.Rect(image.Rect(0, 0, graphWidth, rowH)).Push(gtx.Ops)
			graphGtx := gtx
			graphGtx.Constraints = layout.Exact(image.Pt(graphWidth, rowH))
			rootLayout.Graph.Layout(graphGtx, host, hoverPos, hoverValid)
			graphClipStack.Pop()
			graphOffsetStack.Pop()
		}

		// Divider between stacked host rows.
		if hostIndex > 0 {
			drawHLine(gtx.Ops, currentTheme.BorderColor, 0, rowTop, totalWidth)
		}
	}

	// Single dark/light toggle, pinned to the window's bottom-right corner
	// with a margin.
	{
		margin := gtx.Dp(12)
		buttonX := totalWidth - gtx.Dp(toggleButtonWidthDp) - margin
		buttonY := totalHeight - gtx.Dp(toggleButtonHeightDp) - margin
		drawThemeToggleButton(gtx, rootLayout.MatTheme, currentTheme, &rootLayout.ThemeButton,
			buttonX, buttonY)
	}

	if appState.ContextMenuVisible {
		// Full-window backdrop registered last (highest z-order): any Gio pointer click
		// outside the menu item area is blocked here and dismisses the menu.
		{
			area := clip.Rect(image.Rect(0, 0, totalWidth, totalHeight)).Push(gtx.Ops)
			event.Op(gtx.Ops, &rootLayout.backdropTag)
			area.Pop()
		}
		// Draw menu and register menu items (higher z-order than backdrop).
		drawContextMenu(gtx, currentTheme, rootLayout.MatTheme,
			&rootLayout.settingsItem, &rootLayout.exitItem,
			appState.ContextMenuPos)
	}

	return layout.Dimensions{Size: gtx.Constraints.Max}
}
