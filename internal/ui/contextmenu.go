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
	menuWidthDp      = 100
	menuItemHeightDp = 24
	menuPaddingXDp   = 8
)

func drawContextMenu(
	gtx layout.Context,
	currentTheme *Theme,
	matTheme *material.Theme,
	exitItem *widget.Clickable,
	pos image.Point,
) {
	menuWidth  := gtx.Dp(menuWidthDp)
	itemHeight := gtx.Dp(menuItemHeightDp)
	menuHeight := itemHeight + 2 // 1px border top + bottom

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

	// Border lines
	fillRect(gtx.Ops, currentTheme.BorderColor,
		image.Rect(menuRect.Min.X, menuRect.Min.Y, menuRect.Max.X, menuRect.Min.Y+1))
	fillRect(gtx.Ops, currentTheme.BorderColor,
		image.Rect(menuRect.Min.X, menuRect.Max.Y-1, menuRect.Max.X, menuRect.Max.Y))
	fillRect(gtx.Ops, currentTheme.BorderColor,
		image.Rect(menuRect.Min.X, menuRect.Min.Y, menuRect.Min.X+1, menuRect.Max.Y))
	fillRect(gtx.Ops, currentTheme.BorderColor,
		image.Rect(menuRect.Max.X-1, menuRect.Min.Y, menuRect.Max.X, menuRect.Max.Y))

	itemRect := image.Rect(
		menuRect.Min.X+1, menuRect.Min.Y+1,
		menuRect.Max.X-1, menuRect.Min.Y+1+itemHeight,
	)
	itemWidth := itemRect.Dx()

	offsetStack := op.Offset(itemRect.Min).Push(gtx.Ops)
	clipStack := clip.Rect(image.Rect(0, 0, itemWidth, itemHeight)).Push(gtx.Ops)

	exitGtx := gtx
	exitGtx.Constraints = layout.Exact(image.Pt(itemWidth, itemHeight))
	exitItem.Layout(exitGtx, func(gtx layout.Context) layout.Dimensions {
		if exitItem.Hovered() {
			fillRect(gtx.Ops, currentTheme.ButtonFace,
				image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y))
		}
		pad := gtx.Dp(menuPaddingXDp)
		labelOffset := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
		labelGtx := gtx
		labelGtx.Constraints = layout.Exact(
			image.Pt(gtx.Constraints.Max.X-pad, gtx.Constraints.Max.Y))
		lbl := material.Label(matTheme, unit.Sp(12), "Exit")
		lbl.Color = currentTheme.PanelText
		lbl.Alignment = text.Start
		lbl.Layout(labelGtx)
		labelOffset.Pop()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	})

	clipStack.Pop()
	offsetStack.Pop()
}
