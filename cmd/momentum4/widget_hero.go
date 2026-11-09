package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// heroArt is the illustration on the overview: the headset inside two hairline
// rings over a soft violet glow.
type heroArt struct {
	widget.BaseWidget
	active bool
}

func newHeroArt() *heroArt {
	h := &heroArt{}
	h.ExtendBaseWidget(h)
	return h
}

// SetActive brightens the glow while noise control is engaged.
func (h *heroArt) SetActive(a bool) {
	if h.active != a {
		h.active = a
		h.Refresh()
	}
}

func (h *heroArt) CreateRenderer() fyne.WidgetRenderer {
	outer := canvas.NewCircle(color.Transparent)
	outer.StrokeColor = colorRing
	outer.StrokeWidth = 1
	inner := canvas.NewCircle(color.Transparent)
	inner.StrokeColor = color.NRGBA{R: 0x8A, G: 0x6A, B: 0x48, A: 0x38}
	inner.StrokeWidth = 1
	glow := canvas.NewRadialGradient(color.NRGBA{R: 0x8B, G: 0x5C, B: 0xF6, A: 0x40}, color.Transparent)
	img := canvas.NewImageFromResource(headphonesArt)
	img.FillMode = canvas.ImageFillContain
	r := &heroRenderer{h: h, glow: glow, outer: outer, inner: inner, img: img}
	r.Refresh()
	return r
}

type heroRenderer struct {
	h            *heroArt
	glow         *canvas.RadialGradient
	outer, inner *canvas.Circle
	img          *canvas.Image
}

func (r *heroRenderer) Layout(size fyne.Size) {
	d := fyne.Min(size.Width, size.Height)
	place := func(o fyne.CanvasObject, dd float32) {
		o.Move(fyne.NewPos((size.Width-dd)/2, (size.Height-dd)/2))
		o.Resize(fyne.NewSize(dd, dd))
	}
	place(r.glow, d)
	place(r.outer, d)
	place(r.inner, d*0.74)
	place(r.img, d*0.86)
}

func (r *heroRenderer) MinSize() fyne.Size { return fyne.NewSize(260, 260) }

func (r *heroRenderer) Refresh() {
	if r.h.active {
		r.glow.StartColor = color.NRGBA{R: 0x8B, G: 0x5C, B: 0xF6, A: 0x48}
	} else {
		r.glow.StartColor = color.NRGBA{R: 0x8B, G: 0x5C, B: 0xF6, A: 0x14}
	}
	r.glow.Refresh()
}

func (r *heroRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.glow, r.outer, r.inner, r.img}
}
func (r *heroRenderer) Destroy() {}
