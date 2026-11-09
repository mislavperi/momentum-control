//go:build !darwin && !linux && !windows

package rfcomm

import "time"

// Conn is an open RFCOMM channel. This platform has no implementation, so no
// Conn is ever returned.
type Conn struct{}

// Dial always fails with ErrUnsupported.
func Dial(string, Options) (*Conn, error) { return nil, ErrUnsupported }

func (c *Conn) ChannelID() int               { return 0 }
func (c *Conn) SetReadDeadline(time.Time)    {}
func (c *Conn) Read([]byte) (int, error)     { return 0, ErrUnsupported }
func (c *Conn) Write([]byte) (int, error)    { return 0, ErrUnsupported }
func (c *Conn) Close() error                 { return nil }
func PairedDevices() []Device                { return nil }
func OnDeviceConnected(func(address string)) {}
func Main(fn func())                         { fn() }
