// Tests for static/js/money-lasted-calc.js (/tools/money-lasted), copied on 19 November
// 2026 from James's prototype in C:\Projects\go\investor-tools (tools/money-lasted/calc.test.js),
// with the imports pointed at the site's copy. Keep both in step.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  shareReturns,
  bondPrice,
  bondReturn,
  bondRealReturns,
  blendReturns,
  runPeriod,
  simulate,
  indexOfMonth,
  yearsAndMonths,
} from "../../static/js/money-lasted-calc.js";

const close = (actual, expected, tol, msg = "") =>
  assert.ok(Math.abs(actual - expected) <= tol, `${msg} expected ${expected}, got ${actual}`);
const constant = (n, r) => new Float64Array(n).fill(r);

// --- Returns ---------------------------------------------------------------

test("share returns come from the total return index", () => {
  const r = shareReturns([100, 110, 99]);
  assert.equal(r.length, 2);
  close(r[0], 0.1, 1e-12);
  close(r[1], -0.1, 1e-12);
});

test("a par bond is worth its face value", () => {
  close(bondPrice(0.05, 0.05, 10), 1, 1e-12);
  close(bondPrice(0.03, 0.03, 10, 1), 1, 1e-12);
  // A rise in yields lowers the price; a fall raises it.
  assert.ok(bondPrice(0.05, 0.06, 10) < 1);
  assert.ok(bondPrice(0.05, 0.04, 10) > 1);
});

test("unchanged yields earn a month of coupon", () => {
  close(bondReturn(6, 6), 0.06 / 12, 1e-12);
});

test("bond return matches Shiller's own figure for January 1871", () => {
  // Shiller's "Monthly Total Bond Returns" for 1871-01 is 1.0041769357.
  close(1 + bondReturn(5.32, 5.323333333333333), 1.0041769357, 2e-5);
});

test("real bond returns take off inflation", () => {
  // Flat yields at 6%, prices up 1% in the month.
  const r = bondRealReturns([6, 6], [100, 101]);
  close(r[0], 1.005 / 1.01 - 1, 1e-12);
});

test("blending rebalances to the split every month", () => {
  const r = blendReturns(new Float64Array([0.1, -0.2]), new Float64Array([0.0, 0.02]), 0.6);
  close(r[0], 0.06, 1e-12);
  close(r[1], -0.112, 1e-12);
});

// --- One period ------------------------------------------------------------

test("constant return matches the closed form", () => {
  // Spending at the start of each month: B_n = B0(1+r)^n − s(1+r)((1+r)^n − 1)/r
  const r = 0.004;
  const n = 360;
  const plan = { balance: 1_000_000, spending: 48_000, fees: 0 };
  const s = plan.spending / 12;
  const g = (1 + r) ** n;
  const expected = plan.balance * g - (s * (1 + r) * (g - 1)) / r;
  const out = runPeriod(constant(n, r), 0, n, plan);
  assert.equal(out.lasted, true);
  close(out.end, expected, 1e-6);
});

test("zero return runs out exactly when the money does", () => {
  // $120k at $12k a year lasts exactly 120 months.
  const plan = { balance: 120_000, spending: 12_000, fees: 0 };
  assert.equal(runPeriod(constant(120, 0), 0, 120, plan).lasted, true);
  const short = runPeriod(constant(121, 0), 0, 121, plan, true);
  assert.equal(short.lasted, false);
  assert.equal(short.monthsLasted, 120);
  assert.equal(short.end, 0);
  assert.equal(short.path[121], 0);
});

test("fees come off every month", () => {
  const plan = { balance: 100_000, spending: 0, fees: 0.012 };
  const out = runPeriod(constant(12, 0), 0, 12, plan);
  close(out.end, 100_000 * 0.999 ** 12, 1e-6);
});

test("sequence risk: a crash in year 1 hurts more than the same crash in year 25", () => {
  const n = 360;
  const early = constant(n, 0.005);
  const late = constant(n, 0.005);
  early[0] = -0.4; // crash in the first month
  late[300] = -0.4; // the same crash in year 26
  const plan = { balance: 1_000_000, spending: 50_000, fees: 0 };

  // With no spending, the order doesn't matter.
  const hold = { ...plan, spending: 0 };
  close(runPeriod(early, 0, n, hold).end, runPeriod(late, 0, n, hold).end, 1e-6);

  // With spending, the early crash ends far lower.
  const e = runPeriod(early, 0, n, plan);
  const l = runPeriod(late, 0, n, plan);
  assert.equal(l.lasted, true);
  assert.equal(e.lasted, false);
  assert.ok(e.monthsLasted < n);
});

// --- Every period ----------------------------------------------------------

test("simulate tests every complete start month", () => {
  const returns = constant(100, 0);
  const out = simulate(returns, { balance: 50, spending: 12, fees: 0, years: 5 });
  assert.equal(out.months, 60);
  assert.equal(out.tested, 41); // starts 0..40 have 60 months of returns
  assert.equal(out.succeeded, 0); // $50 at $1 a month lasts 50 months
  assert.equal(out.worst.monthsLasted, 50);
});

test("simulate finds the worst and median periods", () => {
  // Start 0 gets a crash first; later starts don't see it.
  const returns = constant(30, 0);
  returns[0] = -0.5;
  const out = simulate(returns, { balance: 100, spending: 12 * 6, fees: 0, years: 1 });
  assert.equal(out.tested, 19);
  assert.equal(out.worst.start, 0);
  assert.equal(out.worst.lasted, false);
  assert.equal(out.succeeded, 18);
  assert.equal(out.median.end, 28); // 100 − 12 × 6
  assert.equal(out.median.min, 28);
});

test("no complete period means nothing tested", () => {
  const out = simulate(constant(10, 0), { balance: 1, spending: 0, fees: 0, years: 1 });
  assert.equal(out.tested, 0);
  assert.equal(out.median, null);
});

test("date helpers", () => {
  assert.equal(indexOfMonth("1871-01", "1871-01"), 0);
  assert.equal(indexOfMonth("1871-01", "1929-01"), 696);
  assert.equal(indexOfMonth("1871-01", "1870-12"), -1);
  assert.deepEqual(yearsAndMonths(327), { years: 27, months: 3 });
});

// --- Real data -------------------------------------------------------------

const data = JSON.parse(readFileSync(new URL("../../static/data/shiller.json", import.meta.url), "utf8"));

test("Shiller data is monthly, complete and starts in January 1871", () => {
  assert.equal(data.first, "1871-01");
  assert.equal(data.date.length, data.months);
  for (const key of ["realTotalReturn", "cpi", "gs10", "realPrice", "price"]) {
    assert.equal(data[key].length, data.months, key);
    assert.ok(data[key].every((v) => Number.isFinite(v) && v > 0), key);
  }
});

test("sanity check: 4% rule, 30 years, 100% shares lasts in 90–100% of periods", () => {
  const returns = shareReturns(data.realTotalReturn);
  const out = simulate(returns, { balance: 1_000_000, spending: 40_000, fees: 0, years: 30 });
  assert.ok(out.tested > 1400, `tested ${out.tested}`);
  assert.ok(out.rate >= 0.9 && out.rate <= 1, `success rate ${out.rate}`);
});

test("real bond returns compound to a sensible long-run figure", () => {
  // US 10-year government bonds have returned roughly 1–4% a year after
  // inflation since 1871. data/SOURCE.md compares the monthly figures with
  // Shiller's own bond series.
  const r = bondRealReturns(data.gs10, data.cpi);
  const growth = r.reduce((g, x) => g * (1 + x), 1);
  const yearly = growth ** (12 / r.length) - 1;
  assert.ok(yearly > 0.01 && yearly < 0.04, `real bond return ${yearly}`);
});
