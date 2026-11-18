package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	defaultPingInterval = time.Second
	defaultHistory      = 5 * time.Minute
)

func main() {
	options, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	m := newModelWithOptions(options.host, options.interval, options.traceCfg, options.traceEvery, options.noDNS)
	m.history = options.history
	p := tea.NewProgram(m, tea.WithAltScreen())
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
	history    time.Duration
}

// parseHistory accepts Go durations such as 30s, 5m or 1h. A bare number
// means minutes.
func parseHistory(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	var d time.Duration
	if minutes, err := strconv.ParseFloat(value, 64); err == nil {
		if math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes > float64(math.MaxInt64/time.Minute) {
			return 0, fmt.Errorf("invalid history %q", value)
		}
		d = time.Duration(minutes * float64(time.Minute))
	} else if d, err = time.ParseDuration(value); err != nil {
		return 0, fmt.Errorf("invalid history %q: use a number of minutes or a duration like 30s, 5m, 1h", value)
	}
	if d <= 0 {
		return 0, errors.New("history must be greater than zero")
	}
	return d, nil
}

func parseOptions(arguments []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("tping", flag.ContinueOnError)
	flags.SetOutput(output)
	intervalSeconds := flags.Float64("i", defaultPingInterval.Seconds(), "wait SECONDS between pings")
	traceEvery := flags.Int("trace-every", 1, "run traceroute sweep every N ping cycles (0 disables tracing)")
	maxHops := flags.Int("max-hops", 30, "maximum number of hops in traceroute")
	noDNS := flags.Bool("n", false, "disable reverse DNS lookup, show IP addresses only")
	historyText := defaultHistory.String()
	historyUsage := "how far back the graph can scroll: 30s, 5m, 1h; a bare number means minutes"
	flags.StringVar(&historyText, "history", historyText, historyUsage)
	flags.StringVar(&historyText, "H", historyText, "shorthand for -history")
	if err := flags.Parse(arguments); err != nil {
		return options{}, err
	}
	history, err := parseHistory(historyText)
	if err != nil {
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
		return options{}, fmt.Errorf("usage: tping [-i seconds] [-trace-every N] [-max-hops N] [-history duration] [-n] <host-or-ip>")
	}
	traceCfg := defaultTraceConfig()
	traceCfg.maxHops = *maxHops
	return options{
		host:       flags.Arg(0),
		interval:   time.Duration(*intervalSeconds * float64(time.Second)),
		traceEvery: *traceEvery,
		traceCfg:   traceCfg,
		noDNS:      *noDNS,
		history:    history,
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
