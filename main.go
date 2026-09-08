package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const defaultPingInterval = time.Second

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	intervalSeconds := flags.Float64("i", defaultPingInterval.Seconds(), "wait SECONDS between pings")
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *intervalSeconds <= 0 {
		fmt.Fprintln(os.Stderr, "ping interval must be greater than zero")
		os.Exit(2)
	}
	if flags.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [-i seconds] <host-or-ip>\n", os.Args[0])
		os.Exit(2)
	}

	interval := time.Duration(*intervalSeconds * float64(time.Second))
	p := tea.NewProgram(newModelWithInterval(flags.Arg(0), interval), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type pingMsg struct {
	rtt time.Duration
	err error
}

type tickMsg struct{}

func pingTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return tickMsg{} })
}
