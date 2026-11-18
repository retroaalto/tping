package main

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestModelRecordsRollingSamples(t *testing.T) {
	m := newModel("localhost")
	m.history = maxSamples * m.interval
	for i := 0; i < maxSamples+3; i++ {
		var err error
		if i == maxSamples+2 {
			err = errors.New("timeout")
		}
		updated, _ := m.Update(pingMsg{rtt: 20 * time.Millisecond, err: err})
		m = updated.(model)
	}

	if len(m.samples) != maxSamples {
		t.Fatalf("got %d samples, want %d", len(m.samples), maxSamples)
	}
	if m.total != maxSamples+3 || m.lost != 1 {
		t.Fatalf("got total/lost %d/%d", m.total, m.lost)
	}
	if !strings.Contains(m.View(), "unreachable") {
		t.Fatal("view does not show unreachable status")
	}
}

func TestLossPercent(t *testing.T) {
	m := newModel("localhost")
	m.total, m.lost = 4, 1
	if got := m.lossPercent(); got != 25 {
		t.Fatalf("got %.1f%%, want 25%%", got)
	}
}

func TestLatencyStatistics(t *testing.T) {
	m := newModel("localhost")
	for _, rtt := range []time.Duration{10 * time.Millisecond, 30 * time.Millisecond, 20 * time.Millisecond} {
		updated, _ := m.Update(pingMsg{rtt: rtt})
		m = updated.(model)
	}

	if m.min != 10*time.Millisecond || m.average() != 20*time.Millisecond || m.max != 30*time.Millisecond {
		t.Fatalf("got min/avg/max %s/%s/%s", m.min, m.average(), m.max)
	}
	if !strings.Contains(m.View(), "MIN 10ms") || !strings.Contains(m.View(), "AVG 20ms") || !strings.Contains(m.View(), "MAX 30ms") {
		t.Fatal("view does not show latency statistics")
	}
}

func TestGraphMaxScalesToVisibleSamples(t *testing.T) {
	tests := []struct {
		name   string
		sample time.Duration
		want   time.Duration
	}{
		{name: "microseconds", sample: 700 * time.Microsecond, want: 900 * time.Microsecond},
		{name: "milliseconds", sample: 7 * time.Millisecond, want: 9 * time.Millisecond},
		{name: "slow response", sample: 140 * time.Millisecond, want: 200 * time.Millisecond},
		{name: "high latency", sample: 321 * time.Millisecond, want: 400 * time.Millisecond},
		{name: "smooth high latency", sample: 420 * time.Millisecond, want: 600 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := graphMax([]time.Duration{tt.sample}); got != tt.want {
				t.Fatalf("graphMax(%s) = %s, want %s", tt.sample, got, tt.want)
			}
		})
	}
}

func TestGraphUsesStaticWidthBeforeSamplesFillIt(t *testing.T) {
	m := newModel("localhost")
	m.width = 30
	updated, _ := m.Update(pingMsg{rtt: 7 * time.Millisecond})
	graph := updated.(model).renderGraph()
	lines := strings.Split(graph, "\n")
	if len(lines) != graphHeight+1 {
		t.Fatalf("got %d graph lines, want %d", len(lines), graphHeight+1)
	}
	if got, want := len([]rune(lines[graphHeight]))-8, 26; got != want {
		t.Fatalf("got X-axis width %d, want %d", got, want)
	}
}

func TestGraphUsesPartialBarLevels(t *testing.T) {
	m := newModel("localhost")
	m.width = 20
	updated, _ := m.Update(pingMsg{rtt: 13 * time.Millisecond})
	graph := updated.(model).renderGraph()
	if !strings.Contains(graph, "▂") {
		t.Fatal("graph does not render partial bar levels")
	}
}

func TestHopStatistics(t *testing.T) {
	h := hop{ttl: 1}

	// Test initial state
	if h.lossPercent() != 0 {
		t.Fatalf("expected 0%% loss, got %.1f%%", h.lossPercent())
	}
	if h.average() != 0 {
		t.Fatalf("expected 0 average, got %v", h.average())
	}

	// Add some samples
	h.sent = 3
	h.received = 2
	h.sum = 30 * time.Millisecond
	h.min = 10 * time.Millisecond
	h.max = 20 * time.Millisecond
	h.latest = 15 * time.Millisecond

	if got := h.lossPercent(); got != 33.33333333333333 {
		t.Fatalf("expected 33.3%% loss, got %.1f%%", got)
	}
	if got := h.average(); got != 15*time.Millisecond {
		t.Fatalf("expected 15ms average, got %v", got)
	}
}

func TestTraceTableRendering(t *testing.T) {
	m := newModel("localhost")
	m.width = 100
	m.height = 40

	// Add some hops
	m.hops = []hop{
		{ttl: 1, sent: 3, received: 3, latest: 5 * time.Millisecond, min: 4 * time.Millisecond, max: 6 * time.Millisecond, sum: 15 * time.Millisecond},
		{ttl: 2, sent: 3, received: 2, latest: 15 * time.Millisecond, min: 10 * time.Millisecond, max: 20 * time.Millisecond, sum: 30 * time.Millisecond},
		{ttl: 3, sent: 3, received: 0},
	}

	table := m.renderTraceTable()

	// Check that table contains expected elements
	if !strings.Contains(table, "HOP") {
		t.Fatal("table missing HOP header")
	}
	if !strings.Contains(table, "HOST") {
		t.Fatal("table missing HOST header")
	}
	if !strings.Contains(table, "LOSS") {
		t.Fatal("table missing LOSS header")
	}
	if !strings.Contains(table, "MIN") {
		t.Fatal("table missing MIN header")
	}
	if !strings.Contains(table, "AVG") {
		t.Fatal("table missing AVG header")
	}
	if !strings.Contains(table, "MAX") {
		t.Fatal("table missing MAX header")
	}

	// Check that hops are rendered
	if !strings.Contains(table, "1") {
		t.Fatal("table missing hop 1")
	}
	if !strings.Contains(table, "2") {
		t.Fatal("table missing hop 2")
	}
	if !strings.Contains(table, "3") {
		t.Fatal("table missing hop 3")
	}
}

func TestTraceTableHeightLimit(t *testing.T) {
	m := newModel("localhost")
	m.width = 100
	m.height = 15 // Small height

	// Add many hops
	for i := 1; i <= 30; i++ {
		m.hops = append(m.hops, hop{ttl: i, sent: 1, received: 1, latest: 10 * time.Millisecond})
	}

	table := m.renderTraceTable()

	// Should show "and X more hops" message
	if !strings.Contains(table, "more hops") {
		t.Fatal("table should show remaining hops message when height is limited")
	}
}

func TestTraceErrorDataUsesOffenderAddress(t *testing.T) {
	data := make([]byte, sockExtendedErrLen+sockaddrInet4Len)
	data[4] = soEEOriginICMP
	data[5] = icmpTimeExceeded
	copy(data[sockExtendedErrLen+4:], net.IPv4(192, 168, 86, 35).To4())

	ip, reached, ok := parseTraceErrorData(data)
	if !ok || reached {
		t.Fatalf("got ok/reached %t/%t, want true/false", ok, reached)
	}
	if got, want := ip.String(), "192.168.86.35"; got != want {
		t.Fatalf("got offender %s, want %s", got, want)
	}
}

func TestTraceErrorDataRecognizesDestination(t *testing.T) {
	data := make([]byte, sockExtendedErrLen+sockaddrInet4Len)
	data[4] = soEEOriginICMP
	data[5] = icmpDestUnreachable
	data[6] = icmpPortUnreachable
	copy(data[sockExtendedErrLen+4:], net.IPv4(1, 1, 1, 1).To4())

	_, reached, ok := parseTraceErrorData(data)
	if !ok || !reached {
		t.Fatalf("got ok/reached %t/%t, want true/true", ok, reached)
	}
}

func TestICMPEchoHasValidChecksumAndSequence(t *testing.T) {
	packet := makeICMPEcho(42)
	if got, want := len(packet), 8; got != want {
		t.Fatalf("got packet length %d, want %d", got, want)
	}
	if packet[0] != 8 || packet[1] != 0 {
		t.Fatalf("got ICMP type/code %d/%d, want 8/0", packet[0], packet[1])
	}
	if packet[4] != 0 || packet[5] != 42 {
		t.Fatalf("got ICMP sequence %d/%d, want 0/42", packet[4], packet[5])
	}
	if got := internetChecksum(packet); got != 0 {
		t.Fatalf("got checksum verification value %#x, want 0", got)
	}
}

func TestApplyTraceTruncatesRouteAtDestination(t *testing.T) {
	m := newModelWithOptions("1.1.1.1", time.Second, defaultTraceConfig(), 1, true)
	m.hops = []hop{{ttl: 1}, {ttl: 2}, {ttl: 3}}
	results := []traceResult{
		{ttl: 1, ip: net.IPv4(192, 168, 86, 35), rtt: 7 * time.Millisecond},
		{ttl: 2, ip: net.IPv4(1, 1, 1, 1), rtt: 20 * time.Millisecond, reached: true},
	}
	m.applyTrace(results)

	if got, want := len(m.hops), 2; got != want {
		t.Fatalf("got %d hops, want %d", got, want)
	}
	if got, want := m.hops[1].ip.String(), "1.1.1.1"; got != want {
		t.Fatalf("got destination %s, want %s", got, want)
	}
}

func TestApplyTraceResetsStatisticsWhenHopChanges(t *testing.T) {
	m := newModelWithOptions("1.1.1.1", time.Second, defaultTraceConfig(), 1, true)
	m.hops = []hop{
		{ttl: 1, ip: net.IPv4(192, 168, 86, 35), sent: 5, received: 5, min: 5 * time.Millisecond, max: 50 * time.Millisecond, sum: 100 * time.Millisecond},
	}
	m.applyTrace([]traceResult{{ttl: 1, ip: net.IPv4(192, 168, 0, 1), rtt: 10 * time.Millisecond}})

	h := m.hops[0]
	if h.ip.String() != "192.168.0.1" || h.sent != 1 || h.received != 1 || h.min != 10*time.Millisecond || h.max != 10*time.Millisecond {
		t.Fatalf("hop statistics were not reset after route change: %+v", h)
	}
}

func TestTraceTableColumnsHaveConsistentWidth(t *testing.T) {
	m := newModelWithOptions("localhost", time.Second, defaultTraceConfig(), 1, true)
	m.height = 40
	m.hops = []hop{
		{ttl: 1, ip: net.IPv4(192, 168, 86, 35), sent: 1, received: 1, latest: 7 * time.Millisecond, min: 7 * time.Millisecond, max: 7 * time.Millisecond, sum: 7 * time.Millisecond},
		{ttl: 2, sent: 1},
	}

	lines := strings.Split(strings.TrimSuffix(m.renderTraceTable(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	want := lipgloss.Width(lines[0])
	for _, line := range lines[1:] {
		if got := lipgloss.Width(line); got != want {
			t.Fatalf("line width %d, want %d: %q", got, want, line)
		}
	}
	if got, want := lipgloss.Width(lines[0]), m.width-4; got > want {
		t.Fatalf("table width %d exceeds content width %d", got, want)
	}
}

func TestParseOptions(t *testing.T) {
	var output bytes.Buffer
	options, err := parseOptions([]string{"-i", "0.5", "-trace-every", "2", "-max-hops", "10", "-n", "1.1.1.1"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if options.host != "1.1.1.1" || options.interval != 500*time.Millisecond || options.traceEvery != 2 || options.traceCfg.maxHops != 10 || !options.noDNS {
		t.Fatalf("unexpected options: %+v", options)
	}
}

func TestParseOptionsRejectsInvalidTraceEvery(t *testing.T) {
	_, err := parseOptions([]string{"-trace-every", "-1", "1.1.1.1"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "trace-every") {
		t.Fatalf("got %v, want trace-every validation error", err)
	}
}

func TestApplyTraceKeepsStatisticsWhenSilentHopFirstAnswers(t *testing.T) {
	m := newModelWithOptions("1.1.1.1", time.Second, defaultTraceConfig(), 1, true)
	m.hops = []hop{{ttl: 1, sent: 4}}
	m.applyTrace([]traceResult{{ttl: 1, ip: net.IPv4(192, 168, 0, 1), rtt: 10 * time.Millisecond}})

	h := m.hops[0]
	if h.ip.String() != "192.168.0.1" || h.sent != 5 || h.received != 1 {
		t.Fatalf("earlier timeouts were lost when the hop first answered: %+v", h)
	}
}

func TestTraceErrorDataTreatsAnyDestinationUnreachableAsEnd(t *testing.T) {
	data := make([]byte, sockExtendedErrLen+sockaddrInet4Len)
	data[4] = soEEOriginICMP
	data[5] = icmpDestUnreachable
	data[6] = 1 // host unreachable
	copy(data[sockExtendedErrLen+4:], net.IPv4(10, 0, 0, 1).To4())

	_, reached, ok := parseTraceErrorData(data)
	if !ok || !reached {
		t.Fatalf("got ok/reached %t/%t, want true/true", ok, reached)
	}
}

func TestTraceTableClipsToNarrowTerminal(t *testing.T) {
	m := newModelWithOptions("localhost", time.Second, defaultTraceConfig(), 1, true)
	m.width = 40
	m.height = 40
	m.hops = []hop{{ttl: 1, ip: net.IPv4(192, 168, 86, 35), sent: 1, received: 1, latest: 7 * time.Millisecond}}

	for _, line := range strings.Split(strings.TrimSuffix(m.renderTraceTable(), "\n"), "\n") {
		if got, want := lipgloss.Width(line), m.width-4; got > want {
			t.Fatalf("line width %d exceeds content width %d: %q", got, want, line)
		}
	}
}

func TestViewShowsTraceErrorOverStaleTable(t *testing.T) {
	m := newModelWithOptions("localhost", time.Second, defaultTraceConfig(), 1, true)
	m.hops = []hop{{ttl: 1, ip: net.IPv4(192, 168, 86, 35), sent: 1, received: 1}}
	m.traceErr = errors.New("resolve \"localhost\": boom")

	view := m.View()
	if !strings.Contains(view, "TRACE resolve") || strings.Contains(view, "192.168.86.35") {
		t.Fatalf("view should show the trace error instead of the stale table:\n%s", view)
	}
}

func TestParseHistory(t *testing.T) {
	valid := map[string]time.Duration{
		"5m":    5 * time.Minute,
		"1h":    time.Hour,
		"30s":   30 * time.Second,
		"1h30m": 90 * time.Minute,
		"10":    10 * time.Minute,
		"1.5":   90 * time.Second,
		" 2 ":   2 * time.Minute,
	}
	for in, want := range valid {
		got, err := parseHistory(in)
		if err != nil || got != want {
			t.Errorf("parseHistory(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "0", "0s", "-5m", "-1", "inf", "-inf", "nan", "1e30", "5x"} {
		if got, err := parseHistory(in); err == nil {
			t.Errorf("parseHistory(%q) = %s, want error", in, got)
		}
	}
}

func TestParseOptionsHistory(t *testing.T) {
	tests := []struct {
		args []string
		want time.Duration
	}{
		{[]string{"1.1.1.1"}, 5 * time.Minute},
		{[]string{"--history", "1h", "1.1.1.1"}, time.Hour},
		{[]string{"--history=30s", "1.1.1.1"}, 30 * time.Second},
		{[]string{"-history", "10", "1.1.1.1"}, 10 * time.Minute},
		{[]string{"-H", "5m", "1.1.1.1"}, 5 * time.Minute},
		{[]string{"-H", "2", "1.1.1.1"}, 2 * time.Minute},
	}
	for _, tt := range tests {
		options, err := parseOptions(tt.args, &bytes.Buffer{})
		if err != nil || options.history != tt.want {
			t.Errorf("parseOptions(%v) history = %s, %v; want %s", tt.args, options.history, err, tt.want)
		}
	}
	for _, bad := range []string{"0", "-1m", "soon"} {
		if _, err := parseOptions([]string{"-H", bad, "1.1.1.1"}, &bytes.Buffer{}); err == nil {
			t.Errorf("-H %s: want error", bad)
		}
	}
}

func feedPings(m model, count int) model {
	for i := 1; i <= count; i++ {
		updated, _ := m.Update(pingMsg{rtt: time.Duration(i) * time.Millisecond})
		m = updated.(model)
	}
	return m
}

func press(m model, k tea.KeyMsg) model {
	updated, _ := m.Update(k)
	return updated.(model)
}

func TestHistoryRetainsSamplesForWindow(t *testing.T) {
	m := feedPings(newModel("localhost"), 305)
	if len(m.samples) != 300 {
		t.Fatalf("got %d samples, want 300 (5m at 1s)", len(m.samples))
	}

	m = newModelWithInterval("localhost", 500*time.Millisecond)
	m.history = time.Minute
	m = feedPings(m, 130)
	if len(m.samples) != 120 {
		t.Fatalf("got %d samples, want 120 (1m at 0.5s)", len(m.samples))
	}
}

func TestScrollKeys(t *testing.T) {
	m := feedPings(newModel("localhost"), 200) // graph width is 48, so 152 samples can be scrolled back
	right := tea.KeyMsg{Type: tea.KeyRight}
	left := tea.KeyMsg{Type: tea.KeyLeft}
	runes := func(r string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)} }

	steps := []struct {
		name string
		key  tea.KeyMsg
		want int
	}{
		{"right at live stays live", right, 0},
		{"left", left, 1},
		{"h", runes("h"), 2},
		{"l", runes("l"), 1},
		{"right", right, 0},
		{"H is a full page", runes("H"), 48},
		{"shift+left is a full page", tea.KeyMsg{Type: tea.KeyShiftLeft}, 96},
		{"L is a full page", runes("L"), 48},
		{"shift+right is a full page", tea.KeyMsg{Type: tea.KeyShiftRight}, 0},
		{"pgup is a full page", tea.KeyMsg{Type: tea.KeyPgUp}, 48},
		{"ctrl+left is a full page", tea.KeyMsg{Type: tea.KeyCtrlLeft}, 96},
		{"ctrl+right is a full page", tea.KeyMsg{Type: tea.KeyCtrlRight}, 48},
		{"pgdown is a full page", tea.KeyMsg{Type: tea.KeyPgDown}, 0},
		{"left", left, 1},
		{"end returns to live", tea.KeyMsg{Type: tea.KeyEnd}, 0},
	}
	for _, step := range steps {
		m = press(m, step.key)
		if m.offset != step.want {
			t.Fatalf("%s: offset = %d, want %d", step.name, m.offset, step.want)
		}
	}

	for i := 0; i < 10; i++ {
		m = press(m, runes("H"))
	}
	if m.offset != 152 {
		t.Fatalf("offset = %d, want clamp at 152", m.offset)
	}
}

func TestScrollDoesNothingWhenHistoryFitsOnScreen(t *testing.T) {
	m := feedPings(newModel("localhost"), 10)
	m = press(m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.offset != 0 || strings.Contains(m.View(), "ago") {
		t.Fatalf("scrolled with nothing to scroll to: offset %d", m.offset)
	}
}

func TestScrolledGraphShowsOlderSamples(t *testing.T) {
	m := feedPings(newModel("localhost"), 100) // 1ms..100ms
	if !strings.Contains(m.renderGraph(), "200ms") {
		t.Fatal("live graph should be scaled to the newest samples")
	}
	if strings.Contains(m.View(), "ago") {
		t.Fatal("live view should not show the scrolled-back indicator")
	}

	for i := 0; i < 3; i++ {
		m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")})
	}
	if m.offset != 52 {
		t.Fatalf("offset = %d, want 52", m.offset)
	}
	graph := m.renderGraph()
	if strings.Contains(graph, "200ms") || !strings.Contains(graph, "60ms") {
		t.Fatalf("scrolled graph should be scaled to samples 1..48ms:\n%s", graph)
	}
	if view := m.View(); !strings.Contains(view, "52s ago") || !strings.Contains(view, "End live") {
		t.Fatalf("view does not show scrolled-back position:\n%s", view)
	}
	if !strings.Contains(m.View(), "MAX 100ms") {
		t.Fatal("footer stats should stay whole-session while scrolled")
	}

	m = press(m, tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(m.renderGraph(), "200ms") {
		t.Fatal("End should return the graph to live")
	}
}

func TestScrolledWindowStaysOnSameMomentAsPingsArrive(t *testing.T) {
	m := feedPings(newModel("localhost"), 100)
	m = press(m, tea.KeyMsg{Type: tea.KeyLeft})
	m = press(m, tea.KeyMsg{Type: tea.KeyLeft})
	before := m.renderGraph()

	updated, _ := m.Update(pingMsg{rtt: 500 * time.Millisecond})
	m = updated.(model)
	if m.offset != 3 {
		t.Fatalf("offset = %d, want 3 after a new ping", m.offset)
	}
	if m.renderGraph() != before {
		t.Fatal("graph changed while scrolled back")
	}

	m = press(m, tea.KeyMsg{Type: tea.KeyEnd})
	updated, _ = m.Update(pingMsg{rtt: time.Millisecond})
	if updated.(model).offset != 0 {
		t.Fatal("live view should stay live")
	}
}

func TestScrollOffsetFollowsHistoryTrim(t *testing.T) {
	m := newModel("localhost")
	m.history = 60 * time.Second
	m = feedPings(m, 60)
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")})
	if m.offset != 12 { // 60 samples - 48 visible
		t.Fatalf("offset = %d, want 12", m.offset)
	}
	m = feedPings(m, 30)
	if len(m.samples) != 60 || m.offset != 12 {
		t.Fatalf("got %d samples, offset %d; want 60 samples, offset clamped to 12", len(m.samples), m.offset)
	}
}

func TestScrollOffsetClampsWhenTerminalWidens(t *testing.T) {
	m := feedPings(newModel("localhost"), 60)
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")})
	m.width = 20 // graph narrows to 16, so more can be scrolled
	if got := m.maxOffset(); got != 44 {
		t.Fatalf("maxOffset = %d, want 44", got)
	}
	m.width = 200
	if got := m.scrollOffset(); got != 12 {
		t.Fatalf("scrollOffset = %d, want 12", got)
	}
	if strings.Count(m.renderGraph(), "\n") != graphHeight {
		t.Fatal("graph should still render after resize")
	}
}
