// Tests for static/js/stop-working-calc.js (/tools/stop-working), copied on 19 November
// 2026 from James's prototype in C:\Projects\go\investor-tools (tools/stop-working/calc.test.js),
// with the imports pointed at the site's copy. Keep both in step.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  yearsToStop,
  savingsCurve,
  netSuperContribution,
  project,
  checkStopAge,
  planWithSuper,
} from "../../static/js/stop-working-calc.js";

const close = (actual, expected, tol, msg) =>
  assert.ok(
    Math.abs(actual - expected) <= tol,
    `${msg ?? ""} expected ${expected}, got ${actual}`,
  );

// --- Simple mode -----------------------------------------------------------

test("reproduces the article's table (5% real return, 4% withdrawal rate)", () => {
  close(yearsToStop(0.1), 51.4, 0.05, "10%:");
  close(yearsToStop(0.5), 16.6, 0.05, "50%:");
  close(yearsToStop(0.75), 7.1, 0.05, "75%:");
});

test("closed form matches a year-by-year simulation", () => {
  for (const s of [0.05, 0.2, 0.37, 0.6, 0.9]) {
    const target = (1 - s) / 0.04;
    let balance = 0;
    let years = 0;
    while (balance < target) {
      balance = balance * 1.05 + s;
      years++;
    }
    const n = yearsToStop(s, 0.05, 0.04);
    assert.ok(n <= years && n > years - 1, `s=${s}: ${n} vs ${years}`);
  }
});

test("zero real return is a straight division", () => {
  // Target is 25 years of spending; saving 50% buys one year of spending a year.
  assert.equal(yearsToStop(0.5, 0, 0.04), 25);
});

test("edge cases", () => {
  assert.equal(yearsToStop(0), Infinity);
  assert.equal(yearsToStop(1), 0);
  assert.equal(yearsToStop(0.5, 0.05, 0), Infinity);
  // A negative return caps savings at s / |r|; here that's below the target.
  assert.equal(yearsToStop(0.1, -0.05, 0.04), Infinity);
  // ...but a high savings rate can still get there.
  assert.ok(Number.isFinite(yearsToStop(0.9, -0.01, 0.04)));
});

test("higher savings rates always mean fewer years", () => {
  const curve = savingsCurve([0.05, 0.1, 0.25, 0.5, 0.75, 0.9], 0.05, 0.04);
  for (let i = 1; i < curve.length; i++) {
    assert.ok(curve[i].years < curve[i - 1].years);
  }
});

// --- Australian mode -------------------------------------------------------

test("employer super is 12% of salary less 15% contributions tax", () => {
  close(netSuperContribution(100_000), 10_200, 1e-9);
  assert.equal(netSuperContribution(-5), 0);
});

test("with no super and no bridge it matches simple mode", () => {
  // Preservation age at the current age removes the bridge; no salary means
  // no employer super. Saving 50% at 5% takes 16.6 years, so 17 whole years.
  const plan = planWithSuper({
    age: 30,
    preservationAge: 30,
    takeHome: 100_000,
    spending: 50_000,
    outside: 0,
    superBalance: 0,
    salary: 0,
  });
  assert.equal(plan.stopAge, 47);
  assert.equal(plan.yearsFromNow, 17);
});

test("bridge years need savings outside super to cover every year to 60", () => {
  const base = {
    age: 50,
    takeHome: 40_000,
    spending: 40_000,
    superBalance: 1_000_000, // 40k ÷ 4% = 1m, already enough from 60
    salary: 0,
    realReturn: 0,
    superReturn: 0,
  };
  // Exactly 10 years of spending covers ages 50 to 59.
  assert.equal(planWithSuper({ ...base, outside: 400_000 }).stopAge, 50);

  // One dollar short at 50 runs out at 59, but a year later the bridge is
  // only 9 years.
  const short = checkStopAge({ ...base, outside: 399_999 }, 50);
  assert.equal(short.bridgeOk, false);
  assert.equal(short.bridgeShortAge, 59);
  const later = planWithSuper({ ...base, outside: 399_999 });
  assert.equal(later.stopAge, 51);
  assert.equal(later.check.bridgeYears, 9);

  // Spending more than you earn drains savings while you work: never.
  const never = planWithSuper({
    ...base,
    outside: 100_000,
    superBalance: 900_000,
    takeHome: 30_000,
  });
  assert.equal(never.stopAge, null);
  assert.equal(never.check.bridgeOk, true); // at 75 there's no bridge
  assert.equal(never.check.retiredOk, false);
});

test("from 60, super plus leftover savings must meet the withdrawal rate", () => {
  const base = {
    age: 55,
    takeHome: 50_000,
    spending: 40_000,
    outside: 200_000, // covers the 5-year bridge exactly
    salary: 0,
    realReturn: 0,
    superReturn: 0,
  };
  const short = checkStopAge({ ...base, superBalance: 999_999 }, 55);
  assert.equal(short.bridgeOk, true);
  assert.equal(short.retiredOk, false);
  assert.equal(short.needed, 1_000_000);

  const enough = checkStopAge({ ...base, superBalance: 1_000_000 }, 55);
  assert.equal(enough.ok, true);
  assert.equal(enough.startAge, 60);
});

test("projection steps through work, bridge and retirement", () => {
  const rows = project(
    {
      age: 58,
      takeHome: 60_000,
      spending: 40_000,
      outside: 100_000,
      superBalance: 500_000,
      salary: 100_000,
      realReturn: 0,
      superReturn: 0,
    },
    59,
    61,
  );
  assert.deepEqual(
    rows.map((r) => [r.age, r.phase, Math.round(r.outside), Math.round(r.super)]),
    [
      [58, "work", 100_000, 500_000],
      [59, "bridge", 120_000, 510_200], // saved 20k, plus 10.2k employer super
      [60, "retired", 80_000, 510_200], // one bridge year from outside
      [61, "retired", 40_000, 510_200], // outside used first from 60
    ],
  );
});

test("zero spending means you can stop now", () => {
  const plan = planWithSuper({
    age: 40,
    takeHome: 80_000,
    spending: 0,
    outside: 0,
    superBalance: 0,
    salary: 100_000,
  });
  assert.equal(plan.stopAge, 40);
});

test("the page's example figures give a plausible answer", () => {
  const plan = planWithSuper({
    age: 35,
    takeHome: 85_000,
    spending: 55_000,
    outside: 50_000,
    superBalance: 90_000,
    salary: 112_000,
  });
  assert.ok(plan.stopAge >= 45 && plan.stopAge <= 60, `got ${plan.stopAge}`);
});
