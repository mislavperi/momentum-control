package m4

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mislavperi/momentum-control/internal/rfcomm"
)

// deviceNameHints matches the paired-device name for a MOMENTUM 4.
var deviceNameHints = []string{"momentum 4", "momentum4"}

// FindHeadphones returns the address of a paired MOMENTUM 4. It fails when no
// match is found, or when several are paired and the choice is ambiguous.
func FindHeadphones() (rfcomm.Device, error) {
	var matches []rfcomm.Device
	for _, d := range rfcomm.PairedDevices() {
		name := strings.ToLower(d.Name)
		for _, hint := range deviceNameHints {
			if strings.Contains(name, hint) {
				matches = append(matches, d)
				break
			}
		}
	}

	switch len(matches) {
	case 0:
		return rfcomm.Device{}, errors.New("m4: no paired MOMENTUM 4 found; pair the headphones first, or pass -addr")
	case 1:
		return matches[0], nil
	default:
		var names []string
		for _, d := range matches {
			names = append(names, fmt.Sprintf("%s (%s)", d.Name, d.Address))
		}
		return rfcomm.Device{}, fmt.Errorf("m4: several MOMENTUM 4 devices are paired, pass -addr to choose: %s",
			strings.Join(names, ", "))
	}
}
