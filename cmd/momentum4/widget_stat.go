package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// statChip is a small labelled readout in the header.
type statChip struct {
	obj   fyne.CanvasObject
	value *canvas.Text
}

func newStatChip(label string) *statChip {
	l := eyebrow(label)
	l.Alignment = fyne.TextAlignCenter
	v := text("—", 14, colorText, true)
	v.Alignment = fyne.TextAlignCenter
	body := container.NewVBox(l, v)
	obj := container.NewStack(panel(colorCard, 14), padded(body, 10))
	return &statChip{obj: fixed(obj, 150, 62), value: v}
}

func (c *statChip) Set(s string) {
	if s == "" {
		s = "—"
	}
	c.value.Text = s
	c.value.Refresh()
}
