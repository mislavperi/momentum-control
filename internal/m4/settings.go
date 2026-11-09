package m4

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// SettingKind says how a setting's value is presented and parsed.
type SettingKind int

const (
	// KindBool is off (0) or on (1).
	KindBool SettingKind = iota
	// KindLevel is an integer between Min and Max.
	KindLevel
	// KindChoice is one of Choices.
	KindChoice
)

// Choice is one allowed value of a KindChoice setting.
type Choice struct {
	Value int
	Name  string // what the CLI accepts, e.g. "tones"
	Label string // what the app shows, e.g. "Tones only"
}

// Setting describes one user-adjustable headphone setting. The table below is
// the single source for the CLI's get/set and the app's Settings page, so the
// two cannot drift apart.
type Setting struct {
	Name        string // CLI name, e.g. "smart-pause"
	Title       string // app label
	Description string
	Kind        SettingKind
	Min, Max    int      // KindLevel
	Choices     []Choice // KindChoice

	read  func(c *Client) (int, error)
	write func(c *Client, v int) error
}

// Read fetches the setting's current value from the device.
func (s Setting) Read(c *Client) (int, error) { return s.read(c) }

// Write sends a new value. It is validated first, so the device never sees a
// value outside what the setting allows.
func (s Setting) Write(c *Client, v int) error {
	if err := s.validate(v); err != nil {
		return err
	}
	return s.write(c, v)
}

func (s Setting) validate(v int) error {
	switch s.Kind {
	case KindBool:
		if v != 0 && v != 1 {
			return fmt.Errorf("m4: %s is on or off", s.Name)
		}
	case KindLevel:
		if v < s.Min || v > s.Max {
			return fmt.Errorf("m4: %s takes %d to %d", s.Name, s.Min, s.Max)
		}
	case KindChoice:
		if _, ok := s.choice(v); !ok {
			return fmt.Errorf("m4: %s takes one of %s", s.Name, s.choiceNames())
		}
	}
	return nil
}

func (s Setting) choice(v int) (Choice, bool) {
	for _, ch := range s.Choices {
		if ch.Value == v {
			return ch, true
		}
	}
	return Choice{}, false
}

func (s Setting) choiceNames() string {
	names := make([]string, len(s.Choices))
	for i, ch := range s.Choices {
		names[i] = ch.Name
	}
	return strings.Join(names, ", ")
}

// Parse turns CLI text into a value: on/off for switches, a number for levels,
// a choice name for choices.
func (s Setting) Parse(text string) (int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	var v int
	switch s.Kind {
	case KindBool:
		switch text {
		case "on", "1", "true", "yes":
			v = 1
		case "off", "0", "false", "no":
			v = 0
		default:
			return 0, fmt.Errorf("m4: %s needs on or off, not %q", s.Name, text)
		}
	case KindLevel:
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, fmt.Errorf("m4: %s needs a number, not %q", s.Name, text)
		}
		v = n
	case KindChoice:
		for _, ch := range s.Choices {
			if ch.Name == text {
				return ch.Value, nil
			}
		}
		return 0, fmt.Errorf("m4: %s takes one of %s, not %q", s.Name, s.choiceNames(), text)
	}
	return v, s.validate(v)
}

// Format renders a value the way Parse accepts it.
func (s Setting) Format(v int) string {
	switch s.Kind {
	case KindBool:
		if v != 0 {
			return "on"
		}
		return "off"
	case KindChoice:
		if ch, ok := s.choice(v); ok {
			return ch.Name
		}
		return fmt.Sprintf("unknown(%d)", v)
	}
	return strconv.Itoa(v)
}

// byteSetting is a setting stored as one byte, read with get and written with
// set.
func byteSetting(get, set uint16) (func(*Client) (int, error), func(*Client, int) error) {
	read := func(c *Client) (int, error) {
		f, err := c.requestTimeout(get)
		if err != nil {
			return 0, err
		}
		b, err := payloadByte(f, get)
		return int(b), err
	}
	write := func(c *Client, v int) error {
		_, err := c.requestTimeout(set, byte(v))
		return err
	}
	return read, write
}

func boolSetting(name, title, desc string, get, set uint16) Setting {
	r, w := byteSetting(get, set)
	return Setting{Name: name, Title: title, Description: desc, Kind: KindBool, read: r, write: w}
}

// Auto power-off delays the app offers, in minutes. The device stores seconds
// and accepts other values, but only these are confirmed; "never" waits until
// its encoding is known.
var autoPowerOffChoices = []Choice{
	{Value: 15, Name: "15", Label: "15 minutes"},
	{Value: 30, Name: "30", Label: "30 minutes"},
	{Value: 60, Name: "60", Label: "1 hour"},
}

// AutoPowerOff reports the auto power-off delay in minutes.
func (c *Client) AutoPowerOff() (int, error) {
	f, err := c.requestTimeout(gaia.CmdGetTimer, gaia.TimerAutoPowerOff)
	if err != nil {
		return 0, err
	}
	// The reply echoes the timer number, then the value in seconds.
	if len(f.Payload) < 3 {
		return 0, fmt.Errorf("m4: timer reply carried %d bytes, want 3", len(f.Payload))
	}
	return int(binary.BigEndian.Uint16(f.Payload[1:3])) / 60, nil
}

// SetAutoPowerOff sets the auto power-off delay in minutes.
func (c *Client) SetAutoPowerOff(minutes int) error {
	secs := make([]byte, 2)
	binary.BigEndian.PutUint16(secs, uint16(minutes*60))
	_, err := c.requestTimeout(gaia.CmdSetTimer, gaia.TimerAutoPowerOff, secs[0], secs[1])
	return err
}

// Settings lists every adjustable setting, in the order the app shows them.
var Settings = func() []Setting {
	onHeadR, onHeadW := byteSetting(gaia.CmdGetOnHeadDetection, gaia.CmdSetOnHeadDetection)
	promptR, promptW := byteSetting(gaia.CmdGetPromptMode, gaia.CmdSetPromptMode)
	sideR, sideW := byteSetting(gaia.CmdGetSidetone, gaia.CmdSetSidetone)
	btR, btW := byteSetting(gaia.CmdGetBTCompatibility, gaia.CmdSetBTCompatibility)

	return []Setting{
		{
			Name: "on-head-detection", Title: "Pause when taken off",
			Description: "Pause playback when you take the headphones off",
			Kind:        KindBool, read: onHeadR, write: onHeadW,
		},
		boolSetting("smart-pause", "Smart pause",
			"Resume playback when you put the headphones back on",
			gaia.CmdGetSmartPause, gaia.CmdSetSmartPause),
		boolSetting("auto-answer", "Answer calls automatically",
			"Pick up incoming calls when you put the headphones on",
			gaia.CmdGetAutoAnswer, gaia.CmdSetAutoAnswer),
		boolSetting("comfort-call", "Comfort call",
			"Lower ANC during calls so your own voice sounds natural",
			gaia.CmdGetComfortCall, gaia.CmdSetComfortCall),
		{
			Name: "sidetone", Title: "Sidetone",
			Description: "How much of your own voice you hear during calls",
			Kind:        KindLevel, Min: 0, Max: 5, read: sideR, write: sideW,
		},
		{
			Name: "prompts", Title: "Voice prompts",
			Description: "What the headphones say or play on events",
			Kind:        KindChoice, read: promptR, write: promptW,
			Choices: []Choice{
				{Value: 0, Name: "off", Label: "Off"},
				{Value: 1, Name: "tones", Label: "Tones"},
				{Value: 2, Name: "voice", Label: "Tones + voice"},
			},
		},
		boolSetting("touch-lock", "Touch lock",
			"Ignore the touch controls",
			gaia.CmdGetTouchLock, gaia.CmdSetTouchLock),
		{
			Name: "bluetooth-mode", Title: "Bluetooth mode",
			Description: "Prefer sound quality, or connection stability with older devices",
			Kind:        KindChoice, read: btR, write: btW,
			Choices: []Choice{
				{Value: 0, Name: "audio", Label: "Better audio"},
				{Value: 1, Name: "compatibility", Label: "Compatibility"},
			},
		},
		{
			Name: "auto-power-off", Title: "Auto power-off",
			Description: "Turn off after this long without a connection",
			Kind:        KindChoice, Choices: autoPowerOffChoices,
			read:  (*Client).AutoPowerOff,
			write: (*Client).SetAutoPowerOff,
		},
	}
}()

// LookupSetting finds a setting by its CLI name.
func LookupSetting(name string) (Setting, bool) {
	for _, s := range Settings {
		if s.Name == name {
			return s, true
		}
	}
	return Setting{}, false
}
