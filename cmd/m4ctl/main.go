// Command m4ctl controls a Sennheiser MOMENTUM 4 from the terminal, and probes
// its undocumented control protocol.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
	"github.com/mislavperi/momentum4-control/internal/rfcomm"
)

const usage = `m4ctl - control a Sennheiser MOMENTUM 4

Usage:
  m4ctl [flags] <command>

Commands:
  devices              List paired Bluetooth devices
  status               Show the current settings
  anc on|off           Turn active noise cancellation on or off
  transparency <0-100> Set the transparency level
  bass on|off          Turn the bass boost preset on or off
  watch                Print setting changes as they happen on the headphones
  probe                Sweep read-only commands to discover unknown features

Pass -wait to any command to keep retrying until the headphones accept a
connection, rather than failing straight away.

Flags:
`

func main() {
	var (
		addr    = flag.String("addr", "", "Bluetooth address (default: the paired MOMENTUM 4)")
		channel = flag.Int("channel", 0, "connect to this RFCOMM channel directly, skipping SDP lookup")
		from    = flag.Uint("from", 0x1000, "probe: first command ID")
		to      = flag.Uint("to", 0x1AFF, "probe: last command ID")
		timeout = flag.Duration("timeout", 400*time.Millisecond, "probe: per-command timeout")
		confirm = flag.Bool("i-understand-the-risk", false, "probe: required acknowledgement, see below")
		wait    = flag.Bool("wait", false, "keep retrying the connection until the device accepts one")
	)
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	// IOBluetooth delivers its callbacks on the main queue, so the main thread has
	// to run a Cocoa run loop while the real work happens on a goroutine.
	code := 0
	rfcomm.Main(func() {
		if err := run(flag.Args(), options{
			addr:    *addr,
			channel: *channel,
			from:    uint16(*from),
			to:      uint16(*to),
			timeout: *timeout,
			confirm: *confirm,
			wait:    *wait,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			code = 1
		}
	})
	os.Exit(code)
}

type options struct {
	addr    string
	channel int
	from    uint16
	to      uint16
	timeout time.Duration
	confirm bool
	wait    bool
}

func run(args []string, opt options) error {
	if len(args) == 0 {
		flag.Usage()
		return errors.New("no command given")
	}

	if args[0] == "devices" {
		return listDevices()
	}

	client, err := connect(opt)
	if err != nil {
		return err
	}
	defer client.Close()

	switch args[0] {
	case "status":
		return showStatus(client)
	case "anc":
		on, err := parseOnOff(args, "anc")
		if err != nil {
			return err
		}
		if err := client.SetANC(on); err != nil {
			return err
		}
		return showStatus(client)
	case "transparency":
		if len(args) < 2 {
			return errors.New("transparency needs a level from 0 to 100")
		}
		level, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("transparency level %q is not a number", args[1])
		}
		if level < 0 || level > 100 {
			return fmt.Errorf("transparency level %d is outside 0-100", level)
		}
		if err := client.SetTransparency(level); err != nil {
			return err
		}
		return showStatus(client)
	case "bass":
		on, err := parseOnOff(args, "bass")
		if err != nil {
			return err
		}
		if err := client.SetBassBoost(on); err != nil {
			return err
		}
		return showStatus(client)
	case "watch":
		return watch(client)
	case "raw":
		return raw(client, args[1:])
	case "probe":
		return probe(client, opt)
	default:
		flag.Usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func connect(opt options) (*m4.Client, error) {
	addr := opt.addr
	if addr == "" {
		device, err := m4.FindHeadphones()
		if err != nil {
			return nil, err
		}
		addr = device.Address
		fmt.Printf("Using %s (%s)\n", device.Name, device.Address)
	}

	if !opt.wait {
		return m4.Connect(addr, rfcomm.Options{Channel: opt.channel})
	}

	// The headphones refuse a control connection for long, unpredictable
	// stretches. Waiting lets a probe be started now and left alone: it connects
	// whenever the device relents. The interval is deliberately unhurried, since
	// repeated failed opens appear to make the device less willing, not more.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Printf("Waiting for the headphones to accept a control connection (Ctrl-C to stop)...\n")

	for attempt := 1; ; attempt++ {
		client, err := m4.Connect(addr, rfcomm.Options{Channel: opt.channel})
		if err == nil {
			fmt.Printf("Connected after %d attempt(s) on RFCOMM channel %d.\n",
				attempt, client.ChannelID())
			return client, nil
		}

		fmt.Printf("%s  attempt %d: still refused\n", time.Now().Format("15:04:05"), attempt)

		select {
		case <-time.After(waitInterval):
		case <-ctx.Done():
			return nil, fmt.Errorf("gave up waiting after %d attempts", attempt)
		}
	}
}

// waitInterval paces -wait. Slow on purpose: hammering the device with failed
// opens seems to make matters worse.
const waitInterval = 60 * time.Second

func listDevices() error {
	devices := rfcomm.PairedDevices()
	if len(devices) == 0 {
		return errors.New("no paired Bluetooth devices")
	}
	for _, d := range devices {
		fmt.Printf("%s  %s\n", d.Address, d.Name)
	}
	return nil
}

func showStatus(client *m4.Client) error {
	state, err := client.State()
	if err != nil {
		return err
	}
	fmt.Printf("RFCOMM channel: %d\n", state.ChannelID)
	fmt.Printf("ANC:            %s\n", onOff(state.ANC))
	fmt.Printf("Transparency:   %d%%\n", state.Transparency)
	fmt.Printf("Bass boost:     %s\n", onOff(state.BassBoost))
	return nil
}

func watch(client *m4.Client) error {
	client.RegisterNotifications()
	if err := showStatus(client); err != nil {
		return err
	}
	fmt.Println("\nWatching for changes, press Ctrl-C to stop.")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for {
		select {
		case n, ok := <-client.Notifications():
			if !ok {
				return errors.New("the headphones closed the connection")
			}
			fmt.Printf("%s  %-24s % x\n",
				time.Now().Format("15:04:05"), gaia.CommandName(n.Command), n.Payload)
		case <-ctx.Done():
			return nil
		}
	}
}

// raw sends a hand-written command and dumps every frame that arrives for a few
// seconds, without the request/response matching Request does. It exists to
// debug the protocol itself: if the device answers on a command we do not expect,
// or answers with a frame we fail to parse, raw still shows it.
func raw(client *m4.Client, args []string) error {
	if len(args) == 0 {
		return errors.New("raw needs a command ID in hex, e.g. raw 1a05, plus optional payload bytes")
	}

	cmd, err := strconv.ParseUint(strings.TrimPrefix(args[0], "0x"), 16, 16)
	if err != nil {
		return fmt.Errorf("command %q is not a 16-bit hex value", args[0])
	}

	var payload []byte
	for _, arg := range args[1:] {
		b, err := strconv.ParseUint(strings.TrimPrefix(arg, "0x"), 16, 8)
		if err != nil {
			return fmt.Errorf("payload byte %q is not a hex byte", arg)
		}
		payload = append(payload, byte(b))
	}

	fmt.Printf("RFCOMM channel: %d\n", client.ChannelID())

	encoded, err := gaia.Command(uint16(cmd), payload...).Encode()
	if err != nil {
		return err
	}
	fmt.Printf("send: % x\n", encoded)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	frame, err := client.Request(ctx, uint16(cmd), payload...)
	if err != nil {
		fmt.Printf("no matching reply: %v\n", err)
	} else {
		fmt.Printf("reply: %s\n", frame)
	}

	// Drain anything else the device volunteered, matched or not.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case n, ok := <-client.Notifications():
			if !ok {
				return nil
			}
			fmt.Printf("unsolicited: %s % x\n", gaia.CommandName(n.Command), n.Payload)
		case <-deadline:
			return nil
		}
	}
}

const probeWarning = `probe sends every odd-numbered command in the range to your headphones.

Odd IDs read a value and even IDs write one, which is why only odd IDs are sent.
That pairing is an observed convention in an undocumented protocol, not a promise
from Sennheiser: an unknown command could still change a setting or reset the
device. Nothing here can be verified in advance.

Re-run with -i-understand-the-risk to continue.`

func probe(client *m4.Client, opt options) error {
	if !opt.confirm {
		return errors.New(probeWarning)
	}
	if opt.from > opt.to {
		return fmt.Errorf("probe range %#04x-%#04x is empty", opt.from, opt.to)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Printf("Probing %#04x-%#04x, press Ctrl-C to stop.\n\n", opt.from, opt.to)

	found := 0
	for res := range client.Probe(ctx, opt.from, opt.to, opt.timeout) {
		if !res.Supported() {
			continue
		}
		found++
		fmt.Printf("%#04x  %-24s % x\n", res.Command, gaia.CommandName(res.Command), res.Payload)
	}

	fmt.Printf("\n%d command(s) answered.\n", found)
	return nil
}

func parseOnOff(args []string, name string) (bool, error) {
	if len(args) < 2 {
		return false, fmt.Errorf("%s needs 'on' or 'off'", name)
	}
	switch strings.ToLower(args[1]) {
	case "on", "true", "1":
		return true, nil
	case "off", "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("%s: expected 'on' or 'off', got %q", name, args[1])
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
