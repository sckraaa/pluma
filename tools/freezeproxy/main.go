// Command freezeproxy is a TCP proxy that passes the first bytes of each
// connection and then goes silent without closing it, as the filtered
// networks of task 092 do with a blocked address (pkg/freezeproxy). Put in
// front of an entry of the gateway, it lets a client be checked on a path
// that freezes (task 113): the client must find the path frozen within
// the stall time of its probe and take another one.
//
// Usage (from the repo root):
//
//	go run ./tools/freezeproxy -listen 0.0.0.0:20443 -target 127.0.0.1:10443
//	go run ./tools/freezeproxy -listen 127.0.0.1:20443 -target 127.0.0.1:10443 -down 0   # a clean path
//
// -down (default 16384) is how many bytes of each connection pass from the
// target to the client, -up the same towards the target (default 0: all).
// It prints one line per connection with its byte counts, nothing of the
// traffic itself, until interrupted.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/teamgram/teamgram-server/pkg/freezeproxy"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "freezeproxy:", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("freezeproxy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", "127.0.0.1:20443", "address to listen on (host:port)")
	target := flags.String("target", "", "address of the gateway entry to pass connections to (host:port)")
	down := flags.Int64("down", 16<<10, "bytes of each connection passed from the target to the client before it freezes; 0 passes all")
	up := flags.Int64("up", 0, "bytes of each connection passed from the client to the target before it freezes; 0 passes all")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *target == "" {
		flags.Usage()
		return errors.New("-target is required")
	}

	p, err := freezeproxy.Listen(*listen, freezeproxy.Options{
		Target: *target,
		Down:   *down,
		Up:     *up,
		OnClose: func(s freezeproxy.ConnStats) {
			fmt.Fprintf(stdout, "connection %d: %d bytes down, %d up, frozen %v\n", s.ID, s.Down, s.Up, s.Frozen)
		},
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "freezeproxy: %s -> %s, freezing after %d bytes down and %d up (0: never)\n", p.Addr(), *target, *down, *up)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return p.Close()
}
