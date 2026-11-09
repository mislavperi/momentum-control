package main

import (
	_ "embed"
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
)

// Line icons on a 24-unit grid, drawn as strokes so one path renders at any
// colour. Fyne cannot recolour an arbitrary SVG, so each colour gets its own
// resource, built on demand and cached.
var iconPaths = map[string]string{
	"overview":   `<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>`,
	"shield":     `<path d="M12 3l8 3v6c0 4.6-3.4 8-8 9-4.6-1-8-4.4-8-9V6z"/>`,
	"ambient":    `<path d="M3 12h2M19 12h2M8 8v8M12 5v14M16 8v8"/>`,
	"power":      `<path d="M12 3v8"/><path d="M6.3 6.8a8 8 0 1 0 11.4 0"/>`,
	"eq":         `<path d="M6 4v16M12 4v16M18 4v16"/><path d="M4 9h4M10 15h4M16 7h4"/>`,
	"sparkle":    `<path d="M12 3l1.9 5.6L19.5 10l-5.6 1.9L12 17.5l-1.9-5.6L4.5 10l5.6-1.4z"/><path d="M19 16l.7 2 2 .7-2 .7-.7 2-.7-2-2-.7 2-.7z"/>`,
	"settings":   `<circle cx="12" cy="12" r="3"/><path d="M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M5.3 18.7l2.1-2.1M16.6 7.4l2.1-2.1"/>`,
	"bluetooth":  `<path d="M7 7l10 10-5 5V2l5 5L7 17"/>`,
	"headphones": `<path d="M4 15v-3a8 8 0 0 1 16 0v3"/><rect x="3" y="14" width="4" height="7" rx="1.5"/><rect x="17" y="14" width="4" height="7" rx="1.5"/>`,
	"refresh":    `<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/>`,
	"wind":       `<path d="M3 8h11a3 3 0 1 0-3-3M3 12h16a3 3 0 1 1-3 3M3 16h8"/>`,
	"adaptive":   `<circle cx="12" cy="12" r="3"/><path d="M5.6 5.6a9 9 0 0 0 0 12.8M18.4 5.6a9 9 0 0 1 0 12.8"/>`,
	"battery":    `<rect x="2.5" y="7" width="17" height="10" rx="2"/><path d="M22 10.5v3"/>`,
}

var (
	iconMu    sync.Mutex
	iconCache = map[string]fyne.Resource{}
)

// icon returns the named line icon stroked in col.
func icon(name string, col color.Color) fyne.Resource {
	hex := hexColor(col)
	key := name + hex

	iconMu.Lock()
	defer iconMu.Unlock()
	if r, ok := iconCache[key]; ok {
		return r
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">%s</svg>`,
		hex, iconPaths[name])
	r := fyne.NewStaticResource(key+".svg", []byte(svg))
	iconCache[key] = r
	return r
}

func hexColor(c color.Color) string {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return fmt.Sprintf("#%02x%02x%02x", n.R, n.G, n.B)
}

// headphonesArt is the product photo shown in the overview, cut out from its
// white background so it sits on the dark panel.
//
//go:embed assets/headphones.png
var headphonesPNG []byte

var headphonesArt = fyne.NewStaticResource("headphones.png", headphonesPNG)
