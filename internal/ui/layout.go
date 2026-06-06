package ui

import (
	"image"
	"log/slog"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type RootLayout struct {
	AppState    *AppState
	MatTheme    *material.Theme
	Graph       *Graph
	StatsPanel  *StatsPanel
	backdropTag struct{}
	exitItem    widget.Clickable
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
	// Process events deferred from previous frame's handlers.
	for {
		ev, ok := gtx.Source.Event(pointer.Filter{Target: &rootLayout.backdropTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := ev.(pointer.Event); ok {
			rootLayout.AppState.ContextMenuVisible = false
		}
	}
	for rootLayout.exitItem.Clicked(gtx) {
		rootLayout.AppState.ContextMenuVisible = false
		rootLayout.AppState.ExitRequested = true
	}

	currentTheme := rootLayout.AppState.CurrentTheme
	fillRect(gtx.Ops, currentTheme.Background,
		image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y))
	totalWidth := gtx.Constraints.Max.X
	totalHeight := gtx.Constraints.Max.Y
	statsPanelWidth := gtx.Dp(statsPanelWidthDp)
	if totalWidth <= 0 || totalHeight <= 0 {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	statsLeft := totalWidth - statsPanelWidth
	graphWidth := statsLeft

	// Compute the plot area width (the number of data points visible this frame).
	// yAxisLabelWidthDp and rightPaddingDp are defined in graph.go (same package).
	plotWidth := graphWidth - gtx.Dp(yAxisLabelWidthDp) - gtx.Dp(rightPaddingDp)
	if plotWidth > 0 && plotWidth != rootLayout.AppState.GraphWidthPx {
		rootLayout.AppState.GraphWidthPx = plotWidth
		slog.Info("display window", "dataPoints", plotWidth, "seconds", plotWidth)
	}

	// Register drag regions. The graph area (full height) and stats panel top (above
	// button row) are draggable; the button row is excluded so it stays clickable.
	// These regions return HTCAPTION from WM_NCHITTEST — right-clicks there arrive
	// as WM_NCRBUTTONDOWN and are handled by platform_windows.go's custom WndProc.
	buttonRowTop := totalHeight - gtx.Dp(toggleButtonHeightDp) - gtx.Dp(12)
	if graphWidth > 0 {
		stack := clip.Rect(image.Rect(0, 0, graphWidth, totalHeight)).Push(gtx.Ops)
		system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
		stack.Pop()
	}
	if statsLeft > 0 && buttonRowTop > 0 {
		stack := clip.Rect(image.Rect(statsLeft, 0, totalWidth, buttonRowTop)).Push(gtx.Ops)
		system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
		stack.Pop()
	}

	if statsLeft > 0 {
		statsOffsetStack := op.Offset(image.Pt(statsLeft, 0)).Push(gtx.Ops)
		statsClipStack := clip.Rect(image.Rect(0, 0, statsPanelWidth, totalHeight)).Push(gtx.Ops)
		statsGtx := gtx
		statsGtx.Constraints = layout.Exact(image.Pt(statsPanelWidth, totalHeight))
		rootLayout.StatsPanel.Layout(statsGtx)
		statsClipStack.Pop()
		statsOffsetStack.Pop()
	}
	if graphWidth > 0 {
		graphOffsetStack := op.Offset(image.Pt(0, 0)).Push(gtx.Ops)
		graphClipStack := clip.Rect(image.Rect(0, 0, graphWidth, totalHeight)).Push(gtx.Ops)
		graphGtx := gtx
		graphGtx.Constraints = layout.Exact(image.Pt(graphWidth, totalHeight))
		rootLayout.Graph.Layout(graphGtx)
		graphClipStack.Pop()
		graphOffsetStack.Pop()
	}

	if rootLayout.AppState.ContextMenuVisible {
		// Full-window backdrop registered last (highest z-order): any Gio pointer click
		// outside the menu item area is blocked here and dismisses the menu.
		{
			area := clip.Rect(image.Rect(0, 0, totalWidth, totalHeight)).Push(gtx.Ops)
			event.Op(gtx.Ops, &rootLayout.backdropTag)
			area.Pop()
		}
		// Draw menu and register exit item (higher z-order than backdrop).
		drawContextMenu(gtx, currentTheme, rootLayout.MatTheme,
			&rootLayout.exitItem, rootLayout.AppState.ContextMenuPos)
	}

	return layout.Dimensions{Size: gtx.Constraints.Max}
}
