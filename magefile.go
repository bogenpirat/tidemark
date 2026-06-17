//go:build mage

// Build orchestration for Tidemark. Run `mage` (no args) for a release build,
// or `mage <target>`; list targets with `mage -l`.
//
// Requires the mage binary: go install github.com/magefile/mage@latest
package main

import (
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const binary = "tidemark.exe"

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

// Clean removes build artifacts (the binary and the generated .syso).
func Clean() error {
	for _, f := range []string{binary, "resource_windows.syso"} {
		if err := sh.Rm(f); err != nil {
			return err
		}
	}
	return nil
}
