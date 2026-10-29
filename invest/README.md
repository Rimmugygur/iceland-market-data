# Watchlist scoring

`score.py` ranks holdings, watch names and the digest's suggestions using the scorecards the daily investing digest stores. It has no dependencies; run `python3 -I invest/score.py input.json`.

## Quality, 0–100: is it a great business?

| Weight | Area | Inputs (good → 100, bad → 0) |
|---|---|---|
| 30 | Cash | OCF/NI 5y avg (0.8→1.3), OCF growth/yr (0→12%), FCF growth/yr (0→10%), FCF margin (5→25%) |
| 25 | Returns | worst ROIC in 5y (5→20%), latest ROIC (8→25%) |
| 15 | Consistency | years OCF fell (0 best), gross-margin swings (sd 4→1 pt), ROIC now vs 5y ago |
| 15 | Balance sheet | net debt/FCF (4→0×), interest cover (3→15×) |
| 10 | Working capital | change in cash conversion cycle (+40→0 days), CCC level (120→30 days) |
| 5 | Owner | share count over 5y (+10%→−5%) |

Banks, card issuers and insurers use: ROE level and worst year (35), book-value or earnings growth (25), cost/income (15), capital ratio (15), share count (10).

**Red flags cap quality at 49:** OCF below net income on average, net debt above 4× FCF, returns eroding (ROIC under 15% and under 60% of its 5y peak), dilution above 10% in 5 years, negative FCF.

## Value, 0–100: is the price attractive?

Expected return = FCF yield (earnings yield for financials)
+ ⅔ of the slower of OCF and FCF growth, capped at 8%/yr
+ half the yearly drift if P/FCF moved to 25× (P/E to 15× for financials) over 10 years. A 79× P/FCF stock loses about 5.5 pts a year here; a 21× one gains about 1.

Value scores the excess over the 10-year government yield in the stock's currency (`rates` in the input, falling back to the US 10-year), −2 pts → 0 and +8 pts → 100. It's blended 70/30 with where P/FCF sits in its own 5-year range when the scorecard has `pFcf5yLow`/`pFcf5yHigh`. A stock with no price (pre-IPO) gets no value score and the group "… · no price".

## Groups and swaps

- Quality ≥ 65 is a great business; value ≥ 50 is a fair price (expected return at least 3 pts over the 10-year).
- Groups: Great · fair price, Great · pricey, Weaker · cheap, Weaker · pricey.
- Swap alert: for each holding, the one best non-held name that is itself great at a fair price, at least 15 quality points above the holding and at a value no more than 5 points worse. The weakest holdings pick first and each candidate is used once, so the alerts offer different alternatives. At most 3 a day.

Tests: `python3 -I -m unittest discover -s invest`.
