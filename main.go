package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const defaultPingInterval = time.Second

func main() {
	options, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	p := tea.NewProgram(newModelWithOptions(options.host, options.interval, options.traceCfg, options.traceEvery, options.noDNS), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	host       string
	interval   time.Duration
	traceEvery int
	traceCfg   traceConfig
	noDNS      bool
}

func parseOptions(arguments []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("tping", flag.ContinueOnError)
	flags.SetOutput(output)
	intervalSeconds := flags.Float64("i", defaultPingInterval.Seconds(), "wait SECONDS between pings")
	traceEvery := flags.Int("trace-every", 1, "run traceroute sweep every N ping cycles (0 disables tracing)")
	maxHops := flags.Int("max-hops", 30, "maximum number of hops in traceroute")
	noDNS := flags.Bool("n", false, "disable reverse DNS lookup, show IP addresses only")
	if err := flags.Parse(arguments); err != nil {
		return options{}, err
	}
	if *intervalSeconds <= 0 {
		return options{}, errors.New("ping interval must be greater than zero")
	}
	if *traceEvery < 0 {
		return options{}, errors.New("trace-every must be non-negative")
	}
	if *maxHops <= 0 || *maxHops > 255 {
		return options{}, errors.New("max-hops must be between 1 and 255")
	}
	if flags.NArg() != 1 {
		return options{}, fmt.Errorf("usage: tping [-i seconds] [-trace-every N] [-max-hops N] [-n] <host-or-ip>")
	}
	traceCfg := defaultTraceConfig()
	traceCfg.maxHops = *maxHops
	return options{
		host:       flags.Arg(0),
		interval:   time.Duration(*intervalSeconds * float64(time.Second)),
		traceEvery: *traceEvery,
		traceCfg:   traceCfg,
		noDNS:      *noDNS,
	}, nil
}

type pingMsg struct {
	rtt time.Duration
	err error
}

type tickMsg struct{}

type traceMsg struct {
	results []traceResult
	err     error
}

type traceHostMsg struct {
	ip       string
	hostname string
}

func pingTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return tickMsg{} })
}
