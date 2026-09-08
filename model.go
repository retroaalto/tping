package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-ping/ping"
)

const maxSamples = 48

const graphHeight = 5

const graphLevels = 8

type model struct {
	host     string
	interval time.Duration
	samples  []time.Duration
	latest   time.Duration
	min      time.Duration
	max      time.Duration
	sum      time.Duration
	received int
	lastErr  error
	total    int
	lost     int
	width    int
	height   int
	spinner  spinner.Model
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC"))
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	goodStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))
	badStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FB7185"))
	barStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8"))
)

func newModel(host string) model {
	return newModelWithInterval(host, defaultPingInterval)
}

func newModelWithInterval(host string, interval time.Duration) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return model{host: host, interval: interval, spinner: s, width: 80, height: 24}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, runPing(m.host))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"))) {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case pingMsg:
		m.total++
		m.lastErr = msg.err
		if msg.err != nil {
			m.lost++
			m.samples = append(m.samples, 0)
		} else {
			m.latest = msg.rtt
			m.sum += msg.rtt
			m.received++
			if m.received == 1 || msg.rtt < m.min {
				m.min = msg.rtt
			}
			if msg.rtt > m.max {
				m.max = msg.rtt
			}
			m.samples = append(m.samples, msg.rtt)
		}
		if len(m.samples) > maxSamples {
			m.samples = m.samples[len(m.samples)-maxSamples:]
		}
		return m, pingTick(m.interval)
	case tickMsg:
		return m, runPing(m.host)
	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("tping"))
	b.WriteString("  ")
	b.WriteString(mutedStyle.Render("live latency timeline"))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("TARGET "))
	b.WriteString(titleStyle.Render(m.host))
	b.WriteString("    ")
	b.WriteString(mutedStyle.Render("STATUS "))
	b.WriteString(m.status())
	b.WriteString("\n\n")

	b.WriteString(m.renderGraph())
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("LATEST "))
	b.WriteString(m.latestText())
	b.WriteString("    ")
	b.WriteString(mutedStyle.Render("MIN "))
	b.WriteString(m.statText(m.min))
	b.WriteString("    ")
	b.WriteString(mutedStyle.Render("AVG "))
	b.WriteString(m.statText(m.average()))
	b.WriteString("    ")
	b.WriteString(mutedStyle.Render("MAX "))
	b.WriteString(m.statText(m.max))
	b.WriteString("    ")
	b.WriteString(mutedStyle.Render("SENT "))
	b.WriteString(fmt.Sprintf("%d", m.total))
	b.WriteString("    ")
	b.WriteString(mutedStyle.Render("LOST "))
	b.WriteString(fmt.Sprintf("%d (%.1f%%)", m.lost, m.lossPercent()))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(m.spinner.View() + " pinging every 1s   q quit"))

	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

func (m model) status() string {
	if m.total == 0 {
		return warnStyle.Render("starting")
	}
	if m.lastErr != nil {
		return badStyle.Render("unreachable")
	}
	return goodStyle.Render("reachable")
}

func (m model) latestText() string {
	if m.total == 0 || m.lastErr != nil {
		return mutedStyle.Render("--")
	}
	return goodStyle.Render(formatDuration(m.latest))
}

func (m model) average() time.Duration {
	if m.received == 0 {
		return 0
	}
	return m.sum / time.Duration(m.received)
}

func (m model) statText(value time.Duration) string {
	if m.received == 0 {
		return mutedStyle.Render("--")
	}
	return goodStyle.Render(formatDuration(value))
}

func (m model) lossPercent() float64 {
	if m.total == 0 {
		return 0
	}
	return float64(m.lost) / float64(m.total) * 100
}

func (m model) renderGraph() string {
	graphWidth := m.width - 4
	if graphWidth < 10 {
		graphWidth = 10
	}
	if graphWidth > maxSamples {
		graphWidth = maxSamples
	}
	values := m.samples
	if len(values) > graphWidth {
		values = values[len(values)-graphWidth:]
	}
	missing := graphWidth - len(values)

	max := graphMax(values)

	var b strings.Builder
	for row := graphHeight; row > 0; row-- {
		label := max * time.Duration(row) / graphHeight
		b.WriteString(mutedStyle.Render(fmt.Sprintf("%6s", formatDuration(label))))
		b.WriteString(" ┤")
		b.WriteString(strings.Repeat(" ", missing))
		for _, sample := range values {
			if sample == 0 {
				if row == 1 {
					b.WriteString(badStyle.Render("×"))
				} else {
					b.WriteString(" ")
				}
				continue
			}
			units := int(sample * graphHeight * graphLevels / max)
			rowUnits := units - (row-1)*graphLevels
			if rowUnits <= 0 {
				b.WriteString(" ")
				continue
			}
			if rowUnits > graphLevels {
				rowUnits = graphLevels
			}
			b.WriteString(barStyle.Render(string([]rune(" ▁▂▃▄▅▆▇█")[rowUnits])))
		}
		b.WriteString("\n")
	}
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%6s", "0ms")))
	b.WriteString(" └")
	b.WriteString(strings.Repeat("─", graphWidth))
	return b.String()
}

func graphMax(values []time.Duration) time.Duration {
	var highest time.Duration
	for _, sample := range values {
		if sample > highest {
			highest = sample
		}
	}
	if highest == 0 {
		return 10 * time.Millisecond
	}

	// Leave room above the tallest bar, then round to a scale with ten smooth steps.
	target := highest + highest/5
	unit := time.Microsecond
	for target/unit >= 10 {
		unit *= 10
	}
	for multiplier := int64(1); multiplier <= 10; multiplier++ {
		scale := unit * time.Duration(multiplier)
		if target <= scale {
			return scale
		}
	}
	return unit * 10
}

func runPing(host string) tea.Cmd {
	return func() tea.Msg {
		pinger, err := ping.NewPinger(host)
		if err != nil {
			return pingMsg{err: err}
		}
		pinger.Count = 1
		pinger.Timeout = 900 * time.Millisecond
		pinger.SetPrivileged(false)
		if err := pinger.Run(); err != nil {
			return pingMsg{err: err}
		}
		stats := pinger.Statistics()
		if stats.PacketsRecv == 0 {
			return pingMsg{err: fmt.Errorf("no response")}
		}
		return pingMsg{rtt: stats.AvgRtt}
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.0fµs", float64(d)/float64(time.Microsecond))
	}
	return fmt.Sprintf("%.0fms", float64(d)/float64(time.Millisecond))
}
