// /tools/mortgage (static/js/mortgage.js) against Rob's spreadsheet,
// "CPW Mortgage Freedom Calculator Aug 26".
//
// mortgage-excel.json holds 60 cases (excel/gen-cases.mjs) and what Excel
// worked out for each, recalculated over COM on 19 November 2026
// (excel/excel-capture.ps1, which says how to rerun it). The spreadsheet
// itself isn't in git.
//
// One known difference, a fix listed at the top of mortgage.js: with no extra
// repayment, floating point leaves a few millionths of a cent owing after the
// last scheduled repayment. The sheet then counts one more repayment (or gives
// #N/A when that would be past its 2,600 rows); the page counts under half a
// cent as paid.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { calc, payoffDate, timeText } = require("../../static/js/mortgage.js");

const cases = JSON.parse(readFileSync(new URL("mortgage-excel.json", import.meta.url), "utf8"));
const PPY = { Weekly: 52, Fortnightly: 26, Monthly: 12 };
const HALF_CENT = 0.005;

test("every mortgage figure matches Excel to the cent", () => {
  assert.equal(cases.length, 60);
  let figures = 0;
  const bad = [];
  const near = (js, xl, what, i) => {
    if (xl === null) return;
    figures++;
    if (!(Math.abs(js - xl) < HALF_CENT)) bad.push(`#${i} ${what}: js ${js}, excel ${xl}`);
  };

  cases.forEach((c, i) => {
    const o = c.input, ppy = PPY[o.freq];
    const r = calc({ loan: o.loan, rate: o.rate, term: o.term, ppy, extra: o.extra });
    near(r.regular, c.regular, "regular repayment", i);
    near(r.baseInterest, c.baseInterest, "original interest", i);
    near(r.interest, c.interest, "interest with extras", i);
    near(r.saved, c.saved, "interest saved", i);
    for (const s of c.samples) {
      if (s.period < r.base.length) near(r.base[s.period], s.base, `balance after ${s.period}`, i);
      else near(0, s.base, `balance after ${s.period}`, i);
      if (s.period < r.withExtra.length) near(r.withExtra[s.period], s.withExtra, `balance with extra after ${s.period}`, i);
      else near(0, s.withExtra, `balance with extra after ${s.period}`, i);
    }

    const n = Math.round(o.term * ppy);
    if (o.extra === 0) {
      // The known difference: the page pays off on schedule. (Above about
      // 17% a year, more than half a cent can be left owing too: see the
      // last test.)
      if (o.rate <= 0.15) assert.equal(r.periods, n, `#${i} periods`);
      assert.equal(r.monthsSaved, 0, `#${i} months saved`);
      assert.ok(c.periods === null || c.periods === n || c.periods === n + 1, `#${i} excel periods ${c.periods}`);
    } else {
      if (r.periods !== c.periods) bad.push(`#${i} periods: js ${r.periods}, excel ${c.periods}`);
      // The sheet's months saved isn't rounded; the page rounds it (the
      // second fix at the top of mortgage.js).
      if (r.monthsSaved !== Math.round(c.monthsSaved)) bad.push(`#${i} months saved: js ${r.monthsSaved}, excel ${c.monthsSaved}`);
    }
  });

  assert.deepEqual(bad.slice(0, 20), [], `${bad.length} mismatches`);
  assert.ok(figures > 900, `only ${figures} figures compared`);
});

test("mortgage-free date follows the sheet's EDATE and day counts", () => {
  const ymd = (d) => [d.getFullYear(), d.getMonth() + 1, d.getDate()];
  // Monthly: EDATE keeps the day, or the month's last day.
  assert.deepEqual(ymd(payoffDate(1, 12, new Date(2027, 0, 31))), [2027, 2, 28]);
  assert.deepEqual(ymd(payoffDate(1, 12, new Date(2028, 0, 31))), [2028, 2, 29]);
  assert.deepEqual(ymd(payoffDate(360, 12, new Date(2026, 10, 19))), [2056, 11, 19]);
  // Weekly and fortnightly: 7 or 14 days a repayment.
  assert.deepEqual(ymd(payoffDate(2, 52, new Date(2026, 11, 25))), [2027, 1, 8]);
  assert.deepEqual(ymd(payoffDate(620, 26, new Date(2026, 10, 19))), [2050, 8, 25]);
});

test("time saved reads in whole years and months", () => {
  assert.equal(timeText(0), "–");
  assert.equal(timeText(74), "6 years 2 months");
  assert.equal(timeText(12), "1 year");
  assert.equal(timeText(1), "1 month");
});

test("with no extra repayment, the loan is paid off on schedule", () => {
  // Every loan from $1,000 to $100 million, 0% to 15%, 1 to 50 years. Above
  // 15%, floating point can leave more than half a cent owing after the last
  // repayment (up to $1.41 on $100 million at 30% over 50 years weekly), so
  // the date moves one repayment later, or shows none when that would be past
  // 2,600 repayments.
  for (const loan of [1000, 100000, 650000, 2e6, 1e7, 1e8]) {
    for (let bp = 0; bp <= 1500; bp += 25) {
      for (const term of [1, 5, 10, 25, 30, 40, 50]) {
        for (const ppy of [52, 26, 12]) {
          const r = calc({ loan, rate: bp / 10000, term, ppy, extra: 0 });
          assert.equal(r.periods, Math.round(term * ppy), `${loan} at ${bp / 100}% for ${term} years, ${ppy} a year`);
          assert.ok(Number.isFinite(r.regular) && Number.isFinite(r.baseInterest));
        }
      }
    }
  }
});
