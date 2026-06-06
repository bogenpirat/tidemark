//go:build !windows

package main

import "gioui.org/io/event"

func onPlatformEvent(e event.Event) {}
