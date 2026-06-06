package ui

import (
	"image"

	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget/material"
)

type RootLayout struct {
	AppState   *AppState
	MatTheme   *material.Theme
	Graph      *Graph
	StatsPanel *StatsPanel
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

	// Register drag regions. The graph area (full height) and the stats panel
	// top (above the button row) are draggable. The button row at the panel
	// bottom is intentionally excluded so the button remains clickable.
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
	return layout.Dimensions{Size: gtx.Constraints.Max}
}
