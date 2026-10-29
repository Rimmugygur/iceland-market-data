import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import score  # noqa: E402


def company(ocf, fcf, ni, roic, gm=None, **latest):
    years = [{"fy": f"FY{20 + i}", "ocf": o, "fcf": f, "netIncome": n, "roic": r, "grossMargin": (gm or [50] * len(ocf))[i]}
             for i, (o, f, n, r) in enumerate(zip(ocf, fcf, ni, roic))]
    return {"kind": "company", "years": years, "latest": latest}


class ScoreTest(unittest.TestCase):
    def test_compounder_beats_decliner(self):
        good = score.score_item({"symbol": "G", "role": "watch", "scorecard": company(
            [100, 112, 125, 140, 157], [80, 90, 101, 113, 127], [80, 90, 100, 112, 125], [22, 23, 24, 25, 26],
            fcfMargin=25, netDebtToFcf=0.5, interestCoverage=30, ccc=20, shareCountChange5y=-4, fcfYield=4)}, 4.0)
        bad = score.score_item({"symbol": "B", "role": "holding", "scorecard": company(
            [100, 95, 90, 85, 80], [60, 50, 45, 40, 30], [110, 110, 105, 100, 100], [18, 15, 12, 10, 8],
            fcfMargin=6, netDebtToFcf=3.5, interestCoverage=4, ccc=110, shareCountChange5y=3, fcfYield=5)}, 4.0)
        self.assertGreater(good["quality"], 75)
        self.assertLess(bad["quality"], 40)
        self.assertTrue(any(f.startswith("Returns eroding") for f in bad["flags"]))

    def test_flag_caps_quality(self):
        r = score.score_item({"symbol": "L", "role": "holding", "scorecard": company(
            [100, 110, 120, 130, 140], [70, 77, 84, 91, 98], [90, 95, 100, 105, 110], [20, 21, 22, 23, 24],
            fcfMargin=25, netDebtToFcf=6.0, interestCoverage=5, ccc=20, shareCountChange5y=-3, fcfYield=4)}, 4.0)
        self.assertLessEqual(r["quality"], score.FLAG_CAP)
        self.assertGreater(r["qualityRaw"], score.FLAG_CAP)

    def test_value_rewards_growth_not_just_yield(self):
        sc = company([100, 112, 125, 140, 157], [80, 90, 101, 113, 127], [80, 90, 100, 112, 125], [22] * 5, fcfYield=3.5)
        flat = company([100] * 5, [80] * 5, [80] * 5, [22] * 5, fcfYield=3.5)
        g = score.score_item({"symbol": "G", "scorecard": sc}, 5.0)["value"]
        f = score.score_item({"symbol": "F", "scorecard": flat}, 5.0)["value"]
        self.assertGreater(g, f)

    def test_financial_and_pending(self):
        fin = {"kind": "financial", "years": [{"roe": r, "netIncome": n} for r, n in [(30, 7), (32, 8), (34, 9)]],
               "latest": {"roe": 34, "pe": 18, "bvpsGrowth5y": 12, "costToIncome": 55, "cet1": 13, "shareCountChange5y": -10}}
        r = score.score_item({"symbol": "F", "scorecard": fin}, 4.5)
        self.assertGreater(r["quality"], 70)
        self.assertEqual(score.score_item({"symbol": "P", "scorecard": {}}, 4.5)["quadrant"], "Pending")


if __name__ == "__main__":
    unittest.main()
