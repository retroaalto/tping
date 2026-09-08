package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestModelRecordsRollingSamples(t *testing.T) {
	m := newModel("localhost")
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
