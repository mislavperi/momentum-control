package m4

import (
	"context"
	"errors"
	"time"

	"github.com/mislavperi/momentum4-control/internal/gaia"
)

// ProbeResult is the outcome of sending one command during discovery.
type ProbeResult struct {
	Command uint16
	Payload []byte
	Status  byte
	Err     error
}

// Supported reports whether the device answered with data rather than an error.
func (r ProbeResult) Supported() bool { return r.Err == nil }

// Probe sends read-only (odd-numbered) commands in the range [from, to] and
// reports which the device answers.
//
// Sennheiser's protocol pairs an even "set" command with the next odd "get"
// command, so restricting probing to odd IDs keeps discovery from changing any
// setting. That pairing is an observed convention rather than a documented
// guarantee, so Probe refuses even IDs outright instead of trusting the caller.
//
// results is closed when the sweep finishes. The caller should drain it; probing
// stops early if ctx is cancelled or the device disconnects.
func (c *Client) Probe(ctx context.Context, from, to uint16, timeout time.Duration) <-chan ProbeResult {
	results := make(chan ProbeResult, 16)

	go func() {
		defer close(results)

		for cmd := from; ; cmd++ {
			if gaia.IsGetter(cmd) {
				select {
				case results <- c.probeOne(ctx, cmd, timeout):
				case <-ctx.Done():
					return
				}
				if errors.Is(c.lastFatal(), ErrNotConnected) {
					return
				}
			}
			if cmd == to { // guards against uint16 wraparound at 0xFFFF
				return
			}
		}
	}()

	return results
}

func (c *Client) probeOne(ctx context.Context, cmd uint16, timeout time.Duration) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	f, err := c.Request(ctx, cmd)
	res := ProbeResult{Command: cmd, Payload: f.Payload, Err: err}

	var devErr *DeviceError
	if errors.As(err, &devErr) {
		res.Status = devErr.Status
	}
	return res
}

func (c *Client) lastFatal() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
