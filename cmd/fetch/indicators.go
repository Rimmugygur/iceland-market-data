package main

import (
	"math"
	"sort"
)

// Bar is one trading day.
type Bar struct {
	Date     string  `json:"date"` // YYYY-MM-DD
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
	Turnover float64 `json:"turnover"` // ISK
}

// Metrics is the per-symbol summary the scan agent reads.
type Metrics struct {
	Symbol      string   `json:"symbol"`
	Name        string   `json:"name,omitempty"`
	LastDate    string   `json:"lastDate"`
	Bars        int      `json:"bars"`
	Close       float64  `json:"close"`
	Ret1d       *float64 `json:"ret1d"`
	Ret5d       *float64 `json:"ret5d"`
	Ret20d      *float64 `json:"ret20d"`
	Ret60d      *float64 `json:"ret60d"`
	SMA20       *float64 `json:"sma20"`
	SMA50       *float64 `json:"sma50"`
	SMA200      *float64 `json:"sma200"`
	SMA50Slope  *float64 `json:"sma50Slope20d"` // % change of SMA50 over 20 bars
	RSI14       *float64 `json:"rsi14"`
	ATR14Pct    *float64 `json:"atr14Pct"`
	High52w     *float64 `json:"high52w"`
	Low52w      *float64 `json:"low52w"`
	Pos3m       *float64 `json:"pos3m"`          // 0 = at 63-day low, 1 = at 63-day high
	DistSMA50   *float64 `json:"distSma50Atr"`   // (close - SMA50) / ATR14
	SwingAmp    *float64 `json:"swingAmp20dPct"` // median 20-day high/low range over the last year, %
	Turnover20  *float64 `json:"avgTurnover20dIsk"`
	ZeroVolDays int      `json:"zeroVolumeDays20d"`
	Regime      string   `json:"regime"` // uptrend | downtrend | range | unknown
}

func f(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	r := math.Round(v*10000) / 10000
	return &r
}

func closes(b []Bar) []float64 {
	out := make([]float64, len(b))
	for i, x := range b {
		out[i] = x.Close
	}
	return out
}

func sma(xs []float64, n int) float64 {
	if len(xs) < n || n <= 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range xs[len(xs)-n:] {
		s += x
	}
	return s / float64(n)
}

func ret(xs []float64, n int) float64 {
	if len(xs) <= n || xs[len(xs)-1-n] == 0 {
		return math.NaN()
	}
	return (xs[len(xs)-1]/xs[len(xs)-1-n] - 1) * 100
}

// rsi uses Wilder smoothing.
func rsi(xs []float64, n int) float64 {
	if len(xs) <= n {
		return math.NaN()
	}
	var gain, loss float64
	for i := 1; i <= n; i++ {
		d := xs[i] - xs[i-1]
		if d > 0 {
			gain += d
		} else {
			loss -= d
		}
	}
	gain /= float64(n)
	loss /= float64(n)
	for i := n + 1; i < len(xs); i++ {
		d := xs[i] - xs[i-1]
		g, l := 0.0, 0.0
		if d > 0 {
			g = d
		} else {
			l = -d
		}
		gain = (gain*float64(n-1) + g) / float64(n)
		loss = (loss*float64(n-1) + l) / float64(n)
	}
	if loss == 0 {
		if gain == 0 {
			return 50
		}
		return 100
	}
	return 100 - 100/(1+gain/loss)
}

// atrPct is ATR(n) as % of the last close. Falls back to close-to-close moves when high/low are missing.
func atrPct(b []Bar, n int) float64 {
	if len(b) <= n {
		return math.NaN()
	}
	trs := make([]float64, 0, len(b)-1)
	for i := 1; i < len(b); i++ {
		pc := b[i-1].Close
		h, l := b[i].High, b[i].Low
		if h == 0 || l == 0 {
			h, l = math.Max(b[i].Close, pc), math.Min(b[i].Close, pc)
		}
		tr := math.Max(h-l, math.Max(math.Abs(h-pc), math.Abs(l-pc)))
		trs = append(trs, tr)
	}
	atr := 0.0
	for _, t := range trs[:n] {
		atr += t
	}
	atr /= float64(n)
	for _, t := range trs[n:] {
		atr = (atr*float64(n-1) + t) / float64(n)
	}
	last := b[len(b)-1].Close
	if last == 0 {
		return math.NaN()
	}
	return atr / last * 100
}

func minMax(xs []float64) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, x := range xs {
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	return lo, hi
}

func tail[T any](xs []T, n int) []T {
	if len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}

// swingAmp is the median, over every 20-bar window in the last 250 bars, of (max-min)/min in %.
func swingAmp(xs []float64) float64 {
	xs = tail(xs, 250)
	if len(xs) < 40 {
		return math.NaN()
	}
	var amps []float64
	for i := 20; i <= len(xs); i++ {
		lo, hi := minMax(xs[i-20 : i])
		if lo > 0 {
			amps = append(amps, (hi-lo)/lo*100)
		}
	}
	sort.Float64s(amps)
	return amps[len(amps)/2]
}

func computeMetrics(sym string, b []Bar) Metrics {
	m := Metrics{Symbol: sym, Bars: len(b), Regime: "unknown"}
	if len(b) == 0 {
		return m
	}
	c := closes(b)
	last := b[len(b)-1]
	m.LastDate, m.Close = last.Date, last.Close
	m.Ret1d, m.Ret5d, m.Ret20d, m.Ret60d = f(ret(c, 1)), f(ret(c, 5)), f(ret(c, 20)), f(ret(c, 60))
	s20, s50, s200 := sma(c, 20), sma(c, 50), sma(c, 200)
	m.SMA20, m.SMA50, m.SMA200 = f(s20), f(s50), f(s200)
	if len(c) >= 70 {
		prev := sma(c[:len(c)-20], 50)
		m.SMA50Slope = f((s50/prev - 1) * 100)
	}
	m.RSI14 = f(rsi(c, 14))
	ap := atrPct(b, 14)
	m.ATR14Pct = f(ap)
	lo52, hi52 := minMax(tail(c, 250))
	m.Low52w, m.High52w = f(lo52), f(hi52)
	if len(c) >= 63 {
		lo, hi := minMax(tail(c, 63))
		if hi > lo {
			m.Pos3m = f((last.Close - lo) / (hi - lo))
		}
	}
	if !math.IsNaN(s50) && !math.IsNaN(ap) && ap > 0 {
		m.DistSMA50 = f((last.Close - s50) / (ap / 100 * last.Close))
	}
	m.SwingAmp = f(swingAmp(c))
	var to float64
	recent := tail(b, 20)
	for _, x := range recent {
		to += x.Turnover
		if x.Volume == 0 {
			m.ZeroVolDays++
		}
	}
	m.Turnover20 = f(to / float64(len(recent)))

	switch {
	case math.IsNaN(s50) || math.IsNaN(s200) || m.SMA50Slope == nil:
		m.Regime = "unknown"
	case last.Close > s50 && s50 > s200*1.02 && *m.SMA50Slope > 1:
		m.Regime = "uptrend"
	case last.Close < s50 && s50 < s200*0.98 && *m.SMA50Slope < -1:
		m.Regime = "downtrend"
	default:
		m.Regime = "range"
	}
	return m
}
