package ui

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"tidemark/internal/config"
)

// DialogResult is the outcome of the settings dialog.
type DialogResult struct {
	Saved  bool
	Config config.AppConfig
}

const (
	dlgLabelWidthDp  = 140
	dlgFieldHeightDp = 26
	dlgFieldPadDp    = 4
	dlgRowGapDp      = 7
	dlgOuterPadDp    = 16
	dlgBtnHeightDp   = 30
	dlgBtnWidthDp    = 88
	dlgBtnGapDp      = 8
)

var (
	dlgErrorColor = color.NRGBA{R: 210, G: 50, B: 50, A: 255}
	dlgNoteColor  = color.NRGBA{R: 130, G: 130, B: 130, A: 255}
)

type dialogAction int

const (
	dlgNone   dialogAction = iota
	dlgSave
	dlgCancel
)

type settingsDialog struct {
	mat     *material.Theme
	theme   *Theme
	closing bool
	errors  []string

	hosts       widget.Editor
	community   widget.Editor
	port        widget.Editor
	snmpVersion widget.Editor
	ifIndex     widget.Editor
	dlOID       widget.Editor
	ulOID       widget.Editor
	timeoutMs   widget.Editor
	retries     widget.Editor

	saveBtn   widget.Clickable
	cancelBtn widget.Clickable
}

func newSettingsDialog(mat *material.Theme, isDark bool, cfg config.AppConfig) *settingsDialog {
	th := &LightTheme
	if isDark {
		th = &DarkTheme
	}
	d := &settingsDialog{mat: mat, theme: th}
	for _, ed := range []*widget.Editor{
		&d.hosts, &d.community, &d.port, &d.snmpVersion, &d.ifIndex,
		&d.dlOID, &d.ulOID, &d.timeoutMs, &d.retries,
	} {
		ed.SingleLine = true
	}
	d.hosts.SetText(cfg.Host)
	d.community.SetText(cfg.Community)
	d.port.SetText(fmt.Sprintf("%d", cfg.Port))
	d.snmpVersion.SetText(cfg.SNMPVersion)
	d.ifIndex.SetText(fmt.Sprintf("%d", cfg.InterfaceIndex))
	d.dlOID.SetText(cfg.DownloadOID)
	d.ulOID.SetText(cfg.UploadOID)
	d.timeoutMs.SetText(fmt.Sprintf("%d", cfg.TimeoutMs))
	d.retries.SetText(fmt.Sprintf("%d", cfg.Retries))
	return d
}

func (d *settingsDialog) validate() (config.AppConfig, []string) {
	var errs []string
	var cfg config.AppConfig

	host := strings.TrimSpace(d.hosts.Text())
	if host == "" {
		errs = append(errs, "Host: required")
	}
	cfg.Host = host

	comm := strings.TrimSpace(d.community.Text())
	if comm == "" {
		errs = append(errs, "Community: required")
	}
	cfg.Community = comm

	if p, err := strconv.ParseUint(strings.TrimSpace(d.port.Text()), 10, 16); err != nil || p == 0 {
		errs = append(errs, "Port: must be 1–65535")
	} else {
		cfg.Port = uint16(p)
	}

	ver := strings.TrimSpace(d.snmpVersion.Text())
	if ver != "1" && ver != "2c" {
		errs = append(errs, "SNMP Version: must be \"1\" or \"2c\"")
	} else {
		cfg.SNMPVersion = ver
	}

	if idx, err := strconv.Atoi(strings.TrimSpace(d.ifIndex.Text())); err != nil || idx < 1 {
		errs = append(errs, "Interface Index: must be ≥ 1")
	} else {
		cfg.InterfaceIndex = idx
	}

	dlOID := strings.TrimSpace(d.dlOID.Text())
	if !isValidOID(dlOID) {
		errs = append(errs, "Download OID: must be dotted-numeric (e.g. 1.3.6.1.2.1.31.1.1.1.6.1)")
	} else {
		cfg.DownloadOID = dlOID
	}

	ulOID := strings.TrimSpace(d.ulOID.Text())
	if !isValidOID(ulOID) {
		errs = append(errs, "Upload OID: must be dotted-numeric (e.g. 1.3.6.1.2.1.31.1.1.1.10.1)")
	} else {
		cfg.UploadOID = ulOID
	}

	if ms, err := strconv.Atoi(strings.TrimSpace(d.timeoutMs.Text())); err != nil || ms <= 0 {
		errs = append(errs, "Timeout (ms): must be > 0")
	} else {
		cfg.TimeoutMs = ms
	}

	if r, err := strconv.Atoi(strings.TrimSpace(d.retries.Text())); err != nil || r < 0 {
		errs = append(errs, "Retries: must be ≥ 0")
	} else {
		cfg.Retries = r
	}

	return cfg, errs
}

func isValidOID(s string) bool {
	if s == "" {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.ParseUint(p, 10, 64); err != nil {
			return false
		}
	}
	return true
}

// Layout renders the dialog form and returns the action triggered this frame.
func (d *settingsDialog) Layout(gtx layout.Context) dialogAction {
	fillRect(gtx.Ops, d.theme.Background,
		image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y))

	if d.closing {
		return dlgNone
	}

	// Process button clicks (from previous frame's layout registrations).
	var action dialogAction
	for d.saveBtn.Clicked(gtx) {
		action = dlgSave
	}
	for d.cancelBtn.Clicked(gtx) {
		action = dlgCancel
	}

	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgOuterPadDp)}.Layout),
		layout.Rigid(d.fieldRow("Host", &d.hosts, "e.g., 192.168.1.1")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgRowGapDp)}.Layout),
		layout.Rigid(d.fieldRow("Community", &d.community, "e.g., public")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgRowGapDp)}.Layout),
		layout.Rigid(d.fieldRow("Port", &d.port, "1–65535")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgRowGapDp)}.Layout),
		layout.Rigid(d.fieldRow("SNMP Version", &d.snmpVersion, "1 or 2c")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgRowGapDp)}.Layout),
		layout.Rigid(d.fieldRow("Interface Index", &d.ifIndex, "e.g., 1")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgRowGapDp)}.Layout),
		layout.Rigid(d.fieldRow("Download OID", &d.dlOID, "1.3.6.1.2.1.31.1.1.1.6.x")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgRowGapDp)}.Layout),
		layout.Rigid(d.fieldRow("Upload OID", &d.ulOID, "1.3.6.1.2.1.31.1.1.1.10.x")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgRowGapDp)}.Layout),
		layout.Rigid(d.fieldRow("Timeout (ms)", &d.timeoutMs, "e.g., 3000")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgRowGapDp)}.Layout),
		layout.Rigid(d.fieldRow("Retries", &d.retries, "e.g., 1")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
		layout.Rigid(d.errorSection),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Max.Y)}
		}),
		layout.Rigid(d.noteRow),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(d.buttonRow),
		layout.Rigid(layout.Spacer{Height: unit.Dp(dlgOuterPadDp)}.Layout),
	)

	return action
}

// fieldRow returns a layout.Widget that renders a right-aligned label and a
// bordered single-line text editor.
func (d *settingsDialog) fieldRow(label string, ed *widget.Editor, hint string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(layout.Spacer{Width: unit.Dp(dlgOuterPadDp)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(dlgLabelWidthDp), gtx.Dp(dlgFieldHeightDp)))
				lbl := material.Label(d.mat, unit.Sp(12), label+":")
				lbl.Color = d.theme.PanelText
				lbl.Alignment = text.End
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return d.editorField(gtx, ed, hint)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(dlgOuterPadDp)}.Layout),
		)
	}
}

// editorField draws a bordered editor box and lays out the editor inside it.
func (d *settingsDialog) editorField(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	h := gtx.Dp(dlgFieldHeightDp)
	w := gtx.Constraints.Max.X

	fillRect(gtx.Ops, d.theme.BorderColor, image.Rect(0, 0, w, h))
	fillRect(gtx.Ops, d.theme.PanelBackground, image.Rect(1, 1, w-1, h-1))

	pad := gtx.Dp(dlgFieldPadDp)
	innerW := w - 2*pad
	innerH := h - 2*pad
	if innerW > 0 && innerH > 0 {
		offsetStack := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
		clipStack := clip.Rect(image.Rect(0, 0, innerW, innerH)).Push(gtx.Ops)

		edGtx := gtx
		edGtx.Constraints = layout.Exact(image.Pt(innerW, innerH))
		edStyle := material.Editor(d.mat, ed, hint)
		edStyle.Color = d.theme.PanelText
		edStyle.HintColor = d.theme.GridLine
		edStyle.TextSize = unit.Sp(12)
		edStyle.Layout(edGtx)

		clipStack.Pop()
		offsetStack.Pop()
	}

	return layout.Dimensions{Size: image.Pt(w, h)}
}

// errorSection renders any current validation errors, one per line.
func (d *settingsDialog) errorSection(gtx layout.Context) layout.Dimensions {
	if len(d.errors) == 0 {
		return layout.Dimensions{}
	}
	lineH := gtx.Dp(15)
	pad := gtx.Dp(dlgOuterPadDp)
	w := gtx.Constraints.Max.X - 2*pad
	var totalH int
	for _, msg := range d.errors {
		if w > 0 {
			offsetStack := op.Offset(image.Pt(pad, totalH)).Push(gtx.Ops)
			clipStack := clip.Rect(image.Rect(0, 0, w, lineH)).Push(gtx.Ops)
			subGtx := gtx
			subGtx.Constraints = layout.Exact(image.Pt(w, lineH))
			lbl := material.Label(d.mat, unit.Sp(11), "• "+msg)
			lbl.Color = dlgErrorColor
			lbl.Alignment = text.Start
			lbl.Layout(subGtx)
			clipStack.Pop()
			offsetStack.Pop()
		}
		totalH += lineH
	}
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, totalH)}
}

// noteRow renders a small note about SNMP settings requiring a restart.
func (d *settingsDialog) noteRow(gtx layout.Context) layout.Dimensions {
	lineH := gtx.Dp(15)
	pad := gtx.Dp(dlgOuterPadDp)
	w := gtx.Constraints.Max.X - 2*pad
	if w <= 0 {
		return layout.Dimensions{}
	}
	offsetStack := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
	clipStack := clip.Rect(image.Rect(0, 0, w, lineH)).Push(gtx.Ops)
	subGtx := gtx
	subGtx.Constraints = layout.Exact(image.Pt(w, lineH))
	lbl := material.Label(d.mat, unit.Sp(10), "SNMP settings take effect after restart.")
	lbl.Color = dlgNoteColor
	lbl.Alignment = text.End
	lbl.Layout(subGtx)
	clipStack.Pop()
	offsetStack.Pop()
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, lineH)}
}

// buttonRow renders Cancel / Save right-aligned at the bottom.
func (d *settingsDialog) buttonRow(gtx layout.Context) layout.Dimensions {
	btnH := gtx.Dp(dlgBtnHeightDp)
	btnW := gtx.Dp(dlgBtnWidthDp)
	gap := gtx.Dp(dlgBtnGapDp)
	pad := gtx.Dp(dlgOuterPadDp)
	totalBtnW := 2*btnW + gap
	startX := gtx.Constraints.Max.X - pad - totalBtnW

	type entry struct {
		btn   *widget.Clickable
		label string
	}
	for i, e := range []entry{
		{&d.cancelBtn, "Cancel"},
		{&d.saveBtn, "Save"},
	} {
		x := startX + i*(btnW+gap)
		d.renderButton(gtx, e.btn, e.label, x, 0, btnW, btnH)
	}
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, btnH)}
}

func (d *settingsDialog) renderButton(gtx layout.Context, btn *widget.Clickable, label string, x, y, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	offsetStack := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	clipStack := clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops)
	btnGtx := gtx
	btnGtx.Constraints = layout.Exact(image.Pt(w, h))
	b := material.Button(d.mat, btn, label)
	b.TextSize = unit.Sp(12)
	b.Background = d.theme.ButtonFace
	b.Color = d.theme.ButtonText
	b.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}
	b.Layout(btnGtx)
	clipStack.Pop()
	offsetStack.Pop()
}

// RunSettingsDialog opens a settings window, blocks until it is closed, and
// returns the user's choice. Safe to call from any goroutine.
func RunSettingsDialog(mat *material.Theme, cfg config.AppConfig, isDark bool) DialogResult {
	win := new(app.Window)
	win.Option(
		app.Title("Settings"),
		app.Size(unit.Dp(520), unit.Dp(493)),
	)

	d := newSettingsDialog(mat, isDark, cfg)

	var ops op.Ops
	var result DialogResult

	for {
		e := win.Event()
		switch ev := e.(type) {
		case app.DestroyEvent:
			return result
		case app.FrameEvent:
			gtx := app.NewContext(&ops, ev)
			action := d.Layout(gtx)
			ev.Frame(&ops)

			switch action {
			case dlgSave:
				parsed, errs := d.validate()
				if len(errs) > 0 {
					d.errors = errs
					win.Invalidate()
				} else {
					d.closing = true
					result = DialogResult{Saved: true, Config: parsed}
					win.Perform(system.ActionClose)
				}
			case dlgCancel:
				win.Perform(system.ActionClose)
			}
		}
	}
}
