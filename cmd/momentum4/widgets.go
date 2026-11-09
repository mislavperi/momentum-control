package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
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

// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
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
