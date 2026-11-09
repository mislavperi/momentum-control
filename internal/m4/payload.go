package m4

import (
	"fmt"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

func payloadByte(f gaia.Frame, cmd uint16) (byte, error) {
	if len(f.Payload) == 0 {
		return 0, fmt.Errorf("m4: %s returned an empty payload", gaia.CommandName(cmd))
	}
	return f.Payload[0], nil
}

func payloadBool(f gaia.Frame, cmd uint16) (bool, error) {
	b, err := payloadByte(f, cmd)
	return b != 0, err
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}
