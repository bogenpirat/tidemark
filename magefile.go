//go:build mage

// Build orchestration for Tidemark. Run `mage` (no args) for a release build,
// or `mage <target>`; list targets with `mage -l`.
//
// Requires the mage binary: go install github.com/magefile/mage@latest
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const binary = "tidemark.exe"

// macOS .app bundle layout produced by the Mac target.
const (
	macAppName = "Tidemark"
	macBundle  = macAppName + ".app"
)

const macInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Tidemark</string>
	<key>CFBundleDisplayName</key><string>Tidemark</string>
	<key>CFBundleIdentifier</key><string>org.tidemark.app</string>
	<key>CFBundleExecutable</key><string>tidemark</string>
	<key>CFBundleIconFile</key><string>tidemark.icns</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>1.0.0</string>
	<key>LSMinimumSystemVersion</key><string>10.14</string>
	<key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
`

// Default is the target run when `mage` is invoked with no arguments.
var Default = Release

// Generate rebuilds resource_windows.syso from versioninfo.json + tidemark.ico
// by running goversioninfo via `go generate`.
func Generate() error {
	return sh.RunV("go", "generate", "./...")
}

// Debug builds an unoptimized binary with the console window attached.
func Debug() error {
	mg.Deps(Generate)
	return sh.RunV("go", "build", "-o", binary, ".")
}

// Release builds the optimized, windowless production binary.
func Release() error {
	mg.Deps(Generate)
	return sh.RunV("go", "build", "-ldflags", "-s -w -H windowsgui", "-o", binary, ".")
}

// Icns generates tidemark.icns from assets/tidemark.svg. macOS only: it shells
// out to rsvg-convert (librsvg) to rasterize the SVG and to the system sips and
// iconutil tools to assemble the .icns. Install the one non-system dependency
// with: brew install librsvg
func Icns() error {
	const src = "assets/tidemark.svg"
	const master = "tidemark-icon-master.png"
	const iconset = "tidemark.iconset"

	if err := os.MkdirAll(iconset, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(iconset)
	defer os.Remove(master)

	// Rasterize a single high-resolution master, then downscale to each size
	// the .iconset format requires (1x and 2x for 16/32/128/256/512 pt).
	if err := sh.RunV("rsvg-convert", "-w", "1024", "-h", "1024", "-o", master, src); err != nil {
		return err
	}
	sizes := []struct {
		name string
		px   int
	}{
		{"icon_16x16.png", 16}, {"icon_16x16@2x.png", 32},
		{"icon_32x32.png", 32}, {"icon_32x32@2x.png", 64},
		{"icon_128x128.png", 128}, {"icon_128x128@2x.png", 256},
		{"icon_256x256.png", 256}, {"icon_256x256@2x.png", 512},
		{"icon_512x512.png", 512}, {"icon_512x512@2x.png", 1024},
	}
	for _, s := range sizes {
		out := filepath.Join(iconset, s.name)
		if err := sh.RunV("sips", "-z", fmt.Sprint(s.px), fmt.Sprint(s.px), master, "--out", out); err != nil {
			return err
		}
	}
	return sh.RunV("iconutil", "-c", "icns", "-o", "tidemark.icns", iconset)
}

// Mac builds a macOS .app bundle (run on macOS; needs the Xcode command-line
// tools for cgo/Cocoa, plus librsvg for the icon — see Icns). Cgo is required,
// so do not set CGO_ENABLED=0. Because Tidemark needs a config-file argument it
// is launched like the Windows build, e.g.:
//
//	./Tidemark.app/Contents/MacOS/tidemark my-router.json
//	open -a Tidemark --args my-router.json
func Mac() error {
	mg.Deps(Icns)

	macOSDir := filepath.Join(macBundle, "Contents", "MacOS")
	resDir := filepath.Join(macBundle, "Contents", "Resources")
	for _, d := range []string{macOSDir, resDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	binPath := filepath.Join(macOSDir, "tidemark")
	if err := sh.RunV("go", "build", "-ldflags", "-s -w", "-o", binPath, "."); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(macBundle, "Contents", "Info.plist"),
		[]byte(macInfoPlist), 0o644); err != nil {
		return err
	}
	icns, err := os.ReadFile("tidemark.icns")
	if err != nil {
		return fmt.Errorf("read generated icon: %w", err)
	}
	return os.WriteFile(filepath.Join(resDir, "tidemark.icns"), icns, 0o644)
}

// Clean removes build artifacts (the binary, the generated .syso, the .app, and
// the generated macOS icon).
func Clean() error {
	for _, f := range []string{binary, "resource_windows.syso", "tidemark.icns"} {
		if err := sh.Rm(f); err != nil {
			return err
		}
	}
	return os.RemoveAll(macBundle)
}
