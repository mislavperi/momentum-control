package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// hoverTap is the shared plumbing for the custom tappable widgets.
type hoverTap struct {
	hovered bool
	onTap   func()
	refresh func()
}

func (h *hoverTap) Tapped(*fyne.PointEvent) {
	if h.onTap != nil {
		h.onTap()
	}
}
func (h *hoverTap) MouseIn(*desktop.MouseEvent)    { h.hovered = true; h.refresh() }
func (h *hoverTap) MouseMoved(*desktop.MouseEvent) {}
func (h *hoverTap) MouseOut()                      { h.hovered = false; h.refresh() }
func (h *hoverTap) Cursor() desktop.Cursor         { return desktop.PointerCursor }

// navItem is one entry of the sidebar.
type navItem struct {
	widget.BaseWidget
	hoverTap
	iconName string
	label    string
	selected bool
}

func newNavItem(iconName, label string, onTap func()) *navItem {
	n := &navItem{iconName: iconName, label: label}
	n.onTap = onTap
	n.refresh = n.Refresh
	n.ExtendBaseWidget(n)
	return n
}

func (n *navItem) SetSelected(s bool) {
	if n.selected != s {
		n.selected = s
		n.Refresh()
	}
}

func (n *navItem) CreateRenderer() fyne.WidgetRenderer {
	r := &navRenderer{
		item: n,
		bg:   canvas.NewRectangle(color.Transparent),
		bar:  canvas.NewRectangle(colorAccent),
		img:  iconImage(icon(n.iconName, colorTextMuted), 18),
		lbl:  text(n.label, 13, colorTextMuted, false),
	}
	r.bg.CornerRadius = 12
	r.bar.CornerRadius = 2
	r.Refresh()
	return r
}

type navRenderer struct {
	item *navItem
	bg   *canvas.Rectangle
	bar  *canvas.Rectangle
	img  *canvas.Image
	lbl  *canvas.Text
}

const navHeight = 46

func (r *navRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bar.Move(fyne.NewPos(0, 12))
	r.bar.Resize(fyne.NewSize(3, size.Height-24))
	r.img.Move(fyne.NewPos(18, (size.Height-18)/2))
	r.img.Resize(fyne.NewSize(18, 18))
	ls := r.lbl.MinSize()
	r.lbl.Move(fyne.NewPos(50, (size.Height-ls.Height)/2))
	r.lbl.Resize(ls)
}

func (r *navRenderer) MinSize() fyne.Size {
	return fyne.NewSize(60+r.lbl.MinSize().Width, navHeight)
}

func (r *navRenderer) Refresh() {
	n := r.item
	fg := colorTextMuted
	switch {
	case n.selected:
		r.bg.FillColor = colorSelected
		r.bg.StrokeColor = color.NRGBA{R: 0x3A, G: 0x2E, B: 0x70, A: 0xFF}
		r.bg.StrokeWidth = 1
		fg = colorText
	case n.hovered:
		r.bg.FillColor = colorHover
		r.bg.StrokeWidth = 0
		fg = colorText
	default:
		r.bg.FillColor = color.Transparent
		r.bg.StrokeWidth = 0
	}
	if n.selected {
		r.bar.Show()
		r.img.Resource = icon(n.iconName, colorAccentSoft)
	} else {
		r.bar.Hide()
		r.img.Resource = icon(n.iconName, fg)
	}
	r.lbl.Color = fg
	r.lbl.TextStyle.Bold = n.selected
	r.bg.Refresh()
	r.img.Refresh()
	r.lbl.Refresh()
}

func (r *navRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.bar, r.img, r.lbl}
}
func (r *navRenderer) Destroy() {}
