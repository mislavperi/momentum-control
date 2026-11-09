package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// toggle is an on/off switch.
type toggle struct {
	widget.BaseWidget
	hoverTap
	on        bool
	OnChanged func(bool)
}

func newToggle(onChanged func(bool)) *toggle {
	t := &toggle{OnChanged: onChanged}
	t.onTap = func() {
		t.on = !t.on
		t.Refresh()
		if t.OnChanged != nil {
			t.OnChanged(t.on)
		}
	}
	t.refresh = t.Refresh
	t.ExtendBaseWidget(t)
	return t
}

// SetOn sets the state without firing OnChanged, for reflecting device state.
func (t *toggle) SetOn(on bool) {
	if t.on != on {
		t.on = on
		t.Refresh()
	}
}

func (t *toggle) CreateRenderer() fyne.WidgetRenderer {
	r := &toggleRenderer{t: t, track: canvas.NewRectangle(colorControl), knob: canvas.NewCircle(colorTextMuted)}
	r.track.StrokeColor = colorBorder
	r.track.StrokeWidth = 1
	r.Refresh()
	return r
}

type toggleRenderer struct {
	t     *toggle
	track *canvas.Rectangle
	knob  *canvas.Circle
}

func (r *toggleRenderer) Layout(size fyne.Size) {
	const w, h = 46, 26
	y := (size.Height - h) / 2
	x := size.Width - w
	r.track.Move(fyne.NewPos(x, y))
	r.track.Resize(fyne.NewSize(w, h))
	r.track.CornerRadius = h / 2
	kx := x + 4
	if r.t.on {
		kx = x + w - 22
	}
	r.knob.Move(fyne.NewPos(kx, y+4))
	r.knob.Resize(fyne.NewSize(18, 18))
}

func (r *toggleRenderer) MinSize() fyne.Size { return fyne.NewSize(46, 26) }

func (r *toggleRenderer) Refresh() {
	if r.t.on {
		r.track.FillColor = colorAccent
		r.track.StrokeColor = colorAccent
		r.knob.FillColor = colorText
	} else {
		r.track.FillColor = colorControl
		r.track.StrokeColor = colorBorder
		r.knob.FillColor = colorTextMuted
	}
	r.Layout(r.t.Size())
	r.track.Refresh()
	r.knob.Refresh()
}

func (r *toggleRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.track, r.knob} }
func (r *toggleRenderer) Destroy()                     {}
