package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// card wraps content in a rounded panel with a heading, the grouping used
// throughout the window.
func card(title string, content fyne.CanvasObject) fyne.CanvasObject {
	background := canvas.NewRectangle(colorCard)
	background.CornerRadius = 14

	heading := canvas.NewText(title, colorText)
	heading.TextStyle = fyne.TextStyle{Bold: true}
	heading.TextSize = 15

	body := container.NewVBox(heading, content)
	return container.NewStack(background, container.NewPadded(body))
}

// modeSize governs the circular mode buttons.
const (
	modeCircle  = 64
	modeSpacing = 6
)

// modeButton is a large circular toggle, as used for the sound modes. Fyne has
// no equivalent: widget.Button is rectangular and cannot show a selected state
// this way.
type modeButton struct {
	widget.BaseWidget

	symbol   string
	caption  string
	selected bool
	onTap    func()
}

func newModeButton(symbol, caption string, onTap func()) *modeButton {
	b := &modeButton{symbol: symbol, caption: caption, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

// SetSelected marks the button as the active mode.
func (b *modeButton) SetSelected(selected bool) {
	if b.selected == selected {
		return
	}
	b.selected = selected
	b.Refresh()
}

func (b *modeButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *modeButton) CreateRenderer() fyne.WidgetRenderer {
	circle := canvas.NewCircle(colorControl)

	symbol := canvas.NewText(b.symbol, colorText)
	symbol.TextSize = 24
	symbol.Alignment = fyne.TextAlignCenter

	caption := canvas.NewText(b.caption, colorTextMuted)
	caption.TextSize = 12
	caption.Alignment = fyne.TextAlignCenter

	r := &modeRenderer{button: b, circle: circle, symbol: symbol, caption: caption}
	r.Refresh()
	return r
}

type modeRenderer struct {
	button  *modeButton
	circle  *canvas.Circle
	symbol  *canvas.Text
	caption *canvas.Text
}

func (r *modeRenderer) Layout(size fyne.Size) {
	x := (size.Width - modeCircle) / 2

	r.circle.Move(fyne.NewPos(x, 0))
	r.circle.Resize(fyne.NewSize(modeCircle, modeCircle))

	// canvas.Text draws from its top-left, so centre it inside the circle.
	symbolSize := r.symbol.MinSize()
	r.symbol.Move(fyne.NewPos(0, (modeCircle-symbolSize.Height)/2))
	r.symbol.Resize(fyne.NewSize(size.Width, symbolSize.Height))

	captionSize := r.caption.MinSize()
	r.caption.Move(fyne.NewPos(0, modeCircle+modeSpacing))
	r.caption.Resize(fyne.NewSize(size.Width, captionSize.Height))
}

func (r *modeRenderer) MinSize() fyne.Size {
	width := fyne.Max(modeCircle, r.caption.MinSize().Width)
	return fyne.NewSize(width, modeCircle+modeSpacing+r.caption.MinSize().Height)
}

func (r *modeRenderer) Refresh() {
	if r.button.selected {
		r.circle.FillColor = colorAccent
		r.symbol.Color = colorText
		r.caption.Color = colorText
		r.caption.TextStyle = fyne.TextStyle{Bold: true}
	} else {
		r.circle.FillColor = colorControl
		r.symbol.Color = colorTextMuted
		r.caption.Color = colorTextMuted
		r.caption.TextStyle = fyne.TextStyle{}
	}
	r.circle.Refresh()
	r.symbol.Refresh()
	r.caption.Refresh()
}

func (r *modeRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.circle, r.symbol, r.caption}
}

func (r *modeRenderer) Destroy() {}

// heading builds the large device title shown at the top of the window.
func heading(text string, size float32, col color.Color, bold bool) *canvas.Text {
	t := canvas.NewText(text, col)
	t.TextSize = size
	t.Alignment = fyne.TextAlignCenter
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}
