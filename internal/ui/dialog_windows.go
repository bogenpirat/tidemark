//go:build windows

package ui

import (
	"os/exec"
	"strings"
)

// showSaveDialog opens the Windows file-save common dialog via PowerShell
// and returns the chosen path, or "" if the user cancelled.
func showSaveDialog() string {
	script := `Add-Type -AssemblyName System.Windows.Forms; ` +
		`$d = New-Object System.Windows.Forms.SaveFileDialog; ` +
		`$d.Filter = 'JSON Files (*.json)|*.json|All Files (*.*)|*.*'; ` +
		`$d.DefaultExt = 'json'; ` +
		`$d.Title = 'Save configuration as'; ` +
		`if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $d.FileName }`
	out, err := exec.Command(
		"powershell", "-NonInteractive", "-NoProfile", "-Command", script,
	).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
