//go:build !windows

package main

import (
	"image"

	"gioui.org/app"
	"gioui.org/io/event"
)

func onPlatformEvent(win *app.Window, e event.Event) {}

func TakeRightClick() (bool, image.Point) { return false, image.Point{} }
