package ui

import (
	"image"
	"image/color"

	"ntg/internal/model"
	"ntg/internal/units"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	statsPanelWidthDp    = 150
	toggleButtonHeightDp = 28
	toggleButtonWidthDp  = 40
)

// StatsPanel is a Gio widget that displays current, max, and average
// download and upload speeds, plus the dark/light mode toggle button.
type StatsPanel struct {
	AppState    *AppState
	MatTheme    *material.Theme
	ThemeButton widget.Clickable
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

// Layout renders the statistics panel and the theme toggle button.
func (statsPanel *StatsPanel) Layout(gtx layout.Context) layout.Dimensions {
	// Handle theme toggle clicks before rendering.
	for statsPanel.ThemeButton.Clicked(gtx) {
		statsPanel.AppState.ToggleTheme()
	}

	currentTheme := statsPanel.AppState.CurrentTheme
	matTheme := statsPanel.MatTheme
	snapshot := statsPanel.AppState.DataBuffer.Snapshot()
	if visibleCount := statsPanel.AppState.GraphWidthPx; visibleCount > 0 && len(snapshot) > visibleCount {
		snapshot = snapshot[len(snapshot)-visibleCount:]
	}

	stats := computeStats(snapshot)

	panelWidth := gtx.Dp(statsPanelWidthDp)
	panelHeight := gtx.Constraints.Max.Y

	fillRect(gtx.Ops, currentTheme.Background, image.Rect(0, 0, panelWidth, panelHeight))

	innerPadding := gtx.Dp(12)
	lineHeight := gtx.Dp(18)
	sectionGap := gtx.Dp(12)

	yOffset := innerPadding

	yOffset = renderStatSection(gtx, matTheme, currentTheme,
		"Download ▼", currentTheme.DownloadLabel, stats.currentDownloadBytesPerSec,
		stats.maxDownloadBytesPerSec, stats.avgDownloadBytesPerSec,
		innerPadding, yOffset, panelWidth, lineHeight)

	yOffset += sectionGap

	renderStatSection(gtx, matTheme, currentTheme,
		"Upload ▲", currentTheme.UploadLabel, stats.currentUploadBytesPerSec,
		stats.maxUploadBytesPerSec, stats.avgUploadBytesPerSec,
		innerPadding, yOffset, panelWidth, lineHeight)

	// Theme toggle button pinned to the bottom of the panel.
	drawThemeToggleButton(gtx, matTheme, currentTheme, &statsPanel.ThemeButton,
		innerPadding, panelWidth, panelHeight)

	return layout.Dimensions{Size: image.Pt(panelWidth, panelHeight)}
}

// drawThemeToggleButton renders the dark/light mode toggle at the bottom of the panel.
func drawThemeToggleButton(
	gtx layout.Context,
	matTheme *material.Theme,
	currentTheme *Theme,
	button *widget.Clickable,
	innerPadding, panelWidth, panelHeight int,
) {
	buttonHeight := gtx.Dp(toggleButtonHeightDp)
	buttonWidth := gtx.Dp(toggleButtonWidthDp)
	buttonX := (panelWidth - buttonWidth) / 2
	buttonY := panelHeight - buttonHeight - innerPadding

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

// renderStatSection draws one labeled section (Download or Upload) with
// Current, Max, and Avg rows. Returns the y offset after the last row.
func renderStatSection(
	gtx layout.Context,
	matTheme *material.Theme,
	currentTheme *Theme,
	sectionTitle string,
	titleColor color.NRGBA,
	currentBytesPerSec, maxBytesPerSec, avgBytesPerSec float64,
	xPadding, yStart, panelWidth, lineHeight int,
) int {
	contentWidth := panelWidth - xPadding*2

	drawPanelRow(gtx, matTheme, titleColor, sectionTitle,
		xPadding, yStart, contentWidth, lineHeight, unit.Sp(13))
	yStart += lineHeight + 2

	drawPanelRow(gtx, matTheme, currentTheme.PanelText,
		"Current: "+units.FormatBytesPerSec(currentBytesPerSec),
		xPadding, yStart, contentWidth, lineHeight, unit.Sp(11))
	yStart += lineHeight

	drawPanelRow(gtx, matTheme, currentTheme.PanelText,
		"Max:     "+units.FormatBytesPerSec(maxBytesPerSec),
		xPadding, yStart, contentWidth, lineHeight, unit.Sp(11))
	yStart += lineHeight

	drawPanelRow(gtx, matTheme, currentTheme.PanelText,
		"Avg:     "+units.FormatBytesPerSec(avgBytesPerSec),
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
