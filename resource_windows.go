//go:build windows

// Package main — Windows resource wiring for Tidemark.
//
// Running `go generate` invokes goversioninfo, which reads versioninfo.json
// (and the tidemark.ico it points at) and emits resource_windows.syso.
// Because of the _windows.syso suffix, the Go toolchain links it ONLY into
// Windows builds automatically — no extra build flags required.
//
// Place this file (plus versioninfo.json and tidemark.ico) in your main
// package directory.
package main

//go:generate goversioninfo -64 -o resource_windows.syso versioninfo.json
