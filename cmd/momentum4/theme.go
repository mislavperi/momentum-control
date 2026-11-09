package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Palette: deep navy surfaces with a violet accent. Kept in one place so the
// sidebar, cards, pills and stock widgets stay in step.
var (
	colorBackground = color.NRGBA{R: 0x0A, G: 0x0A, B: 0x12, A: 0xFF}
	colorBackdrop   = color.NRGBA{R: 0x12, G: 0x0E, B: 0x24, A: 0xFF} // top of the main gradient
	colorSidebar    = color.NRGBA{R: 0x0D, G: 0x0D, B: 0x17, A: 0xFF}
	colorCard       = color.NRGBA{R: 0x12, G: 0x12, B: 0x1D, A: 0xFF}
	colorBorder     = color.NRGBA{R: 0x24, G: 0x24, B: 0x3A, A: 0xFF}
	colorControl    = color.NRGBA{R: 0x16, G: 0x16, B: 0x23, A: 0xFF}
	colorHover      = color.NRGBA{R: 0x1C, G: 0x1C, B: 0x2C, A: 0xFF}
	colorSelected   = color.NRGBA{R: 0x22, G: 0x1B, B: 0x45, A: 0xFF}
	colorAccent     = color.NRGBA{R: 0x8B, G: 0x5C, B: 0xF6, A: 0xFF}
	colorAccentSoft = color.NRGBA{R: 0xB4, G: 0x9C, B: 0xFF, A: 0xFF}
	colorRing       = color.NRGBA{R: 0x8A, G: 0x6A, B: 0x48, A: 0x70} // warm hairline rings around the hero
	colorText       = color.NRGBA{R: 0xF3, G: 0xF3, B: 0xF7, A: 0xFF}
	colorTextMuted  = color.NRGBA{R: 0x8A, G: 0x8A, B: 0xA0, A: 0xFF}
	colorTextFaint  = color.NRGBA{R: 0x5E, G: 0x5E, B: 0x74, A: 0xFF}
	colorSuccess    = color.NRGBA{R: 0x34, G: 0xD3, B: 0x99, A: 0xFF}
	colorDanger     = color.NRGBA{R: 0xF0, G: 0x6A, B: 0x6A, A: 0xFF}
)

// panelTheme is the built-in dark theme with the palette above applied, so the
// stock widgets (sliders, buttons) match the custom ones.
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
		return colorBorder
	case theme.ColorNameHover:
		return colorHover
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
