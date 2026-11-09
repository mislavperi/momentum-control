package m4

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// PairedHost is one entry of the headphones' own paired-device list, which is
// what multipoint switches between. It is the device's view, independent of
// which hosts this computer knows about.
type PairedHost struct {
	Index     int
	Name      string
	Connected bool
	Priority  int

	// This is the host the app is running on. Disconnecting or forgetting it
	// would cut the app's own control channel, so the app does not offer it.
	This bool
}

// PairedHosts reads the headphones' paired-host list.
func (c *Client) PairedHosts() ([]PairedHost, error) {
	f, err := c.requestTimeout(gaia.CmdGetPairedCount)
	if err != nil {
		return nil, err
	}
	if len(f.Payload) < 2 {
		return nil, fmt.Errorf("m4: paired count reply carried %d bytes, want 2", len(f.Payload))
	}
	count := int(binary.BigEndian.Uint16(f.Payload[:2]))

	own := -1
	if f, err := c.requestTimeout(gaia.CmdGetOwnPairedIndex); err == nil && len(f.Payload) > 0 {
		own = int(f.Payload[0])
	}

	hosts := make([]PairedHost, 0, count)
	for i := 0; i < count; i++ {
		f, err := c.requestTimeout(gaia.CmdGetPairedInfo, byte(i))
		if err != nil {
			return nil, err
		}
		h, err := decodePairedHost(f.Payload)
		if err != nil {
			return nil, err
		}
		h.This = h.Index == own
		hosts = append(hosts, h)
	}
	return hosts, nil
}

// decodePairedHost reads index, priority, status, then a NUL-terminated name.
func decodePairedHost(p []byte) (PairedHost, error) {
	if len(p) < 3 {
		return PairedHost{}, fmt.Errorf("m4: paired host reply carried %d bytes, want at least 3", len(p))
	}
	name := p[3:]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return PairedHost{
		Index:     int(p[0]),
		Priority:  int(p[1]),
		Connected: p[2] == 1,
		Name:      string(name),
	}, nil
}

// MaxConnections reports how many hosts can be connected at once.
func (c *Client) MaxConnections() (int, error) {
	f, err := c.requestTimeout(gaia.CmdGetMaxConnections)
	if err != nil {
		return 0, err
	}
	b, err := payloadByte(f, gaia.CmdGetMaxConnections)
	return int(b), err
}

// ConnectHost asks the headphones to connect to a paired host.
func (c *Client) ConnectHost(index int) error {
	_, err := c.requestTimeout(gaia.CmdConnectPaired, byte(index))
	return err
}

// DisconnectHost asks the headphones to drop a connected host.
func (c *Client) DisconnectHost(index int) error {
	_, err := c.requestTimeout(gaia.CmdDisconnectPaired, byte(index))
	return err
}

// ForgetHost removes a host from the paired list. It has to pair again to
// reconnect.
func (c *Client) ForgetHost(index int) error {
	_, err := c.requestTimeout(gaia.CmdForgetPaired, byte(index))
	return err
}
