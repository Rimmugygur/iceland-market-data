# iceland-market-data

Daily end-of-day prices for every share on Nasdaq Iceland, plus swing-trading metrics. It feeds the "Iceland market scan" scheduled task.

- A GitHub Action (`.github/workflows/fetch.yml`) runs weekdays at 15:50 UTC, after the 15:30 close. It can also be started manually from the Actions tab.
- `go run ./cmd/fetch` resolves each symbol in `tickers.txt` to its Nasdaq Nordic orderbook id and pulls the price history. It merges the new bars into `data/prices/<SYMBOL>.csv` and writes `data/latest.json`.
- The first run backfills 2 years. Later runs fetch only the most recent days.

## data/latest.json

There is one entry per symbol:

| field | meaning |
|---|---|
| `lastDate`, `close` | last trading day and closing price (ISK) |
| `ret1d/5d/20d/60d` | % return over N trading days |
| `sma20/50/200` | simple moving averages |
| `sma50Slope20d` | % change of SMA50 over the last 20 bars |
| `rsi14` | Wilder RSI |
| `atr14Pct` | ATR(14) as % of close |
| `high52w`, `low52w` | 250-day close range |
| `pos3m` | where the close sits in its 63-day range (0 = low, 1 = high) |
| `distSma50Atr` | (close − SMA50) in ATR units |
| `swingAmp20dPct` | median 20-day high/low range over the last year, i.e. how far it typically swings in a month |
| `avgTurnover20dIsk`, `zeroVolumeDays20d` | liquidity |
| `regime` | `uptrend` / `downtrend` / `range` / `unknown` |

Source: the public Nasdaq Nordic API used by nasdaq.com. It isn't an official API, so if a run fails, check the error list in `latest.json` and the Action log.
