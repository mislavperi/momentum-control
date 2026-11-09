package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// pill is a rounded button with an optional icon, used for the sound modes and
// for small segmented choices.
type pill struct {
	widget.BaseWidget
	hoverTap
	iconName string
	label    string
	selected bool
	compact  bool
}

func newPill(iconName, label string, onTap func()) *pill {
	p := &pill{iconName: iconName, label: label}
	p.onTap = onTap
	p.refresh = p.Refresh
	p.ExtendBaseWidget(p)
	return p
}

func (p *pill) SetSelected(s bool) {
	if p.selected != s {
		p.selected = s
		p.Refresh()
	}
}

// SetLabel changes the caption, used by pills that show live values.
func (p *pill) SetLabel(s string) {
	if p.label != s {
		p.label = s
		p.Refresh()
	}
}

func (p *pill) CreateRenderer() fyne.WidgetRenderer {
	r := &pillRenderer{p: p, bg: panel(colorControl, 0), lbl: text(p.label, 13, colorTextMuted, false)}
	if p.iconName != "" {
		r.img = iconImage(icon(p.iconName, colorTextMuted), 17)
	}
	r.Refresh()
	return r
}

type pillRenderer struct {
	p   *pill
	bg  *canvas.Rectangle
	img *canvas.Image
	lbl *canvas.Text
}

func (r *pillRenderer) height() float32 {
	if r.p.compact {
		return 34
	}
	return 46
}

func (r *pillRenderer) contentWidth() float32 {
	w := r.lbl.MinSize().Width
	if r.img != nil {
		w += 17 + 10
	}
	return w
}

func (r *pillRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.CornerRadius = size.Height / 2
	x := (size.Width - r.contentWidth()) / 2
	if r.img != nil {
		r.img.Move(fyne.NewPos(x, (size.Height-17)/2))
		r.img.Resize(fyne.NewSize(17, 17))
		x += 27
	}
	ls := r.lbl.MinSize()
	r.lbl.Move(fyne.NewPos(x, (size.Height-ls.Height)/2))
	r.lbl.Resize(ls)
}

func (r *pillRenderer) MinSize() fyne.Size {
	pad := float32(44)
	if r.p.compact {
		pad = 30
	}
	return fyne.NewSize(r.contentWidth()+pad, r.height())
}

func (r *pillRenderer) Refresh() {
	p := r.p
	fg := colorTextMuted
	switch {
	case p.selected:
		r.bg.FillColor = colorSelected
		r.bg.StrokeColor = colorAccent
		fg = colorText
	case p.hovered:
		r.bg.FillColor = colorHover
		r.bg.StrokeColor = color.NRGBA{R: 0x34, G: 0x34, B: 0x52, A: 0xFF}
		fg = colorText
	default:
		r.bg.FillColor = colorControl
		r.bg.StrokeColor = colorBorder
	}
	r.lbl.Text = p.label
	r.lbl.Color = fg
	r.lbl.TextStyle.Bold = p.selected
	r.lbl.TextSize = 13
	if p.compact {
		r.lbl.TextSize = 12
	}
	if r.img != nil {
		ic := fg
		if p.selected {
			ic = colorAccentSoft
		}
		r.img.Resource = icon(p.iconName, ic)
		r.img.Refresh()
	}
	r.bg.Refresh()
	r.lbl.Refresh()
}

func (r *pillRenderer) Objects() []fyne.CanvasObject {
	if r.img != nil {
		return []fyne.CanvasObject{r.bg, r.img, r.lbl}
	}
	return []fyne.CanvasObject{r.bg, r.lbl}
}
func (r *pillRenderer) Destroy() {}
