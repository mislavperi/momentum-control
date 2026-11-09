package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

// text builds a canvas.Text in one call.
func text(s string, size float32, col color.Color, bold bool) *canvas.Text {
	t := canvas.NewText(s, col)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}

// eyebrow is the small spaced-out uppercase label used above headings.
func eyebrow(s string) *canvas.Text {
	return text(spaced(s), 10, colorTextMuted, true)
}

// spaced letter-spaces a label, which canvas.Text cannot do itself.
func spaced(s string) string {
	out := make([]rune, 0, len(s)*2)
	for i, r := range s {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	return string(out)
}

// panel is a rounded, hairline-bordered surface.
func panel(fill color.Color, radius float32) *canvas.Rectangle {
	r := canvas.NewRectangle(fill)
	r.CornerRadius = radius
	r.StrokeColor = colorBorder
	r.StrokeWidth = 1
	return r
}

// card wraps content in the standard panel with generous padding.
func card(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(panel(colorCard, 20), padded(content, 22))
}

// titledCard is a card with a heading and an optional caption under it.
func titledCard(title, caption string, content fyne.CanvasObject) fyne.CanvasObject {
	head := container.NewVBox(text(title, 15, colorText, true))
	if caption != "" {
		head.Add(text(caption, 12, colorTextMuted, false))
	}
	return card(container.NewVBox(head, spacer(8), content))
}

// padded insets content by p on every side.
func padded(content fyne.CanvasObject, p float32) fyne.CanvasObject {
	return container.New(&insetLayout{p}, content)
}

type insetLayout struct{ p float32 }

func (l *insetLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(l.p, l.p))
		o.Resize(fyne.NewSize(size.Width-2*l.p, size.Height-2*l.p))
	}
}

func (l *insetLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var m fyne.Size
	for _, o := range objs {
		m = m.Max(o.MinSize())
	}
	return m.Add(fyne.NewSize(2*l.p, 2*l.p))
}

// spacer is a fixed vertical gap.
func spacer(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(1, h))
	return r
}

// iconImage renders an icon resource at a fixed size.
func iconImage(res fyne.Resource, size float32) *canvas.Image {
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(size, size))
	return img
}

// dot is a small status light.
func dot(col color.Color, size float32) *canvas.Circle {
	c := canvas.NewCircle(col)
	c.Resize(fyne.NewSize(size, size))
	return c
}

// fixed gives an object an exact size inside flowing layouts.
func fixed(o fyne.CanvasObject, w, h float32) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(w, h), o)
}

// row lays out a label on the left and a control on the right.
func row(iconName, title, caption string, control fyne.CanvasObject) fyne.CanvasObject {
	words := container.NewVBox(text(title, 13, colorText, true))
	if caption != "" {
		words.Add(text(caption, 11, colorTextMuted, false))
	}
	left := container.NewHBox(container.NewCenter(iconImage(icon(iconName, colorAccentSoft), 20)), spacerW(6), words)
	return container.NewBorder(nil, nil, left, container.NewCenter(control))
}

func spacerW(w float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(w, 1))
	return r
}

// centered places objects in a horizontal row centred in the available width.
func centered(objs ...fyne.CanvasObject) fyne.CanvasObject {
	items := []fyne.CanvasObject{layout.NewSpacer()}
	for i, o := range objs {
		if i > 0 {
			items = append(items, spacerW(6))
		}
		items = append(items, o)
	}
	items = append(items, layout.NewSpacer())
	return container.NewHBox(items...)
}
