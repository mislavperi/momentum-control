// Command m4snoop extracts Sennheiser control frames from an Android Bluetooth
// HCI snoop log.
//
// The protocol is undocumented, so the practical way to learn a command is to
// watch the official app use it. Android writes every HCI packet to
// btsnoop_hci.log when "Bluetooth HCI snoop log" is enabled in developer
// options; this reads that file and prints the GAIA frames inside it, in order,
// with direction and timing.
//
// Usage:
//
//	m4snoop [-v] btsnoop_hci.log
//
// Frames are located by scanning packet payloads for the start-of-frame marker
// rather than by reassembling RFCOMM. Reassembly would be the rigorous approach,
// but these frames are small, never fragmented in practice, and the marker plus
// a vendor check is a strong enough signal; being wrong shows up as an obviously
// malformed frame rather than as silent misreading.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// btsnoop file header: the magic string, then version and datalink type.
const (
	snoopMagic      = "btsnoop\x00"
	snoopHeaderSize = 16
	recordHeader    = 24
)

// btsnoop timestamps count microseconds from year 0, using a base the format
// inherits from its Unix-epoch offset.
const epochOffsetMicros = 0x00dcddb30f2f8000

func main() {
	verbose := flag.Bool("v", false, "print every frame, not just the first of each command")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: m4snoop [-v] btsnoop_hci.log")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *verbose); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// capture is one observed frame.
type capture struct {
	at      time.Time
	fromApp bool
	frame   gaia.Frame
}

func run(path string, verbose bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < snoopHeaderSize || string(data[:len(snoopMagic)]) != snoopMagic {
		return fmt.Errorf("%s is not a btsnoop capture", path)
	}

	captures, err := extract(data[snoopHeaderSize:])
	if err != nil {
		return err
	}
	if len(captures) == 0 {
		return fmt.Errorf("no Sennheiser frames found in %s; was the snoop log enabled before the app ran?", path)
	}

	report(captures, verbose)
	return nil
}

// extract walks the records and pulls out every GAIA frame.
func extract(records []byte) ([]capture, error) {
	var out []capture

	for len(records) >= recordHeader {
		included := binary.BigEndian.Uint32(records[4:8])
		flags := binary.BigEndian.Uint32(records[8:12])
		micros := binary.BigEndian.Uint64(records[16:24])

		end := recordHeader + int(included)
		if included > uint32(len(records)) || end > len(records) {
			// A truncated final record is normal in a capture that was still
			// running, so stop rather than fail.
			break
		}
		payload := records[recordHeader:end]

		// Flags bit 0: 0 means host to controller, i.e. sent by the phone.
		fromApp := flags&1 == 0
		at := time.UnixMicro(int64(micros - epochOffsetMicros)).UTC()

		for _, f := range framesIn(payload) {
			out = append(out, capture{at: at, fromApp: fromApp, frame: f})
		}

		records = records[end:]
	}

	return out, nil
}

// framesIn finds the GAIA frames inside one HCI packet.
func framesIn(payload []byte) []gaia.Frame {
	var found []gaia.Frame

	for i := 0; i+gaia.HeaderLen <= len(payload); i++ {
		if payload[i] != gaia.SOF {
			continue
		}
		f, n, err := gaia.Decode(payload[i:])
		if err != nil {
			continue
		}
		// The marker alone is weak; the vendor makes it convincing.
		if f.Vendor != gaia.VendorSennheiser {
			continue
		}
		found = append(found, f)
		i += n - 1
	}
	return found
}

func report(captures []capture, verbose bool) {
	start := captures[0].at

	fmt.Printf("%d frame(s)\n\n", len(captures))
	fmt.Printf("%-10s %-4s %-28s %s\n", "TIME", "DIR", "COMMAND", "PAYLOAD")

	seen := map[uint16]bool{}
	for _, c := range captures {
		if !verbose && seen[c.frame.Command] {
			continue
		}
		seen[c.frame.Command] = true

		dir := "<--"
		if c.fromApp {
			dir = "-->"
		}
		fmt.Printf("%-10s %-4s %-28s % x\n",
			c.at.Sub(start).Round(time.Millisecond),
			dir,
			gaia.CommandName(c.frame.Command),
			c.frame.Payload)
	}

	summarise(captures)
}

// summarise lists the commands the app sent, which is the interesting half: the
// replies follow from them.
func summarise(captures []capture) {
	counts := map[uint16]int{}
	for _, c := range captures {
		if c.fromApp {
			counts[c.frame.Command]++
		}
	}

	commands := make([]uint16, 0, len(counts))
	for cmd := range counts {
		commands = append(commands, cmd)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i] < commands[j] })

	fmt.Printf("\nCommands sent by the app:\n")
	for _, cmd := range commands {
		kind := "set"
		if gaia.IsGetter(cmd) {
			kind = "get"
		}
		fmt.Printf("  %#04x  %-5s %-28s x%d\n", cmd, kind, gaia.CommandName(cmd), counts[cmd])
	}
}
