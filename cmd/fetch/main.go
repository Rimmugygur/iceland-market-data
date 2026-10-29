// Command fetch pulls daily price history for Nasdaq Iceland shares from the public
// Nasdaq Nordic API, keeps one CSV per symbol under data/prices, and writes
// data/latest.json with swing-trading metrics for every symbol.
//
// Usage: go run ./cmd/fetch [-days 730] [-data data] [-tickers tickers.txt]
package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const api = "https://api.nasdaq.com/api/nordic"

type Instrument struct {
	Symbol      string `json:"symbol"`
	OrderbookID string `json:"orderbookId"`
	Name        string `json:"name"`
	ISIN        string `json:"isin,omitempty"`
	Currency    string `json:"currency,omitempty"`
}

var client = &http.Client{Timeout: 15 * time.Second}

func get(u string, out any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
		req, _ := http.NewRequest("GET", u, nil)
		// The API rejects requests without browser-like headers.
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Origin", "https://www.nasdaq.com")
		req.Header.Set("Referer", "https://www.nasdaq.com/")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("GET %s: HTTP %d: %.300s", u, resp.StatusCode, body)
			continue
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("GET %s: decode: %w: %.300s", u, err, body)
		}
		return nil
	}
	return lastErr
}

// resolve finds the Nasdaq Nordic orderbook id of an Iceland-listed symbol.
func resolve(sym string) (Instrument, error) {
	var res struct {
		Data []struct {
			Group       string           `json:"group"`
			Instruments []map[string]any `json:"instruments"`
		} `json:"data"`
	}
	if err := get(api+"/search?searchText="+url.QueryEscape(sym), &res); err != nil {
		return Instrument{}, err
	}
	norm := func(s string) string {
		return strings.ToUpper(strings.NewReplacer(" ", "", ".", "", "_", "").Replace(s))
	}
	var fallback *Instrument
	for _, g := range res.Data {
		for _, in := range g.Instruments {
			ins := Instrument{
				Symbol:      str(in["symbol"]),
				OrderbookID: str(in["orderbookId"]),
				Name:        str(in["fullName"]),
				ISIN:        str(in["isin"]),
				Currency:    str(in["currency"]),
			}
			if ins.OrderbookID == "" || norm(ins.Symbol) != norm(sym) {
				continue
			}
			if strings.HasPrefix(ins.OrderbookID, "ICE") || ins.Currency == "ISK" {
				return ins, nil
			}
			if fallback == nil {
				c := ins
				fallback = &c
			}
		}
	}
	if fallback != nil && fallback.Currency == "ISK" {
		return *fallback, nil
	}
	return Instrument{}, fmt.Errorf("no ISK orderbook found for %q", sym)
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		s := strings.TrimSpace(strings.ReplaceAll(x, ",", ""))
		if s == "" || s == "-" {
			return 0
		}
		n, _ := strconv.ParseFloat(s, 64)
		return n
	}
	return 0
}

// pick returns the first value whose key matches one of the candidate names (case-insensitive).
func pick(row map[string]any, keys ...string) any {
	for _, k := range keys {
		for rk, rv := range row {
			if strings.EqualFold(rk, k) {
				if m, ok := rv.(map[string]any); ok { // some fields are {"value": "..."}
					return m["value"]
				}
				return rv
			}
		}
	}
	return nil
}

func history(ins Instrument, from, to time.Time) ([]Bar, error) {
	u := fmt.Sprintf("%s/instruments/%s/price-history?assetClass=SHARES&lang=en&fromDate=%s&toDate=%s",
		api, ins.OrderbookID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	var res struct {
		Data struct {
			PriceHistory struct {
				Rows []map[string]any `json:"rows"`
			} `json:"priceHistory"`
		} `json:"data"`
	}
	if err := get(u, &res); err != nil {
		return nil, err
	}
	var bars []Bar
	for _, r := range res.Data.PriceHistory.Rows {
		d := str(pick(r, "date", "tradeDate"))
		t, err := parseDate(d)
		if err != nil {
			continue
		}
		b := Bar{
			Date:     t.Format("2006-01-02"),
			Open:     num(pick(r, "openPrice", "open")),
			High:     num(pick(r, "highPrice", "high")),
			Low:      num(pick(r, "lowPrice", "low")),
			Close:    num(pick(r, "closePrice", "close", "lastPrice")),
			Volume:   num(pick(r, "totalVolume", "volume")),
			Turnover: num(pick(r, "turnover", "totalTurnover")),
		}
		if b.Close > 0 {
			bars = append(bars, b)
		}
	}
	if len(bars) == 0 && len(res.Data.PriceHistory.Rows) > 0 {
		raw, _ := json.Marshal(res.Data.PriceHistory.Rows[0])
		return nil, fmt.Errorf("could not parse rows; first row: %s", raw)
	}
	return bars, nil
}

func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "01/02/2006", "2006-01-02T15:04:05", "Jan 2, 2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad date %q", s)
}

var header = []string{"date", "open", "high", "low", "close", "volume", "turnover"}

func readCSV(path string) ([]Bar, error) {
	fh, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	recs, err := csv.NewReader(fh).ReadAll()
	if err != nil {
		return nil, err
	}
	var bars []Bar
	for i, r := range recs {
		if i == 0 || len(r) < 7 {
			continue
		}
		p := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
		bars = append(bars, Bar{r[0], p(r[1]), p(r[2]), p(r[3]), p(r[4]), p(r[5]), p(r[6])})
	}
	return bars, nil
}

func writeCSV(path string, bars []Bar) error {
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(fh)
	w.Write(header)
	ff := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	for _, b := range bars {
		w.Write([]string{b.Date, ff(b.Open), ff(b.High), ff(b.Low), ff(b.Close), ff(b.Volume), ff(b.Turnover)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

// merge keeps one bar per date; fresh data wins.
func merge(old, fresh []Bar) []Bar {
	byDate := map[string]Bar{}
	for _, b := range old {
		byDate[b.Date] = b
	}
	for _, b := range fresh {
		byDate[b.Date] = b
	}
	out := make([]Bar, 0, len(byDate))
	for _, b := range byDate {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func readTickers(path string) ([]string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var out []string
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}

func fileSafe(sym string) string { return strings.NewReplacer(" ", "_", "/", "_").Replace(sym) }

type Latest struct {
	GeneratedAt string    `json:"generatedAt"`
	Source      string    `json:"source"`
	Symbols     []Metrics `json:"symbols"`
	Errors      []string  `json:"errors,omitempty"`
}

func main() {
	days := flag.Int("days", 730, "history to request when a symbol has no CSV yet")
	dataDir := flag.String("data", "data", "output directory")
	tickersPath := flag.String("tickers", "tickers.txt", "symbols to track")
	flag.Parse()

	syms, err := readTickers(*tickersPath)
	if err != nil {
		log.Fatal(err)
	}
	pricesDir := filepath.Join(*dataDir, "prices")
	if err := os.MkdirAll(pricesDir, 0o755); err != nil {
		log.Fatal(err)
	}

	// Cached symbol -> orderbook id map.
	instPath := filepath.Join(*dataDir, "instruments.json")
	insts := map[string]Instrument{}
	if raw, err := os.ReadFile(instPath); err == nil {
		json.Unmarshal(raw, &insts)
	}

	now := time.Now().UTC()
	out := Latest{GeneratedAt: now.Format(time.RFC3339), Source: "Nasdaq Nordic API (api.nasdaq.com/api/nordic)"}
	ok, failed := 0, 0
	for _, sym := range syms {
		// Fail fast when the API is down, so a broken run doesn't burn Actions minutes on retries.
		if ok == 0 && failed >= 4 {
			msg := "aborting: first 4 symbols all failed, API looks unavailable"
			log.Print(msg)
			out.Errors = append(out.Errors, msg)
			break
		}
		ins, cached := insts[sym]
		if !cached || ins.OrderbookID == "" {
			ins, err = resolve(sym)
			if err != nil {
				msg := fmt.Sprintf("%s: resolve: %v", sym, err)
				log.Print(msg)
				out.Errors = append(out.Errors, msg)
				failed++
				continue
			}
			insts[sym] = ins
			time.Sleep(250 * time.Millisecond)
		}

		csvPath := filepath.Join(pricesDir, fileSafe(sym)+".csv")
		old, err := readCSV(csvPath)
		if err != nil {
			log.Printf("%s: read csv: %v", sym, err)
		}
		from := now.AddDate(0, 0, -*days)
		if len(old) > 0 {
			if t, err := time.Parse("2006-01-02", old[len(old)-1].Date); err == nil {
				from = t.AddDate(0, 0, -10) // overlap a little to pick up corrections
			}
		}
		fresh, err := history(ins, from, now)
		if err != nil {
			msg := fmt.Sprintf("%s (%s): history: %v", sym, ins.OrderbookID, err)
			log.Print(msg)
			out.Errors = append(out.Errors, msg)
			failed++
		}
		bars := merge(old, fresh)
		if len(bars) > 0 {
			if err := writeCSV(csvPath, bars); err != nil {
				log.Fatal(err)
			}
		}
		m := computeMetrics(sym, bars)
		m.Name = ins.Name
		out.Symbols = append(out.Symbols, m)
		if len(fresh) > 0 {
			ok++
		}
		time.Sleep(250 * time.Millisecond)
	}

	raw, _ := json.MarshalIndent(insts, "", "  ")
	if err := os.WriteFile(instPath, raw, 0o644); err != nil {
		log.Fatal(err)
	}
	raw, _ = json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(filepath.Join(*dataDir, "latest.json"), raw, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("updated %d/%d symbols, %d errors", ok, len(syms), len(out.Errors))
	if ok == 0 {
		os.Exit(1) // fail the workflow so a broken API is noticed
	}
}
