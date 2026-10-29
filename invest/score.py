#!/usr/bin/env python3
"""Quality and value scores for Björn's watchlist, from the digest's stored scorecards.

Usage:
    python3 -I invest/score.py input.json > ranking.json

input.json:
    {"tenYear": 4.05,                       # US 10-year Treasury yield, % (fallback risk-free rate)
     "rates": {"USD": 4.05, "EUR": 2.6, "ISK": 6.9, ...},  # optional: 10y government yield per currency
     "items": [{"symbol": "MCO",
                "role": "holding" | "watch" | "suggestion" | "idea",
                "scorecard": {...}}]}       # a doc from the watchlist's `scorecards` collection

Output: {"ranking": [...], "swaps": [...], "method": {...}} — see README in this folder.

Two scores, never blended:
  quality 0-100  how good the business is (Cunningham: cash first, trends over snapshots)
  value   0-100  how attractive today's price is: expected return (FCF or earnings yield + 2/3 of growth capped
                 at 8% + half the drift of P/FCF toward 25x, or P/E toward 15x, over 10 years) minus the
                 10y government yield in the stock's currency, blended with P/FCF vs its own 5y range
Red flags cap quality at 49, so a flagged company can never rank as "great".
"""
import json
import statistics as st
import sys

GREAT = 65      # quality at or above this is a great business
FAIR = 50       # value at or above this is a fair-or-better price (expected return >= 10y + 3 pts)
GROWTH_CREDIT = 2 / 3  # share of historical growth we assume continues...
GROWTH_CAP = 8.0       # ...capped at this many % a year
FAIR_PFCF = 25.0       # multiples drift toward this P/FCF (P/E 15 for financials)...
FAIR_PE = 15.0
REVERT_YEARS = 10      # ...over this many years...
REVERT_WEIGHT = 0.5    # ...and we count half of that drift (Buffett's margin of safety without assuming full mean reversion)
FLAG_CAP = 49   # quality ceiling when a red flag fires
SWAP_GAP = 15   # a watch name this many quality points above a holding...
SWAP_VALUE_SLACK = 5  # ...at a value no more than this many points worse triggers a swap alert
MAX_SWAPS = 3         # show at most this many swap ideas a day (one per holding)

COMPANY_WEIGHTS = {"cash": 30, "returns": 25, "consistency": 15, "balance": 15, "workingCapital": 10, "owner": 5}
FINANCIAL_WEIGHTS = {"returns": 35, "growth": 25, "efficiency": 15, "capital": 15, "owner": 10}


def lin(x, bad, good):
    """Map x linearly so that `bad` -> 0 and `good` -> 100, clamped. Works for either direction."""
    if x is None:
        return None
    t = (x - bad) / (good - bad)
    return max(0.0, min(100.0, t * 100.0))


def wavg(parts):
    """Weighted average of (score, weight) pairs, skipping missing scores."""
    parts = [(s, w) for s, w in parts if s is not None]
    if not parts:
        return None
    return sum(s * w for s, w in parts) / sum(w for _, w in parts)


def num(v):
    return v if isinstance(v, (int, float)) and not isinstance(v, bool) else None


def cagr(a, b, years):
    if a is None or b is None or a <= 0 or b <= 0 or years <= 0:
        return None
    return ((b / a) ** (1 / years) - 1) * 100


def series(years, key):
    return [num(y.get(key)) for y in years]


def company_quality(sc):
    years = [y for y in sc.get("years") or [] if num(y.get("ocf")) is not None]
    L = sc.get("latest") or {}
    n = len(years) - 1
    ocf, fcf, ni = series(years, "ocf"), series(years, "fcf"), series(years, "netIncome")
    rets = [r for r in (num(y.get("roic")) if num(y.get("roic")) is not None else num(y.get("roe")) for y in years) if r is not None]
    gm = [g for g in series(years, "grossMargin") if g is not None]
    ccc = [c for c in series(years, "ccc") if c is not None]

    ratios = [o / i for o, i in zip(ocf, ni) if o is not None and i and i > 0]
    ocf_ni = st.mean(ratios) if ratios else num(L.get("ocfToNi"))
    ocf_cagr = cagr(ocf[0], ocf[-1], n) if n >= 2 else None
    fcf_cagr = cagr(fcf[0], fcf[-1], n) if n >= 2 else None
    fcf_margin = num(L.get("fcfMargin"))
    down_years = sum(1 for a, b in zip(ocf, ocf[1:]) if a is not None and b is not None and b < a)
    ret_last = num(L.get("roic")) if num(L.get("roic")) is not None else (rets[-1] if rets else None)
    ret_min = min(rets) if rets else None
    gm_sd = st.pstdev(gm) if len(gm) >= 3 else None
    ret_trend = (rets[-1] / rets[0]) if len(rets) >= 3 and rets[0] > 0 else None
    nd_fcf = num(L.get("netDebtToFcf"))
    cover = num(L.get("interestCoverage"))
    ccc_now = num(L.get("ccc")) if num(L.get("ccc")) is not None else (ccc[-1] if ccc else None)
    ccc_change = (ccc[-1] - ccc[0]) if len(ccc) >= 2 else None
    shares = num(L.get("shareCountChange5y"))

    parts = {
        "cash": wavg([(lin(ocf_ni, 0.8, 1.3), 35), (lin(ocf_cagr, 0, 12), 30), (lin(fcf_cagr, 0, 10), 20), (lin(fcf_margin, 5, 25), 15)]),
        "returns": wavg([(lin(ret_min, 5, 20), 50), (lin(ret_last, 8, 25), 50)]),
        "consistency": wavg([({0: 100, 1: 70, 2: 40}.get(down_years, 0) if n >= 2 else None, 50),
                             (lin(gm_sd, 4, 1), 25), (lin(ret_trend, 0.5, 1.0), 25)]),
        "balance": wavg([(lin(nd_fcf, 4, 0), 70),
                         (lin(cover, 3, 15) if cover is not None else (100 if nd_fcf is not None and nd_fcf <= 1 else None), 30)]),
        "workingCapital": wavg([(lin(ccc_change, 40, 0), 60), (lin(ccc_now, 120, 30), 40)]),
        "owner": lin(shares, 10, -5),
    }
    flags = []
    if ocf_ni is not None and ocf_ni < 0.8:
        flags.append("OCF below net income")
    if nd_fcf is not None and nd_fcf > 4:
        flags.append(f"Net debt {nd_fcf:.1f}× FCF")
    if ret_last is not None and rets and ret_last < 15 and ret_last < 0.6 * max(rets):
        flags.append(f"Returns eroding: ROIC {ret_last:.0f}% vs {max(rets):.0f}% peak")
    if shares is not None and shares > 10:
        flags.append(f"Dilution {shares:+.0f}% in 5y")
    if fcf and fcf[-1] is not None and fcf[-1] < 0:
        flags.append("Negative FCF")
    inputs = {"growth": min(g for g in (ocf_cagr, fcf_cagr) if g is not None) if (ocf_cagr is not None or fcf_cagr is not None) else None,
              "ocfToNi5y": ocf_ni, "ocfCagr": ocf_cagr, "fcfCagr": fcf_cagr, "fcfMargin": fcf_margin, "retMin": ret_min,
              "retLast": ret_last, "ocfDownYears": down_years, "grossMarginSd": gm_sd, "netDebtFcf": nd_fcf,
              "cccChange": ccc_change, "shareChange5y": shares}
    return parts, COMPANY_WEIGHTS, flags, inputs


def financial_quality(sc):
    years = sc.get("years") or []
    L = sc.get("latest") or {}
    roes = [r for r in series(years, "roe") if r is not None]
    if not roes and num(L.get("roe")) is not None:
        roes = [L["roe"]]
    ni = [x for x in series(years, "netIncome") if x is not None]
    bvps = num(L.get("bvpsGrowth5y"))
    growth = bvps if bvps is not None else (cagr(ni[0], ni[-1], len(ni) - 1) if len(ni) >= 3 else None)
    cti = num(L.get("costToIncome"))
    cap = num(L.get("cet1")) if num(L.get("cet1")) is not None else num(L.get("capitalRatio"))
    shares = num(L.get("shareCountChange5y"))
    parts = {
        "returns": wavg([(lin(st.mean(roes) if roes else None, 8, 20), 60), (lin(min(roes) if roes else None, 5, 15), 40)]),
        "growth": lin(growth, 0, 12),
        "efficiency": lin(cti, 75, 45),
        "capital": lin(cap, 9, 15),
        "owner": lin(shares, 10, -5),
    }
    flags = []
    if roes and min(roes) < 5:
        flags.append("ROE below 5% in a recent year")
    if shares is not None and shares > 10:
        flags.append(f"Dilution {shares:+.0f}% in 5y")
    inputs = {"growth": growth, "roeAvg": st.mean(roes) if roes else None, "roeMin": min(roes) if roes else None, "growth": growth,
              "costToIncome": cti, "capitalRatio": cap, "shareChange5y": shares}
    return parts, FINANCIAL_WEIGHTS, flags, inputs


def value_score(sc, ten_year, growth):
    """Buffett-style: expected return = cash yield + a conservative share of growth + half the drift of the
    multiple toward a fair level, vs the government 10-year in the stock's currency. Blended with where
    P/FCF sits in its own 5y range (pFcf5yLow/High in the scorecard) when known."""
    L = sc.get("latest") or {}
    financial = sc.get("kind") == "financial"
    pe, pfcf = num(L.get("pe")), num(L.get("pFcf"))
    if financial:
        yld = 100 / pe if pe and pe > 0 else num(L.get("fcfYield"))
        basis = "earnings yield"
        mult, fair = pe, FAIR_PE
    else:
        yld = num(L.get("fcfYield"))
        basis = "FCF yield"
        mult, fair = (pfcf if pfcf and pfcf > 0 else (100 / yld if yld and yld > 0 else None)), FAIR_PFCF
    g = max(0.0, min(GROWTH_CAP, (growth or 0.0) * GROWTH_CREDIT))
    rerate = (((fair / mult) ** (1 / REVERT_YEARS) - 1) * 100 * REVERT_WEIGHT) if mult else 0.0
    exp_ret = (yld + g + rerate) if yld is not None else None
    excess = (exp_ret - ten_year) if exp_ret is not None and ten_year is not None else None
    lo, hi, now = num(L.get("pFcf5yLow")), num(L.get("pFcf5yHigh")), num(L.get("pFcf"))
    pos = ((now - lo) / (hi - lo)) if None not in (lo, hi, now) and hi > lo else None
    score = wavg([(lin(excess, -2, 8), 70), (lin(pos, 1, 0), 30)])
    return score, {"basis": basis, "yield": yld, "growthCredit": g, "rerating": rerate, "expectedReturn": exp_ret,
                   "riskFree": ten_year, "excessVs10y": excess, "rangePos": pos}


def quadrant(q, v):
    if q is None:
        return "Pending"
    if v is None:
        return ("Great" if q >= GREAT else "Weaker") + " · no price"
    great, fair = q >= GREAT, v >= FAIR
    return {(True, True): "Great · fair price", (True, False): "Great · pricey",
            (False, True): "Weaker · cheap", (False, False): "Weaker · pricey"}[(great, fair)]


def r1(x):
    return None if x is None else round(x, 1)


def risk_free(sc, ten_year, rates):
    """Government 10-year yield in the stock's currency when given, else the US 10-year."""
    cur = str(sc.get("currency") or "").upper()
    return num((rates or {}).get(cur)) if num((rates or {}).get(cur)) is not None else ten_year


def score_item(item, ten_year, rates=None):
    sc = item.get("scorecard") or {}
    if not sc.get("years") and not sc.get("latest"):
        return {"symbol": item["symbol"], "role": item.get("role"), "quality": None, "value": None, "quadrant": "Pending", "flags": []}
    if sc.get("kind") == "financial":
        parts, weights, flags, inputs = financial_quality(sc)
    else:
        parts, weights, flags, inputs = company_quality(sc)
    q = wavg([(parts[k], w) for k, w in weights.items()])
    raw = q
    if q is not None and flags:
        q = min(q, FLAG_CAP)
    v, vin = value_score(sc, risk_free(sc, ten_year, rates), inputs.get("growth"))
    return {
        "symbol": item["symbol"], "name": sc.get("name"), "role": item.get("role"), "kind": sc.get("kind", "company"),
        "quality": r1(q), "qualityRaw": r1(raw), "value": r1(v), "quadrant": quadrant(q, v), "flags": flags,
        "parts": {k: r1(s) for k, s in parts.items()}, "inputs": {k: r1(x) if isinstance(x, float) else x for k, x in {**inputs, **vin}.items()},
        "period": sc.get("period"),
    }


def find_swaps(rows):
    """For each holding, the single best non-held name that is itself a great business at a fair price and beats
    the holding by SWAP_GAP quality points at a value no more than SWAP_VALUE_SLACK worse. At most MAX_SWAPS."""
    holdings = [r for r in rows if r.get("role") == "holding" and r.get("quality") is not None]
    pool = [r for r in rows if r.get("role") != "holding" and r.get("quality") is not None and r.get("value") is not None
            and r["quality"] >= GREAT and r["value"] >= FAIR]
    swaps = []
    for h in holdings:
        hv = h.get("value") or 0
        best = [w for w in pool if w["quality"] >= h["quality"] + SWAP_GAP and w["value"] >= hv - SWAP_VALUE_SLACK]
        if best:
            w = max(best, key=lambda x: (x["quality"] + x["value"]))
            swaps.append({"holding": h["symbol"], "candidate": w["symbol"], "qualityGap": r1(w["quality"] - h["quality"]),
                          "valueGap": r1(w["value"] - hv)})
    swaps.sort(key=lambda s: -s["qualityGap"])
    return swaps[:MAX_SWAPS]


def main():
    data = json.load(open(sys.argv[1]) if len(sys.argv) > 1 else sys.stdin)
    ten = num(data.get("tenYear"))
    rates = data.get("rates") or {}
    rows = [score_item(it, ten, rates) for it in data.get("items", [])]
    rows.sort(key=lambda r: (r["quality"] is None, -(r["quality"] or 0), -(r["value"] or 0)))
    for i, r in enumerate(x for x in rows if x["quality"] is not None):
        r["rank"] = i + 1
    swaps = find_swaps(rows)
    json.dump({"tenYear": ten, "rates": rates, "ranking": rows, "swaps": swaps,
               "method": {"great": GREAT, "fair": FAIR, "flagCap": FLAG_CAP, "companyWeights": COMPANY_WEIGHTS,
                          "financialWeights": FINANCIAL_WEIGHTS, "swapGap": SWAP_GAP}}, sys.stdout, indent=1, ensure_ascii=False)
    print()


if __name__ == "__main__":
    main()
