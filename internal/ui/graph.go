package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"tidemark/internal/model"
	"tidemark/internal/units"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

const (
	yAxisLabelWidthDp  = 72
	xAxisLabelHeightDp = 20
	topPaddingDp       = 22
	rightPaddingDp     = 8
	tickLengthDp       = 4
)

// pixelPoint holds precomputed screen Y-coordinates for all three data series
// of one data sample. All coordinates are in Gio pixel space (Y increases downward).
type pixelPoint struct {
	x         float32
	downloadY float32
	uploadY   float32
	overlapY  float32
	isError   bool
}

// Graph is a Gio widget that renders the live network throughput chart.
type Graph struct {
	AppState *AppState
	MatTheme *material.Theme
}

// Layout draws the full graph widget for one host into the available
// constraints. hoverPos is the mouse position relative to this widget's
// origin; hoverValid is false when the mouse is outside this host's graph.
func (graph *Graph) Layout(gtx layout.Context, host *HostState, hoverPos image.Point, hoverValid bool) layout.Dimensions {
	currentTheme := graph.AppState.CurrentTheme
	matTheme := graph.MatTheme
	snapshot := host.DataBuffer.Snapshot()
	hostLabel := host.HostLabel

	totalWidth := gtx.Constraints.Max.X
	totalHeight := gtx.Constraints.Max.Y

	yLabelWidth := gtx.Dp(yAxisLabelWidthDp)
	xLabelHeight := gtx.Dp(xAxisLabelHeightDp)
	topPadding := gtx.Dp(topPaddingDp)
	rightPadding := gtx.Dp(rightPaddingDp)
	tickLength := gtx.Dp(tickLengthDp)

	plotLeft := yLabelWidth
	plotTop := topPadding
	plotRight := totalWidth - rightPadding
	plotBottom := totalHeight - xLabelHeight
	plotWidth := plotRight - plotLeft
	plotHeight := plotBottom - plotTop

	if plotWidth <= 0 || plotHeight <= 0 {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Fill backgrounds.
	fillRect(gtx.Ops, currentTheme.Background, image.Rect(0, 0, totalWidth, totalHeight))
	fillRect(gtx.Ops, currentTheme.GraphBackground, image.Rect(plotLeft, plotTop, plotRight, plotBottom))

	// Determine Y-axis scale from data currently in view. The visible window is
	// plotWidth seconds wide (1px = 1s), matching buildPixelPoints below, so the
	// scale shrinks again once a spike scrolls off the left edge.
	maximumBytesPerSec := computeMaximumBytesPerSec(snapshot, plotWidth)
	scaleUnit := units.GetScaleUnit(maximumBytesPerSec)
	niceMaxInUnit, stepSizeInUnit := units.NiceAxisMax(maximumBytesPerSec / scaleUnit.Divisor)
	niceMaxBytesPerSec := niceMaxInUnit * scaleUnit.Divisor
	if niceMaxBytesPerSec <= 0 {
		niceMaxBytesPerSec = scaleUnit.Divisor
		niceMaxInUnit = 1.0
		stepSizeInUnit = 0.25
	}

	drawYAxis(gtx, matTheme, currentTheme, scaleUnit, niceMaxInUnit, stepSizeInUnit,
		plotLeft, plotTop, plotRight, plotBottom, plotHeight, tickLength)

	drawXAxis(gtx, matTheme, currentTheme,
		plotLeft, plotBottom, plotWidth, tickLength)

	if len(snapshot) > 0 {
		pixelPoints := buildPixelPoints(snapshot, plotWidth,
			plotLeft, plotBottom, plotWidth, plotHeight, niceMaxBytesPerSec)
		drawDataAreas(gtx.Ops, currentTheme, pixelPoints, float32(plotTop), float32(plotBottom))
	}

	// Host label at top-center.
	drawPositionedLabel(gtx, matTheme, currentTheme.AxisText, "Host: "+hostLabel,
		image.Pt(0, 3), totalWidth, topPadding-3, text.Middle)

	// Plot border.
	drawHLine(gtx.Ops, currentTheme.BorderColor, plotLeft, plotTop, plotRight)
	drawHLine(gtx.Ops, currentTheme.BorderColor, plotLeft, plotBottom, plotRight)
	drawVLine(gtx.Ops, currentTheme.BorderColor, plotLeft, plotTop, plotBottom)
	drawVLine(gtx.Ops, currentTheme.BorderColor, plotRight, plotTop, plotBottom)

	// Hover tooltip: when the mouse is over a data-point column whose point
	// carries top-talker info, show the LAN IP and its rate for that second.
	if hoverValid && len(snapshot) > 0 &&
		hoverPos.X >= plotLeft && hoverPos.X < plotRight &&
		hoverPos.Y >= plotTop && hoverPos.Y < plotBottom {
		drawHoverTooltip(gtx, matTheme, currentTheme, snapshot, hoverPos,
			plotLeft, plotTop, plotRight, plotBottom, plotWidth)
	}

	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// hoverMatchToleranceMs is the maximum distance between the hovered column's
// timestamp and a data point for the point to count as hovered. Columns are
// 1 second apart, so anything beyond ~3/4 s is a gap, not a neighbor.
const hoverMatchToleranceMs = 750

// findHoveredDataPoint inverts the x→age mapping used by buildPixelPoints
// (1 px = 1 s) and returns the snapshot point nearest to the hovered column,
// or nil when no point lies within the match tolerance.
func findHoveredDataPoint(
	snapshot []model.DataPoint,
	hoverX, plotLeft, plotWidth int,
) *model.DataPoint {
	nowMs := time.Now().UnixMilli()
	historyMs := int64(plotWidth) * 1000
	xFraction := float64(hoverX-plotLeft) / float64(plotWidth)
	targetMs := nowMs - int64((1.0-xFraction)*float64(historyMs))

	var nearestPoint *model.DataPoint
	var nearestDistanceMs int64
	for pointIndex := range snapshot {
		distanceMs := snapshot[pointIndex].TimestampMs - targetMs
		if distanceMs < 0 {
			distanceMs = -distanceMs
		}
		if nearestPoint == nil || distanceMs < nearestDistanceMs {
			nearestPoint = &snapshot[pointIndex]
			nearestDistanceMs = distanceMs
		}
	}
	if nearestPoint == nil || nearestDistanceMs > hoverMatchToleranceMs {
		return nil
	}
	return nearestPoint
}

// tooltipRow is one colored text line inside the hover tooltip box.
type tooltipRow struct {
	textColor color.NRGBA
	content   string
}

// drawHoverTooltip renders the vertical hover marker and the top-talker
// tooltip box for the data point under the cursor: one row for the LAN IP
// that downloaded the most that second, one for the IP that uploaded the
// most. Points without top-talker info (SNMP hosts, error points, feature
// disabled, idle LAN) show nothing.
func drawHoverTooltip(
	gtx layout.Context,
	matTheme *material.Theme,
	currentTheme *Theme,
	snapshot []model.DataPoint,
	hoverPos image.Point,
	plotLeft, plotTop, plotRight, plotBottom, plotWidth int,
) {
	hoveredPoint := findHoveredDataPoint(snapshot, hoverPos.X, plotLeft, plotWidth)
	if hoveredPoint == nil || (hoveredPoint.TopDownloadIP == "" && hoveredPoint.TopUploadIP == "") {
		return
	}

	drawVLine(gtx.Ops, currentTheme.HoverMarker, hoverPos.X, plotTop, plotBottom)

	var rows []tooltipRow
	if hoveredPoint.TopDownloadIP != "" {
		rows = append(rows, tooltipRow{
			textColor: currentTheme.DownloadLabel,
			content: "▼ " + hoveredPoint.TopDownloadIP + "  " +
				units.FormatBytesPerSec(hoveredPoint.TopDownloadBytesPerSec),
		})
	}
	if hoveredPoint.TopUploadIP != "" {
		rows = append(rows, tooltipRow{
			textColor: currentTheme.UploadLabel,
			content: "▲ " + hoveredPoint.TopUploadIP + "  " +
				units.FormatBytesPerSec(hoveredPoint.TopUploadBytesPerSec),
		})
	}

	padding := gtx.Dp(6)
	lineHeight := gtx.Dp(16)
	textChars := 0
	for _, row := range rows {
		if len(row.content) > textChars {
			textChars = len(row.content)
		}
	}
	// Rough glyph width at 11sp; the label is clipped to the box either way.
	boxWidth := gtx.Dp(unit.Dp(float32(textChars)*6.5)) + padding*2
	boxHeight := lineHeight*len(rows) + padding*2

	cursorOffset := gtx.Dp(14)
	boxLeft := hoverPos.X + cursorOffset
	if boxLeft+boxWidth > plotRight {
		boxLeft = hoverPos.X - cursorOffset - boxWidth
	}
	boxTop := hoverPos.Y + cursorOffset
	if boxTop+boxHeight > plotBottom {
		boxTop = hoverPos.Y - cursorOffset - boxHeight
	}

	boxRect := image.Rect(boxLeft, boxTop, boxLeft+boxWidth, boxTop+boxHeight)
	fillRect(gtx.Ops, currentTheme.TooltipBackground, boxRect)
	drawHLine(gtx.Ops, currentTheme.BorderColor, boxRect.Min.X, boxRect.Min.Y, boxRect.Max.X)
	drawHLine(gtx.Ops, currentTheme.BorderColor, boxRect.Min.X, boxRect.Max.Y-1, boxRect.Max.X)
	drawVLine(gtx.Ops, currentTheme.BorderColor, boxRect.Min.X, boxRect.Min.Y, boxRect.Max.Y)
	drawVLine(gtx.Ops, currentTheme.BorderColor, boxRect.Max.X-1, boxRect.Min.Y, boxRect.Max.Y)

	for rowIndex, row := range rows {
		drawPositionedLabel(gtx, matTheme, row.textColor, row.content,
			image.Pt(boxLeft+padding, boxTop+padding+rowIndex*lineHeight),
			boxWidth-padding*2, lineHeight, text.Start)
	}
}

// computeMaximumBytesPerSec returns the highest single download or upload rate
// across all non-error data points currently within the visible window.
// windowSeconds is the width of the visible plot in seconds (1px = 1s); points
// older than this have scrolled off the left edge and are excluded so the scale
// can shrink as old peaks leave view.
func computeMaximumBytesPerSec(dataPoints []model.DataPoint, windowSeconds int) float64 {
	nowMs := time.Now().UnixMilli()
	historyMs := int64(windowSeconds) * 1000
	var maximumValue float64
	for _, dataPoint := range dataPoints {
		if dataPoint.IsError {
			continue
		}
		ageMs := nowMs - dataPoint.TimestampMs
		if ageMs < 0 || ageMs > historyMs {
			continue
		}
		if dataPoint.DownloadBytesPerSec > maximumValue {
			maximumValue = dataPoint.DownloadBytesPerSec
		}
		if dataPoint.UploadBytesPerSec > maximumValue {
			maximumValue = dataPoint.UploadBytesPerSec
		}
	}
	return maximumValue
}

// buildPixelPoints converts raw DataPoints into pixel-space coordinates
// ready for polygon rendering.
func buildPixelPoints(
	snapshot []model.DataPoint,
	windowSeconds int,
	plotLeft, plotBottom, plotWidth, plotHeight int,
	niceMaxBytesPerSec float64,
) []pixelPoint {
	nowMs := time.Now().UnixMilli()
	historyMs := int64(windowSeconds) * 1000
	pixelPoints := make([]pixelPoint, 0, len(snapshot))

	for _, dataPoint := range snapshot {
		ageMs := nowMs - dataPoint.TimestampMs
		if ageMs < 0 || ageMs > historyMs {
			continue
		}
		xFraction := 1.0 - float32(ageMs)/float32(historyMs)
		xPixel := float32(plotLeft) + xFraction*float32(plotWidth)

		pixelPoint := pixelPoint{x: xPixel, isError: dataPoint.IsError}

		if !dataPoint.IsError && niceMaxBytesPerSec > 0 {
			downloadFraction := float32(math.Min(dataPoint.DownloadBytesPerSec/niceMaxBytesPerSec, 1.0))
			uploadFraction := float32(math.Min(dataPoint.UploadBytesPerSec/niceMaxBytesPerSec, 1.0))
			overlapFraction := float32(math.Min(
				math.Min(dataPoint.DownloadBytesPerSec, dataPoint.UploadBytesPerSec)/niceMaxBytesPerSec,
				1.0,
			))
			pixelPoint.downloadY = float32(plotBottom) - downloadFraction*float32(plotHeight)
			pixelPoint.uploadY = float32(plotBottom) - uploadFraction*float32(plotHeight)
			pixelPoint.overlapY = float32(plotBottom) - overlapFraction*float32(plotHeight)
		} else {
			pixelPoint.downloadY = float32(plotBottom)
			pixelPoint.uploadY = float32(plotBottom)
			pixelPoint.overlapY = float32(plotBottom)
		}

		pixelPoints = append(pixelPoints, pixelPoint)
	}
	return pixelPoints
}

// drawDataAreas renders the three colored fill regions and error bars.
// Draw order: download (red) → upload (green) → overlap (yellow) → error bars (purple).
func drawDataAreas(
	ops *op.Ops,
	currentTheme *Theme,
	pixelPoints []pixelPoint,
	plotTop, plotBottom float32,
) {
	drawFilledArea(ops, currentTheme.DownloadFill, pixelPoints, plotBottom,
		func(pt pixelPoint) float32 { return pt.downloadY })
	drawFilledArea(ops, currentTheme.UploadFill, pixelPoints, plotBottom,
		func(pt pixelPoint) float32 { return pt.uploadY })
	drawFilledArea(ops, currentTheme.OverlapFill, pixelPoints, plotBottom,
		func(pt pixelPoint) float32 { return pt.overlapY })

	for _, pt := range pixelPoints {
		if pt.isError {
			fillRect(ops, currentTheme.ErrorBar,
				image.Rect(int(pt.x), int(plotTop), int(pt.x)+1, int(plotBottom)))
		}
	}
}

// drawFilledArea renders one colored fill polygon, split at error/gap points.
func drawFilledArea(
	ops *op.Ops,
	fillColor color.NRGBA,
	pixelPoints []pixelPoint,
	baselineY float32,
	getTopY func(pixelPoint) float32,
) {
	runStartIndex := -1
	for i := 0; i <= len(pixelPoints); i++ {
		isGap := i == len(pixelPoints) || pixelPoints[i].isError
		if !isGap {
			if runStartIndex < 0 {
				runStartIndex = i
			}
			continue
		}
		if runStartIndex >= 0 {
			drawPolygonRun(ops, fillColor, pixelPoints[runStartIndex:i], baselineY, getTopY)
			runStartIndex = -1
		}
	}
}

// drawPolygonRun draws a single filled polygon for a contiguous non-error run.
func drawPolygonRun(
	ops *op.Ops,
	fillColor color.NRGBA,
	runPoints []pixelPoint,
	baselineY float32,
	getTopY func(pixelPoint) float32,
) {
	if len(runPoints) < 1 {
		return
	}
	var polygonPath clip.Path
	polygonPath.Begin(ops)
	polygonPath.MoveTo(f32.Pt(runPoints[0].x, baselineY))
	polygonPath.LineTo(f32.Pt(runPoints[0].x, getTopY(runPoints[0])))
	for _, pt := range runPoints[1:] {
		polygonPath.LineTo(f32.Pt(pt.x, getTopY(pt)))
	}
	polygonPath.LineTo(f32.Pt(runPoints[len(runPoints)-1].x, baselineY))
	polygonPath.Close()
	paint.FillShape(ops, fillColor, clip.Outline{Path: polygonPath.End()}.Op())
}

// drawYAxis draws horizontal gridlines and speed labels along the Y axis.
func drawYAxis(
	gtx layout.Context,
	matTheme *material.Theme,
	currentTheme *Theme,
	scaleUnit units.ScaleUnit,
	niceMaxInUnit, stepSizeInUnit float64,
	plotLeft, plotTop, plotRight, plotBottom, plotHeight, tickLength int,
) {
	if stepSizeInUnit <= 0 {
		return
	}
	for notchValue := 0.0; notchValue <= niceMaxInUnit+stepSizeInUnit*0.5; notchValue += stepSizeInUnit {
		yFraction := float32(notchValue / niceMaxInUnit)
		yPixel := int(float32(plotBottom) - yFraction*float32(plotHeight))

		if yPixel < plotTop || yPixel > plotBottom {
			continue
		}
		if notchValue > 0 {
			drawHLine(gtx.Ops, currentTheme.GridLine, plotLeft, yPixel, plotRight)
		}
		drawHLine(gtx.Ops, currentTheme.AxisText, plotLeft-tickLength, yPixel, plotLeft)

		labelText := fmt.Sprintf("%g %s", notchValue, scaleUnit.Label)
		drawPositionedLabel(gtx, matTheme, currentTheme.AxisText, labelText,
			image.Pt(0, yPixel-8), plotLeft-tickLength-2, 16, text.End)
	}
}

// minXLabelSpacingPx is the minimum pixel distance between two X axis labels
// before the interval is widened to the next nice value.
const minXLabelSpacingPx = 36

// niceMinuteIntervals lists candidate tick spacings in ascending order.
// The first value that produces spacing >= minXLabelSpacingPx is chosen.
var niceMinuteIntervals = []int{1, 2, 5, 10, 15, 20, 30, 60, 120, 180, 240, 300, 360}

// chooseXAxisInterval returns the smallest "nice" minute interval such that
// adjacent labels are at least minXLabelSpacingPx pixels apart.
// plotWidthPx is both the pixel width and the number of seconds displayed (1px = 1s).
func chooseXAxisInterval(plotWidthPx int) time.Duration {
	totalMinutes := plotWidthPx / 60
	if totalMinutes <= 0 || plotWidthPx <= 0 {
		return time.Minute
	}
	pixelsPerMinute := float64(plotWidthPx) / float64(totalMinutes)
	for _, intervalMinutes := range niceMinuteIntervals {
		if float64(intervalMinutes)*pixelsPerMinute >= minXLabelSpacingPx {
			return time.Duration(intervalMinutes) * time.Minute
		}
	}
	return time.Duration(niceMinuteIntervals[len(niceMinuteIntervals)-1]) * time.Minute
}

func drawXAxis(
	gtx layout.Context,
	matTheme *material.Theme,
	currentTheme *Theme,
	plotLeft, plotBottom, plotWidth, tickLength int,
) {
	nowMs := time.Now().UnixMilli()
	historyMs := int64(plotWidth) * 1000 // 1px = 1 second
	windowStart := time.UnixMilli(nowMs - historyMs)

	interval := chooseXAxisInterval(plotWidth)
	firstBoundary := windowStart.Truncate(interval).Add(interval)

	for tickTime := firstBoundary; !tickTime.After(time.UnixMilli(nowMs)); tickTime = tickTime.Add(interval) {
		ageMs := nowMs - tickTime.UnixMilli()
		if ageMs < 0 || ageMs > historyMs {
			continue
		}
		xFraction := 1.0 - float64(ageMs)/float64(historyMs)
		xPixel := int(float64(plotLeft) + xFraction*float64(plotWidth))

		drawVLine(gtx.Ops, currentTheme.AxisText, xPixel, plotBottom, plotBottom+tickLength)

		timeLabel := tickTime.Format("15:04")
		drawPositionedLabel(gtx, matTheme, currentTheme.AxisText, timeLabel,
			image.Pt(xPixel-20, plotBottom+tickLength+1), 40, 16, text.Middle)
	}
}

// drawPositionedLabel renders a text label at the given absolute pixel position
// within the widget, clipped to the specified width and height.
func drawPositionedLabel(
	gtx layout.Context,
	matTheme *material.Theme,
	textColor color.NRGBA,
	labelContent string,
	position image.Point,
	width, height int,
	alignment text.Alignment,
) {
	if width <= 0 || height <= 0 {
		return
	}
	offsetStack := op.Offset(position).Push(gtx.Ops)
	clipStack := clip.Rect(image.Rect(0, 0, width, height)).Push(gtx.Ops)

	subGtx := gtx
	subGtx.Constraints = layout.Exact(image.Pt(width, height))
	labelWidget := material.Label(matTheme, unit.Sp(11), labelContent)
	labelWidget.Color = textColor
	labelWidget.Alignment = alignment
	labelWidget.Layout(subGtx)

	clipStack.Pop()
	offsetStack.Pop()
}

// fillRect fills an integer-coordinate rectangle with a solid color.
func fillRect(ops *op.Ops, fillColor color.NRGBA, rect image.Rectangle) {
	paint.FillShape(ops, fillColor, clip.Rect(rect).Op())
}

// drawHLine draws a 1px-tall horizontal line between x1 and x2 at the given y.
func drawHLine(ops *op.Ops, lineColor color.NRGBA, x1, y, x2 int) {
	if x1 >= x2 {
		return
	}
	fillRect(ops, lineColor, image.Rect(x1, y, x2, y+1))
}

// drawVLine draws a 1px-wide vertical line between y1 and y2 at the given x.
func drawVLine(ops *op.Ops, lineColor color.NRGBA, x, y1, y2 int) {
	if y1 >= y2 {
		return
	}
	fillRect(ops, lineColor, image.Rect(x, y1, x+1, y2))
}
