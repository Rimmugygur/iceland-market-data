package main

import (
	"math"
	"testing"
	"time"
)

func synth(n int, fn func(i int) float64) []Bar {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]Bar, n)
	for i := range out {
		c := fn(i)
		out[i] = Bar{Date: start.AddDate(0, 0, i).Format("2006-01-02"), Open: c, High: c * 1.01, Low: c * 0.99, Close: c, Volume: 1000, Turnover: 1000 * c}
	}
	return out
}

func TestUptrend(t *testing.T) {
	m := computeMetrics("UP", synth(300, func(i int) float64 { return 100 * math.Pow(1.002, float64(i)) }))
	if m.Regime != "uptrend" {
		t.Fatalf("regime = %s", m.Regime)
	}
	if *m.RSI14 != 100 {
		t.Fatalf("rsi = %v, want 100 for monotonic rise", *m.RSI14)
	}
	if *m.Pos3m != 1 {
		t.Fatalf("pos3m = %v", *m.Pos3m)
	}
}

func TestRange(t *testing.T) {
	// 25-bar sine wave, ±10% around 100: SMA50 and SMA200 both sit on the mean.
	m := computeMetrics("RNG", synth(300, func(i int) float64 { return 100 + 10*math.Sin(2*math.Pi*float64(i)/25) }))
	if m.Regime != "range" {
		t.Fatalf("regime = %s", m.Regime)
	}
	if m.SwingAmp == nil || *m.SwingAmp < 15 || *m.SwingAmp > 25 {
		t.Fatalf("swingAmp = %v, want ~20%%", m.SwingAmp)
	}
	if *m.RSI14 < 0 || *m.RSI14 > 100 {
		t.Fatalf("rsi out of bounds: %v", *m.RSI14)
	}
}

func TestShortHistory(t *testing.T) {
	m := computeMetrics("NEW", synth(10, func(i int) float64 { return 50 }))
	if m.SMA50 != nil || m.Regime != "unknown" {
		t.Fatalf("expected nil SMA50 and unknown regime, got %v %s", m.SMA50, m.Regime)
	}
	if m.ZeroVolDays != 0 || m.Turnover20 == nil {
		t.Fatal("turnover not computed")
	}
}

func TestMerge(t *testing.T) {
	old := []Bar{{Date: "2026-01-01", Close: 1}, {Date: "2026-01-02", Close: 2}}
	fresh := []Bar{{Date: "2026-01-02", Close: 3}, {Date: "2026-01-03", Close: 4}}
	got := merge(old, fresh)
	if len(got) != 3 || got[1].Close != 3 || got[2].Date != "2026-01-03" {
		t.Fatalf("merge = %+v", got)
	}
}

func TestPickNested(t *testing.T) {
	row := map[string]any{"ClosePrice": map[string]any{"value": "1,234.5"}, "date": "2026-10-28"}
	if v := num(pick(row, "closePrice")); v != 1234.5 {
		t.Fatalf("got %v", v)
	}
}
