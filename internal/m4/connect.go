package m4

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/rfcomm"
)

// fallbackChannels are tried, in order, when the channel SDP advertises does not
// work.
//
// The device's SDP record for the GAIA service goes stale: it keeps advertising
// the channel from an earlier connection while the control service has moved.
// Observed live on a MOMENTUM 4, SDP said channel 2 while the service was
// answering on channel 1.
//
// Deliberately excluded: channel 10 (Hands-Free, in use by macOS for audio) and
// channel 14, which is an Airoha/RACE endpoint speaking an unrelated protocol.
// Opening those achieves nothing and poking at the audio channel risks the call
// the user is on.
var fallbackChannels = []int{1, 2, 3, 15, 16, 4, 5}

// scanTimeout bounds each candidate channel during a scan. A channel that will
// refuse does so promptly, and the full dial budget spent across every candidate
// would leave the caller waiting minutes.
const scanTimeout = 3 * time.Second

// probeTimeout bounds the handshake used to confirm a candidate channel. It is
// short because a wrong channel typically answers with nothing at all.
const probeTimeout = 1500 * time.Millisecond

// Connect opens the control channel to the headphones at addr and verifies that
// what answers really is the Sennheiser control service.
//
// When opts.Channel is set, only that channel is used. Otherwise the channel
// advertised over SDP is tried first, then the fallback list.
func Connect(addr string, opts rfcomm.Options) (*Client, error) {
	return ConnectProgress(addr, opts, nil)
}

// ConnectProgress is Connect with a callback describing each attempt, so a UI can
// show what is happening during a scan rather than appearing to hang.
func ConnectProgress(addr string, opts rfcomm.Options, progress func(string)) (*Client, error) {
	if progress == nil {
		progress = func(string) {}
	}

	if opts.Channel != 0 {
		progress(fmt.Sprintf("Connecting on channel %d...", opts.Channel))
		return connectChannel(addr, opts)
	}

	// SDP first: when its record is current, this is the right answer.
	progress("Connecting on the advertised channel...")
	client, err := connectChannel(addr, opts)
	if err == nil {
		return client, nil
	}

	// A failure that has nothing to do with the channel number will repeat on
	// every candidate, so report it once instead of scanning pointlessly.
	if errors.Is(err, rfcomm.ErrNoMainLoop) {
		return nil, err
	}

	attempts := []string{fmt.Sprintf("sdp: %v", err)}

	for i, channel := range fallbackChannels {
		progress(fmt.Sprintf("Scanning for the control service (%d of %d, channel %d)...",
			i+1, len(fallbackChannels), channel))

		client, err := connectChannel(addr, rfcomm.Options{
			UUID:    opts.UUID,
			Channel: channel,
			Timeout: scanTimeout,
		})
		if err == nil {
			return client, nil
		}
		attempts = append(attempts, fmt.Sprintf("channel %d: %v", channel, err))
	}

	return nil, fmt.Errorf("m4: could not find the control service on %s:\n  %s",
		addr, strings.Join(dedupe(attempts), "\n  "))
}

// dedupe collapses attempts that failed for the same reason, which is the normal
// case when the device refuses every channel.
func dedupe(attempts []string) []string {
	byReason := map[string][]string{}
	var order []string

	for _, attempt := range attempts {
		where, reason, ok := strings.Cut(attempt, ": ")
		if !ok {
			continue
		}
		if _, seen := byReason[reason]; !seen {
			order = append(order, reason)
		}
		byReason[reason] = append(byReason[reason], where)
	}

	out := make([]string, 0, len(order))
	for _, reason := range order {
		out = append(out, fmt.Sprintf("%s: %s", strings.Join(byReason[reason], ", "), reason))
	}
	return out
}

// connectChannel opens one channel and confirms the control service is behind it.
func connectChannel(addr string, opts rfcomm.Options) (*Client, error) {
	conn, err := rfcomm.Dial(addr, opts)
	if err != nil {
		return nil, err
	}

	client := newClient(conn)
	if err := client.verify(); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// verify sends a harmless read and checks that the reply is a well-formed
// Sennheiser frame.
//
// Opening a channel proves nothing: other RFCOMM services on these headphones
// accept a connection and then either stay silent or answer in a protocol of
// their own. Only a valid reply identifies the control service.
func (c *Client) verify() error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	frame, err := c.Request(ctx, gaia.CmdGetANC)
	if err != nil {
		// A device error still proves the control service is listening.
		var devErr *DeviceError
		if errors.As(err, &devErr) {
			return nil
		}
		return err
	}

	if frame.Vendor != gaia.VendorSennheiser {
		return fmt.Errorf("m4: channel answered with vendor %#04x, not Sennheiser", frame.Vendor)
	}
	return nil
}
