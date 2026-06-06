package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	menuWidthDp      = 120
	menuItemHeightDp = 24
	menuPaddingXDp   = 8
)

// drawContextMenu renders the right-click context menu with two items:
// Settings (top) and Exit (bottom). Both items register clickable hit areas
// with z-order higher than the full-window backdrop.
func drawContextMenu(
	gtx layout.Context,
	currentTheme *Theme,
	matTheme *material.Theme,
	settingsItem *widget.Clickable,
	exitItem *widget.Clickable,
	pos image.Point,
) {
	menuWidth  := gtx.Dp(menuWidthDp)
	itemHeight := gtx.Dp(menuItemHeightDp)
	numItems   := 2
	menuHeight := numItems*itemHeight + 2 // 1px border top + bottom

	maxX := gtx.Constraints.Max.X
	maxY := gtx.Constraints.Max.Y
	if pos.X+menuWidth > maxX {
		pos.X = maxX - menuWidth
	}
	if pos.Y+menuHeight > maxY {
		pos.Y = maxY - menuHeight
	}
	if pos.X < 0 {
		pos.X = 0
	}
	if pos.Y < 0 {
		pos.Y = 0
	}

	menuRect := image.Rect(pos.X, pos.Y, pos.X+menuWidth, pos.Y+menuHeight)

	fillRect(gtx.Ops, currentTheme.PanelBackground, menuRect)
	// Border
	fillRect(gtx.Ops, currentTheme.BorderColor,
		image.Rect(menuRect.Min.X, menuRect.Min.Y, menuRect.Max.X, menuRect.Min.Y+1))
	fillRect(gtx.Ops, currentTheme.BorderColor,
		image.Rect(menuRect.Min.X, menuRect.Max.Y-1, menuRect.Max.X, menuRect.Max.Y))
	fillRect(gtx.Ops, currentTheme.BorderColor,
		image.Rect(menuRect.Min.X, menuRect.Min.Y, menuRect.Min.X+1, menuRect.Max.Y))
	fillRect(gtx.Ops, currentTheme.BorderColor,
		image.Rect(menuRect.Max.X-1, menuRect.Min.Y, menuRect.Max.X, menuRect.Max.Y))

	itemWidth := menuWidth - 2
	drawMenuItem(gtx, currentTheme, matTheme, settingsItem, "Settings",
		image.Pt(menuRect.Min.X+1, menuRect.Min.Y+1), itemWidth, itemHeight)
	drawMenuItem(gtx, currentTheme, matTheme, exitItem, "Exit",
		image.Pt(menuRect.Min.X+1, menuRect.Min.Y+1+itemHeight), itemWidth, itemHeight)
}

func drawMenuItem(
	gtx layout.Context,
	currentTheme *Theme,
	matTheme *material.Theme,
	item *widget.Clickable,
	label string,
	origin image.Point,
	w, h int,
) {
	offsetStack := op.Offset(origin).Push(gtx.Ops)
	clipStack := clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops)

	itemGtx := gtx
	itemGtx.Constraints = layout.Exact(image.Pt(w, h))
	item.Layout(itemGtx, func(gtx layout.Context) layout.Dimensions {
		if item.Hovered() {
			fillRect(gtx.Ops, currentTheme.ButtonFace,
				image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y))
		}
		pad := gtx.Dp(menuPaddingXDp)
		labelOffset := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
		labelGtx := gtx
		labelGtx.Constraints = layout.Exact(
			image.Pt(gtx.Constraints.Max.X-pad, gtx.Constraints.Max.Y))
		lbl := material.Label(matTheme, unit.Sp(12), label)
		lbl.Color = currentTheme.PanelText
		lbl.Alignment = text.Start
		lbl.Layout(labelGtx)
		labelOffset.Pop()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	})

	clipStack.Pop()
	offsetStack.Pop()
}
