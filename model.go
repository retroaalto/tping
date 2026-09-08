package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-ping/ping"
)

const (
	maxSamples      = 48
	graphHeight     = 5
	graphLevels     = 8
	maxHostWidth    = 26
	maxBarWidth     = 12
	minHostWidth    = 8
	minBarWidth     = 4
	traceFixedWidth = 39
)

type model struct {
	host          string
	interval      time.Duration
	samples       []time.Duration
	latest        time.Duration
	min           time.Duration
	max           time.Duration
	sum           time.Duration
	received      int
	lastErr       error
	total         int
	lost          int
	width         int
	height        int
	spinner       spinner.Model
	hops          []hop
	traceCfg      traceConfig
	pingCount     int
	traceEvery    int
	traceInFlight bool
	traceErr      error
	noDNS         bool
	resolving     map[string]bool
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC"))
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	goodStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))
	badStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	barStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8"))
)

func newModel(host string) model {
	return newModelWithInterval(host, defaultPingInterval)
}

func newModelWithInterval(host string, interval time.Duration) model {
	return newModelWithOptions(host, interval, defaultTraceConfig(), 1, false)
}

func newModelWithOptions(host string, interval time.Duration, traceCfg traceConfig, traceEvery int, noDNS bool) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return model{
		host:          host,
		interval:      interval,
		spinner:       s,
		width:         80,
		height:        24,
		traceCfg:      traceCfg,
		traceEvery:    traceEvery,
		traceInFlight: traceEvery > 0,
		noDNS:         noDNS,
		resolving:     make(map[string]bool),
	}
}

func (m model) Init() tea.Cmd {
	commands := []tea.Cmd{m.spinner.Tick, runPing(m.host)}
	if m.traceEvery > 0 {
		commands = append(commands, runTrace(m.host, m.traceCfg))
	}
	return tea.Batch(commands...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"))) {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case traceMsg:
		m.traceInFlight = false
		m.traceErr = msg.err
		if msg.err != nil {
			return m, nil
		}
		return m, m.applyTrace(msg.results)
	case traceHostMsg:
		delete(m.resolving, msg.ip)
		for i := range m.hops {
			if m.hops[i].ip != nil && m.hops[i].ip.String() == msg.ip {
				m.hops[i].hostname = msg.hostname
			}
		}
	case pingMsg:
		m.recordPing(msg)
		m.pingCount++
		commands := []tea.Cmd{pingTick(m.interval)}
		if m.traceEvery > 0 && !m.traceInFlight && m.pingCount%m.traceEvery == 0 {
			m.traceInFlight = true
			commands = append(commands, runTrace(m.host, m.traceCfg))
		}
		return m, tea.Batch(commands...)
	case tickMsg:
		return m, runPing(m.host)
	}

	var command tea.Cmd
	m.spinner, command = m.spinner.Update(msg)
	return m, command
}

func (m *model) recordPing(msg pingMsg) {
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
}

func (m *model) applyTrace(results []traceResult) tea.Cmd {
	if len(m.hops) > len(results) {
		m.hops = m.hops[:len(results)]
	}
	commands := make([]tea.Cmd, 0, len(results))
	for i, result := range results {
		if i == len(m.hops) {
			m.hops = append(m.hops, hop{ttl: result.ttl})
		}
		h := &m.hops[i]
		h.ttl = result.ttl
		if result.err == nil && result.ip != nil && (h.ip == nil || !h.ip.Equal(result.ip)) {
			// A route change at this TTL starts a new hop identity. Do not
			// combine its latency history with the router it replaced.
			*h = hop{ttl: result.ttl, ip: result.ip}
		}
		h.sent++
		if result.err == nil && result.ip != nil {
			h.received++
			h.latest = result.rtt
			h.sum += result.rtt
			if h.received == 1 || result.rtt < h.min {
				h.min = result.rtt
			}
			if result.rtt > h.max {
				h.max = result.rtt
			}
			if !m.noDNS && h.hostname == "" && !m.resolving[result.ip.String()] {
				m.resolving[result.ip.String()] = true
				commands = append(commands, resolveTraceHost(result.ip))
			}
		}
	}
	return tea.Batch(commands...)
}

func runTrace(host string, config traceConfig) tea.Cmd {
	return func() tea.Msg {
		results, err := traceHost(host, config)
		return traceMsg{results: results, err: err}
	}
}

func resolveTraceHost(ip net.IP) tea.Cmd {
	ipText := ip.String()
	return func() tea.Msg {
		hostname := ipText
		if names, err := net.LookupAddr(ipText); err == nil && len(names) > 0 {
			hostname = strings.TrimSuffix(names[0], ".")
		}
		return traceHostMsg{ip: ipText, hostname: hostname}
	}
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
	if m.traceEvery > 0 {
		if len(m.hops) > 0 {
			b.WriteString(m.renderTraceTable())
		} else if m.traceErr != nil {
			b.WriteString(badStyle.Render("TRACE " + m.traceErr.Error()))
		} else {
			b.WriteString(mutedStyle.Render("TRACE discovering route…"))
		}
		b.WriteString("\n\n")
	}
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
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%s pinging every %s   q quit", m.spinner.View(), m.interval)))
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

func (m model) renderTraceTable() string {
	var b strings.Builder
	hostWidth, barWidth := m.traceColumnWidths()
	header := fmt.Sprintf("%3s  %-*s  %6s  %-*s  %6s  %6s  %6s", "HOP", hostWidth, "HOST", "LOSS", barWidth, "LATENCY", "MIN", "AVG", "MAX")
	b.WriteString(titleStyle.Render(header))
	b.WriteByte('\n')
	rows := m.traceRowsAvailable()
	for i, h := range m.hops {
		if i == rows {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("… and %d more hops", len(m.hops)-i)))
			b.WriteByte('\n')
			break
		}
		b.WriteString(m.renderTraceHop(h, hostWidth, barWidth))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) traceColumnWidths() (int, int) {
	available := m.width - 4 // outer horizontal padding
	flexible := available - traceFixedWidth
	if flexible < minHostWidth+minBarWidth {
		return minHostWidth, minBarWidth
	}

	barWidth := flexible / 3
	if barWidth < minBarWidth {
		barWidth = minBarWidth
	}
	if barWidth > maxBarWidth {
		barWidth = maxBarWidth
	}
	hostWidth := flexible - barWidth
	if hostWidth > maxHostWidth {
		hostWidth = maxHostWidth
	}
	return hostWidth, barWidth
}

func (m model) traceRowsAvailable() int {
	// View uses 19 non-hop rows: title/target spacing, the six-line graph,
	// table header, statistics, controls, blank lines, and outer padding.
	// Reserve one more row when the table needs its continuation marker.
	rows := m.height - 20
	if rows < 1 {
		return 1
	}
	return rows
}

func (m model) renderTraceHop(h hop, hostWidth, barWidth int) string {
	host := "?"
	if h.ip != nil {
		host = h.ip.String()
		if !m.noDNS && h.hostname != "" {
			host = h.hostname
		}
	}
	host = truncate(host, hostWidth)
	loss := fmt.Sprintf("%5.1f%%", h.lossPercent())
	if h.lossPercent() > 50 {
		loss = badStyle.Render(loss)
	} else if h.lossPercent() > 0 {
		loss = warnStyle.Render(loss)
	} else {
		loss = goodStyle.Render(loss)
	}
	bar := m.renderHopLatencyBar(h, barWidth)
	return fmt.Sprintf("%3d  %-*s  %s  %s  %s  %s  %s",
		h.ttl,
		hostWidth, host,
		loss,
		bar,
		m.hopStatText(h, h.min),
		m.hopStatText(h, h.average()),
		m.hopStatText(h, h.max),
	)
}

func (m model) hopStatText(h hop, value time.Duration) string {
	text := fmt.Sprintf("%6s", "--")
	if h.received > 0 {
		text = fmt.Sprintf("%6s", formatDuration(value))
		return goodStyle.Render(text)
	}
	return mutedStyle.Render(text)
}

func (m model) renderHopLatencyBar(h hop, width int) string {
	if h.received == 0 {
		return badStyle.Render("×" + strings.Repeat(" ", width-1))
	}
	scale := 10 * time.Millisecond
	for _, candidate := range m.hops {
		if candidate.received > 0 && candidate.latest > scale {
			scale = candidate.latest
		}
	}
	filled := int((h.latest*time.Duration(width) + scale - 1) / scale)
	if filled < 1 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("·", width-filled)
	if h.latest < 50*time.Millisecond {
		return goodStyle.Render(bar)
	}
	if h.latest < 150*time.Millisecond {
		return warnStyle.Render(bar)
	}
	return badStyle.Render(bar)
}

func truncate(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	if width < 2 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
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
		b.WriteByte('\n')
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
