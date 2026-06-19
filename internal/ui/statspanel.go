package ui

import (
	"image"
	"image/color"

	"tidemark/internal/model"
	"tidemark/internal/units"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	statsPanelWidthDp    = 120
	toggleButtonHeightDp = 28
	toggleButtonWidthDp  = 40
)

// StatsPanel is a Gio widget that displays current, max, and average
// download and upload speeds for a single host. The dark/light mode toggle is
// rendered once, globally, by RootLayout rather than per panel.
type StatsPanel struct {
	AppState *AppState
	MatTheme *material.Theme
}

// computedStats holds the derived statistics for a snapshot of data points.
type computedStats struct {
	currentDownloadBytesPerSec float64
	currentUploadBytesPerSec   float64
	maxDownloadBytesPerSec     float64
	maxUploadBytesPerSec       float64
	avgDownloadBytesPerSec     float64
	avgUploadBytesPerSec       float64
}

// Layout renders the statistics panel for one host.
func (statsPanel *StatsPanel) Layout(gtx layout.Context, host *HostState) layout.Dimensions {
	currentTheme := statsPanel.AppState.CurrentTheme
	matTheme := statsPanel.MatTheme
	snapshot := host.DataBuffer.Snapshot()
	if visibleCount := host.GraphWidthPx; visibleCount > 0 && len(snapshot) > visibleCount {
		snapshot = snapshot[len(snapshot)-visibleCount:]
	}

	stats := computeStats(snapshot)

	panelWidth := gtx.Dp(statsPanelWidthDp)
	panelHeight := gtx.Constraints.Max.Y

	fillRect(gtx.Ops, currentTheme.Background, image.Rect(0, 0, panelWidth, panelHeight))

	innerPadding := gtx.Dp(12)
	lineHeight := gtx.Dp(18)
	sectionGap := gtx.Dp(14)

	yOffset := innerPadding

	yOffset = renderStatSection(gtx, matTheme, currentTheme,
		"Current", stats.currentDownloadBytesPerSec, stats.currentUploadBytesPerSec,
		innerPadding, yOffset, panelWidth, lineHeight)

	yOffset += sectionGap

	yOffset = renderStatSection(gtx, matTheme, currentTheme,
		"Average", stats.avgDownloadBytesPerSec, stats.avgUploadBytesPerSec,
		innerPadding, yOffset, panelWidth, lineHeight)

	yOffset += sectionGap

	renderStatSection(gtx, matTheme, currentTheme,
		"Max", stats.maxDownloadBytesPerSec, stats.maxUploadBytesPerSec,
		innerPadding, yOffset, panelWidth, lineHeight)

	return layout.Dimensions{Size: image.Pt(panelWidth, panelHeight)}
}

// drawThemeToggleButton renders the dark/light mode toggle at the given
// top-left position in window coordinates.
func drawThemeToggleButton(
	gtx layout.Context,
	matTheme *material.Theme,
	currentTheme *Theme,
	button *widget.Clickable,
	buttonX, buttonY int,
) {
	buttonHeight := gtx.Dp(toggleButtonHeightDp)
	buttonWidth := gtx.Dp(toggleButtonWidthDp)

	if buttonWidth <= 0 || buttonY <= 0 {
		return
	}

	offsetStack := op.Offset(image.Pt(buttonX, buttonY)).Push(gtx.Ops)
	clipStack := clip.Rect(image.Rect(0, 0, buttonWidth, buttonHeight)).Push(gtx.Ops)

	buttonGtx := gtx
	buttonGtx.Constraints = layout.Exact(image.Pt(buttonWidth, buttonHeight))

	btn := material.Button(matTheme, button, "💡")
	btn.TextSize = unit.Sp(16)
	btn.Background = currentTheme.ButtonFace
	btn.Color = currentTheme.ButtonText
	btn.Inset = layout.Inset{
		Top:    unit.Dp(4),
		Bottom: unit.Dp(4),
		Left:   unit.Dp(6),
		Right:  unit.Dp(6),
	}
	btn.Layout(buttonGtx)

	clipStack.Pop()
	offsetStack.Pop()
}

// renderStatSection draws one labeled section (Current, Average, or Max) with
// a download row and an upload row, each colored with its direction color.
// Returns the y offset after the last row.
func renderStatSection(
	gtx layout.Context,
	matTheme *material.Theme,
	currentTheme *Theme,
	sectionTitle string,
	downloadBytesPerSec, uploadBytesPerSec float64,
	xPadding, yStart, panelWidth, lineHeight int,
) int {
	contentWidth := panelWidth - xPadding*2

	drawPanelRow(gtx, matTheme, currentTheme.PanelText, sectionTitle,
		xPadding, yStart, contentWidth, lineHeight, unit.Sp(13))
	yStart += lineHeight + 2

	drawPanelRow(gtx, matTheme, currentTheme.DownloadLabel,
		"▼ "+units.FormatBytesPerSec(downloadBytesPerSec),
		xPadding, yStart, contentWidth, lineHeight, unit.Sp(11))
	yStart += lineHeight

	drawPanelRow(gtx, matTheme, currentTheme.UploadLabel,
		"▲ "+units.FormatBytesPerSec(uploadBytesPerSec),
		xPadding, yStart, contentWidth, lineHeight, unit.Sp(11))
	yStart += lineHeight

	return yStart
}

// drawPanelRow renders a single text row at the given position in the panel.
func drawPanelRow(
	gtx layout.Context,
	matTheme *material.Theme,
	textColor color.NRGBA,
	content string,
	x, y, width, height int,
	textSize unit.Sp,
) {
	if width <= 0 || height <= 0 {
		return
	}
	offsetStack := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	clipStack := clip.Rect(image.Rect(0, 0, width, height)).Push(gtx.Ops)

	subGtx := gtx
	subGtx.Constraints = layout.Exact(image.Pt(width, height))
	labelWidget := material.Label(matTheme, textSize, content)
	labelWidget.Color = textColor
	labelWidget.Alignment = text.Start
	labelWidget.Layout(subGtx)

	clipStack.Pop()
	offsetStack.Pop()
}

// computeStats derives current, max, and average speeds from the snapshot.
// "Current" is the most recent non-error data point. Points are iterated
// oldest-first, so the last non-error point in the loop is the most recent.
func computeStats(dataPoints []model.DataPoint) computedStats {
	var stats computedStats
	var downloadSum, uploadSum float64
	var nonErrorCount int

	for _, dataPoint := range dataPoints {
		if dataPoint.IsError {
			continue
		}
		nonErrorCount++
		downloadSum += dataPoint.DownloadBytesPerSec
		uploadSum += dataPoint.UploadBytesPerSec

		if dataPoint.DownloadBytesPerSec > stats.maxDownloadBytesPerSec {
			stats.maxDownloadBytesPerSec = dataPoint.DownloadBytesPerSec
		}
		if dataPoint.UploadBytesPerSec > stats.maxUploadBytesPerSec {
			stats.maxUploadBytesPerSec = dataPoint.UploadBytesPerSec
		}
		stats.currentDownloadBytesPerSec = dataPoint.DownloadBytesPerSec
		stats.currentUploadBytesPerSec = dataPoint.UploadBytesPerSec
	}

	if nonErrorCount > 0 {
		stats.avgDownloadBytesPerSec = downloadSum / float64(nonErrorCount)
		stats.avgUploadBytesPerSec = uploadSum / float64(nonErrorCount)
	}

	return stats
}
