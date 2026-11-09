package main

import (
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/mislavperi/momentum-control/internal/m4"
)

// settingRow is one setting's row on the Settings page.
type settingRow struct {
	setting m4.Setting
	row     fyne.CanvasObject

	// show reflects a value read from the device without writing it back.
	show func(v int)
}

// settingIcons gives each setting a line icon; anything unlisted gets the gear.
var settingIcons = map[string]string{
	"on-head-detection": "headphones",
	"smart-pause":       "power",
	"auto-answer":       "phone",
	"comfort-call":      "phone",
	"sidetone":          "ambient",
	"prompts":           "sparkle",
	"touch-lock":        "lock",
	"bluetooth-mode":    "bluetooth",
	"auto-power-off":    "clock",
}

// buildSettings lays out one row per entry of m4.Settings, so the page and the
// CLI's get/set always offer the same settings.
func (u *ui) buildSettings() fyne.CanvasObject {
	rows := container.NewVBox()
	for i, s := range m4.Settings {
		r := u.newSettingRow(s)
		u.settingRows = append(u.settingRows, r)
		if i > 0 {
			// Each separator travels with the row below it, so hiding an
			// unsupported row does not leave a double line.
			r.row = container.NewVBox(spacer(6), widget.NewSeparator(), spacer(6), r.row)
		}
		r.row.Hide() // shown once the device answers for it
		rows.Add(r.row)
	}
	u.settingsEmpty = text("Settings appear once the headphones are connected.", 13, colorTextMuted, false)
	return scrollPage(titledCard("Headphone settings",
		"Stored on the headphones, so they apply on every device you use them with.",
		container.NewVBox(u.settingsEmpty, rows)))
}

func (u *ui) newSettingRow(s m4.Setting) *settingRow {
	r := &settingRow{setting: s}
	iconName := settingIcons[s.Name]
	if iconName == "" {
		iconName = "settings"
	}

	switch s.Kind {
	case m4.KindBool:
		t := newToggle(func(on bool) {
			v := 0
			if on {
				v = 1
			}
			u.writeSetting(r, v)
		})
		r.show = func(v int) { t.SetOn(v != 0) }
		r.row = row(iconName, s.Title, s.Description, t)

	default:
		// Levels and choices both become a row of segmented pills.
		var options []m4.Choice
		if s.Kind == m4.KindLevel {
			for v := s.Min; v <= s.Max; v++ {
				options = append(options, m4.Choice{Value: v, Label: strconv.Itoa(v)})
			}
		} else {
			options = s.Choices
		}
		pills := make([]*pill, len(options))
		group := container.NewHBox()
		for i, opt := range options {
			value := opt.Value
			p := newPill("", opt.Label, func() { u.writeSetting(r, value) })
			p.compact = true
			pills[i] = p
			group.Add(p)
		}
		r.show = func(v int) {
			for i, opt := range options {
				pills[i].SetSelected(opt.Value == v)
			}
		}
		r.row = row(iconName, s.Title, s.Description, group)
	}
	return r
}

// writeSetting sends a value and reads it back, so a value the headphones did
// not take shows up instead of the control silently lying.
func (u *ui) writeSetting(r *settingRow, v int) {
	r.show(v) // optimistic; corrected by the read-back
	u.mu.Lock()
	client := u.client
	u.mu.Unlock()
	if client == nil {
		u.setStatus("Not connected — the change was not sent.", colorDanger)
		return
	}

	go func() {
		err := r.setting.Write(client, v)
		got, readErr := r.setting.Read(client)
		fyne.Do(func() {
			if readErr == nil {
				r.show(got)
			}
			switch {
			case err != nil:
				u.setStatusNow(fmt.Sprintf("Failed to change %s: %v", r.setting.Title, err), colorDanger)
			case readErr == nil && got != v:
				u.setStatusNow(fmt.Sprintf("The headphones kept %s at %s", r.setting.Title, r.setting.Format(got)), colorDanger)
			default:
				u.setStatusNow(fmt.Sprintf("%s: %s", r.setting.Title, r.setting.Format(v)), colorTextMuted)
			}
		})
	}()
}

// loadSettings reads every setting and shows the rows the device answers for.
func (u *ui) loadSettings(client *m4.Client) {
	values := make([]int, len(u.settingRows))
	ok := make([]bool, len(u.settingRows))
	for i, r := range u.settingRows {
		v, err := r.setting.Read(client)
		values[i], ok[i] = v, err == nil
	}

	u.mu.Lock()
	current := u.client == client
	u.mu.Unlock()
	if !current {
		return
	}

	fyne.Do(func() {
		any := false
		for i, r := range u.settingRows {
			setVisible(r.row, ok[i])
			if ok[i] {
				r.show(values[i])
				any = true
			}
		}
		setVisible(u.settingsEmpty, !any)
	})
}

// clearSettings hides every row while disconnected.
func (u *ui) clearSettings() {
	for _, r := range u.settingRows {
		r.row.Hide()
	}
	if u.settingsEmpty != nil {
		u.settingsEmpty.Show()
	}
}
