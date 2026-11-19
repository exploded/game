// /tools/compound (static/js/compound.js) against closed-form answers.
// The page follows the assumptions Moneysmart states (the top of
// compound.js): deposits at the end of each period, interest accruing daily
// and credited monthly or yearly.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { simulate, calc } = require("../../static/js/compound.js");

const close = (actual, expected, msg) =>
  assert.ok(Math.abs(actual - expected) <= 1e-6 * Math.max(1, Math.abs(expected)), `${msg}: expected ${expected}, got ${actual}`);
const end = (pts) => pts[pts.length - 1];
const plan = (o) => Object.assign({ initial: 0, deposit: 0, perYear: 12, rate: 0.05, credits: 12, years: 10, delay: 0 }, o);

test("a lump sum compounds at the crediting frequency", () => {
  close(end(simulate(10000, 0, 12, 0.05, 12, 10)).balance, 10000 * Math.pow(1 + 0.05 / 12, 120), "monthly");
  close(end(simulate(10000, 0, 12, 0.05, 1, 10)).balance, 10000 * Math.pow(1.05, 10), "yearly");
  close(end(simulate(10000, 0, 12, 0.3, 12, 60)).balance, 10000 * Math.pow(1 + 0.3 / 12, 720), "30% for 60 years");
});

test("regular deposits are an ordinary annuity", () => {
  const i = 0.06 / 12, n = 240;
  close(end(simulate(0, 500, 12, 0.06, 12, 20)).balance, 500 * (Math.pow(1 + i, n) - 1) / i, "monthly deposits, monthly credits");
  close(end(simulate(0, 6000, 1, 0.06, 1, 20)).balance, 6000 * (Math.pow(1.06, 20) - 1) / 0.06, "yearly deposits, yearly credits");
  // Both together: the lump sum and the deposits add up.
  const both = end(simulate(10000, 500, 12, 0.06, 12, 20)).balance;
  close(both, 10000 * Math.pow(1 + i, n) + 500 * (Math.pow(1 + i, n) - 1) / i, "lump sum plus deposits");
});

test("the interest splits into interest on deposits and interest on interest", () => {
  for (const [perYear, credits] of [[365, 12], [52, 12], [26, 1], [12, 12], [1, 1]]) {
    const e = end(simulate(2500, 100, perYear, 0.07, credits, 15));
    close(e.balance - e.deposits, e.onDeposits + e.onInterest, `split ${perYear}/${credits}`);
    close(e.deposits, 2500 + 100 * perYear * 15, `deposits ${perYear}`);
    assert.ok(e.onInterest > 0 && e.onDeposits > 0);
  }
  // In the first year with yearly crediting, nothing earns interest on
  // interest: the lump sum earns simple interest.
  const y1 = end(simulate(10000, 0, 1, 0.05, 1, 1));
  close(y1.onDeposits, 500, "first year's interest");
  assert.equal(y1.onInterest, 0);
});

test("0% interest just adds up the deposits", () => {
  for (const perYear of [365, 52, 26, 12, 1]) {
    const e = calc(plan({ initial: 1000, deposit: 50, perYear, rate: 0, years: 7 })).end;
    close(e.balance, 1000 + 50 * perYear * 7, `perYear ${perYear}`);
    assert.equal(e.onDeposits, 0);
    assert.equal(e.onInterest, 0);
  }
});

test("0 years leaves the first deposit as it is", () => {
  const r = calc(plan({ initial: 1000, deposit: 50, years: 0 }));
  assert.equal(r.now.length, 1);
  assert.deepEqual(r.end, { t: 0, balance: 1000, deposits: 1000, onDeposits: 0, onInterest: 0 });
  assert.equal(r.later, null);
});

test("a later start makes the same deposits for fewer years and ends on the same date", () => {
  const r = calc(plan({ initial: 1000, deposit: 200, years: 30, delay: 10 }));
  assert.equal(r.later.length, r.now.length);
  close(r.laterEnd.balance, end(simulate(1000, 200, 12, 0.05, 12, 20)).balance, "later balance");
  assert.equal(r.later[0].balance, 0);
  assert.ok(r.laterEnd.balance < r.end.balance);
  // A delay as long as the plan is left out, as the page's check refuses it.
  assert.equal(calc(plan({ initial: 1000, years: 5, delay: 5 })).later, null);
});

test("every figure is finite at the edges the page allows", () => {
  for (const o of [
    { initial: 0, deposit: 1, rate: 0, years: 1 },
    { initial: 100e6, deposit: 10e6, perYear: 365, rate: 0.3, years: 60, delay: 59 },
    { initial: 1, deposit: 0, perYear: 1, credits: 1, rate: 0.3, years: 60 },
  ]) {
    const r = calc(plan(o));
    for (const pt of [...r.now, ...(r.later || [])]) {
      for (const k of ["balance", "deposits", "onDeposits", "onInterest"]) assert.ok(Number.isFinite(pt[k]), `${k} in ${JSON.stringify(o)}`);
    }
  }
});
