//go:build !windows && !darwin

// Package main — fallback platform stubs for operating systems without a
// dedicated implementation (Linux/X11/Wayland, BSD, etc.).
//
// Window position save/restore and right-click-in-drag-region detection are
// implemented natively only on Windows (platform_windows.go) and macOS
// (platform_darwin.go). On other platforms these are no-ops; see README.md for
// the list of behaviors that are platform-specific.
package main

import (
	"image"
	"os"

	"gioui.org/app"
	giofont "gioui.org/font"
	gioopentype "gioui.org/font/opentype"
	"gioui.org/io/event"
)

func onPlatformEvent(win *app.Window, e event.Event) {}

func TakeRightClick() (bool, image.Point) { return false, image.Point{} }

func SetInitialWindowPos(x, y int) {}

func GetWindowPosition() (x int, y int, ok bool) { return 0, 0, false }

// loadSymbolFontFaces returns extra font faces providing Unicode symbol/emoji
// glyphs (notably U+1F4A1 💡, used by the theme-toggle button). Best-effort: it
// tries a few common Linux symbol fonts and returns nil if none are available,
// in which case the glyph renders as a missing-glyph box.
func loadSymbolFontFaces() []giofont.FontFace {
	candidates := []string{
		"/usr/share/fonts/truetype/noto/NotoSansSymbols2-Regular.ttf",
		"/usr/share/fonts/truetype/ancient-scripts/Symbola_hint.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf",
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		faces, err := gioopentype.ParseCollection(data)
		if err != nil {
			continue
		}
		return faces
	}
	return nil
}
