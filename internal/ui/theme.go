package ui

import "image/color"

// Theme holds all colors used for rendering the graph and statistics panel.
type Theme struct {
	Background      color.NRGBA
	GraphBackground color.NRGBA
	DownloadFill    color.NRGBA
	UploadFill      color.NRGBA
	OverlapFill     color.NRGBA
	ErrorBar        color.NRGBA
	AxisText        color.NRGBA
	GridLine        color.NRGBA
	PanelText       color.NRGBA
	PanelBackground color.NRGBA
	BorderColor     color.NRGBA
	ButtonText      color.NRGBA
	ButtonFace      color.NRGBA
	DownloadLabel   color.NRGBA
	UploadLabel     color.NRGBA
}

// DarkTheme is the default dark color scheme.
var DarkTheme = Theme{
	Background:      color.NRGBA{R: 26, G: 26, B: 26, A: 255},
	GraphBackground: color.NRGBA{R: 13, G: 13, B: 13, A: 255},
	DownloadFill:    color.NRGBA{R: 220, G: 60, B: 60, A: 180},
	UploadFill:      color.NRGBA{R: 60, G: 200, B: 60, A: 180},
	OverlapFill:     color.NRGBA{R: 240, G: 220, B: 0, A: 220},
	ErrorBar:        color.NRGBA{R: 160, G: 80, B: 200, A: 200},
	AxisText:        color.NRGBA{R: 180, G: 180, B: 180, A: 255},
	GridLine:        color.NRGBA{R: 50, G: 50, B: 50, A: 255},
	PanelText:       color.NRGBA{R: 200, G: 200, B: 200, A: 255},
	PanelBackground: color.NRGBA{R: 30, G: 30, B: 30, A: 255},
	BorderColor:     color.NRGBA{R: 70, G: 70, B: 70, A: 255},
	ButtonText:      color.NRGBA{R: 220, G: 220, B: 220, A: 255},
	ButtonFace:      color.NRGBA{R: 55, G: 55, B: 55, A: 255},
	DownloadLabel:   color.NRGBA{R: 255, G: 100, B: 100, A: 255},
	UploadLabel:     color.NRGBA{R: 100, G: 230, B: 100, A: 255},
}

// LightTheme is the bright color scheme.
var LightTheme = Theme{
	Background:      color.NRGBA{R: 240, G: 240, B: 240, A: 255},
	GraphBackground: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	DownloadFill:    color.NRGBA{R: 210, G: 50, B: 50, A: 160},
	UploadFill:      color.NRGBA{R: 40, G: 170, B: 40, A: 160},
	OverlapFill:     color.NRGBA{R: 220, G: 190, B: 0, A: 200},
	ErrorBar:        color.NRGBA{R: 130, G: 50, B: 180, A: 200},
	AxisText:        color.NRGBA{R: 60, G: 60, B: 60, A: 255},
	GridLine:        color.NRGBA{R: 200, G: 200, B: 200, A: 255},
	PanelText:       color.NRGBA{R: 40, G: 40, B: 40, A: 255},
	PanelBackground: color.NRGBA{R: 225, G: 225, B: 225, A: 255},
	BorderColor:     color.NRGBA{R: 180, G: 180, B: 180, A: 255},
	ButtonText:      color.NRGBA{R: 30, G: 30, B: 30, A: 255},
	ButtonFace:      color.NRGBA{R: 210, G: 210, B: 210, A: 255},
	DownloadLabel:   color.NRGBA{R: 180, G: 30, B: 30, A: 255},
	UploadLabel:     color.NRGBA{R: 20, G: 140, B: 20, A: 255},
}
