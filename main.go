// Command smsadb reads SMS from an Android phone over adb.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usage = `usage: smsadb [-s serial] <command> [flags]

commands:
  devices  list attached phones
  list     print messages (-json for export)
  tui      browse messages in the terminal
  web      serve a web page with messages
  ntfy     push new incoming messages to an ntfy topic

run "smsadb <command> -h" for command flags
`

func main() {
	serial := flag.String("s", os.Getenv("ANDROID_SERIAL"), "device serial (default: $ANDROID_SERIAL, else the only attached phone)")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage); flag.PrintDefaults() }
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dev := Device{Serial: *serial}
	args := flag.Args()[1:]
	var err error
	switch flag.Arg(0) {
	case "devices":
		err = runDevices(ctx)
	case "list":
		err = runList(ctx, dev, args)
	case "tui":
		err = runTUI(ctx, dev, args)
	case "web":
		err = runWeb(ctx, dev, args)
	case "ntfy":
		err = runNtfy(ctx, dev, args)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "smsadb:", err)
		os.Exit(1)
	}
}

func runDevices(ctx context.Context) error {
	devs, err := Devices(ctx)
	if err != nil {
		return err
	}
	for _, d := range devs {
		fmt.Printf("%-20s %-12s %s\n", d.Serial, d.State, d.Name)
	}
	return nil
}

func runList(ctx context.Context, dev Device, args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	box := fs.String("box", "all", "all, inbox, sent, draft or outbox")
	n := fs.Int("n", 20, "max messages, 0 for all")
	asJSON := fs.Bool("json", false, "print JSON")
	fs.Parse(args)

	msgs, err := dev.Messages(ctx, Query{Box: *box, Limit: *n})
	if err != nil {
		return err
	}
	if *asJSON {
		if msgs == nil {
			msgs = []Message{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(msgs)
	}
	for _, m := range msgs {
		fmt.Printf("%s %s %s\n%s\n\n", m.Date.Format("2006-01-02 15:04"), m.Dir(), Printable(m.Address), Printable(m.Body))
	}
	return nil
}
