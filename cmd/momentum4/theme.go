package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Palette for the dark control panel. Kept in one place so the cards, buttons
// and text stay in step.
var (
	colorBackground = color.NRGBA{R: 0x0F, G: 0x0F, B: 0x12, A: 0xFF}
	colorCard       = color.NRGBA{R: 0x1A, G: 0x1A, B: 0x20, A: 0xFF}
	colorControl    = color.NRGBA{R: 0x26, G: 0x26, B: 0x2E, A: 0xFF}
	colorAccent     = color.NRGBA{R: 0x7F, G: 0x6B, B: 0xEE, A: 0xFF}
	colorText       = color.NRGBA{R: 0xF2, G: 0xF2, B: 0xF5, A: 0xFF}
	colorTextMuted  = color.NRGBA{R: 0x8E, G: 0x8E, B: 0x9A, A: 0xFF}
	colorDanger     = color.NRGBA{R: 0xE5, G: 0x6B, B: 0x6B, A: 0xFF}
)

// panelTheme is the built-in dark theme with the palette above applied, so the
// stock widgets match the custom ones.
type panelTheme struct{ fyne.Theme }

func newPanelTheme() fyne.Theme {
	return &panelTheme{Theme: theme.DefaultTheme()}
}

func (t *panelTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colorBackground
	case theme.ColorNameForeground:
		return colorText
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return colorTextMuted
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameSelection:
		return colorAccent
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return colorControl
	case theme.ColorNameError:
		return colorDanger
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 0x2E, G: 0x2E, B: 0x36, A: 0xFF}
	case theme.ColorNameHover:
		return color.NRGBA{R: 0x33, G: 0x33, B: 0x3D, A: 0xFF}
	case theme.ColorNameShadow:
		return color.NRGBA{A: 0x66}
	}
	return t.Theme.Color(name, theme.VariantDark)
}

func (t *panelTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 10
	}
	return t.Theme.Size(name)
}
