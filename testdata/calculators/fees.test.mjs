// /tools/fees (static/js/fees.js) against closed-form answers. Everything
// moves daily (365 days a year): the balance grows by the daily share of the
// yearly return, then the ongoing and dollar fees come out pro rata;
// contributions arrive at the end of each period, less the contribution fee.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { simulate, calc } = require("../../static/js/fees.js");

const close = (actual, expected, msg) =>
  assert.ok(Math.abs(actual - expected) <= 1e-6 * Math.max(1, Math.abs(expected)), `${msg}: expected ${expected}, got ${actual}`);
const end = (pts) => pts[pts.length - 1];
const NONE = { ongoing: 0, entry: 0, fixed: 0 };
const plan = (o) => Object.assign({ initial: 0, contribution: 0, perYear: 12, years: 10, ret: 0.07 }, o);

test("with no fees a lump sum grows at the yearly return", () => {
  close(end(simulate(plan({ initial: 10000 }), NONE)).balance, 10000 * Math.pow(1.07, 10), "7% for 10 years");
  close(end(simulate(plan({ initial: 10000, ret: 0.2, years: 60 }), NONE)).balance, 10000 * Math.pow(1.2, 60), "20% for 60 years");
});

test("yearly contributions with no fees are an ordinary annuity", () => {
  const e = end(simulate(plan({ initial: 5000, contribution: 1000, perYear: 1, years: 25 }), NONE));
  close(e.balance, 5000 * Math.pow(1.07, 25) + 1000 * (Math.pow(1.07, 25) - 1) / 0.07, "balance");
  close(e.contributed, 5000 + 25000, "contributed");
  assert.equal(e.paid, 0);
});

test("an ongoing fee drags the growth every day", () => {
  const f = 0.015, years = 20;
  const daily = Math.pow(1.07, 1 / 365) * (1 - f / 365);
  const e = end(simulate(plan({ initial: 100000, years }), { ongoing: f, entry: 0, fixed: 0 }));
  close(e.balance, 100000 * Math.pow(daily, 365 * years), "balance");
  // Fee drag: about 1.5 points a year off the return.
  const effective = Math.pow(e.balance / 100000, 1 / years) - 1;
  assert.ok(Math.abs(effective - (1.07 * Math.pow(1 - f / 365, 365) - 1)) < 1e-9, `effective return ${effective}`);
});

test("at 0% return the fees are simple sums", () => {
  // Contribution fee: a share of each contribution, not of the starting amount.
  const a = end(simulate(plan({ initial: 10000, contribution: 100, perYear: 26, years: 5, ret: 0 }), { ongoing: 0, entry: 0.04, fixed: 0 }));
  close(a.balance, 10000 + 100 * 0.96 * 26 * 5, "balance after contribution fees");
  close(a.paid, 100 * 0.04 * 26 * 5, "contribution fees");
  // A yearly dollar fee comes out pro rata.
  const b = end(simulate(plan({ initial: 10000, years: 4, ret: 0 }), { ongoing: 0, entry: 0, fixed: 250 }));
  close(b.balance, 10000 - 1000, "balance after dollar fees");
  close(b.paid, 1000, "dollar fees");
  // A dollar fee can't take the balance below zero.
  const c = end(simulate(plan({ initial: 100, years: 2, ret: 0 }), { ongoing: 0, entry: 0, fixed: 100000 }));
  assert.equal(c.balance, 0);
  close(c.paid, 100, "fees capped at the balance");
});

test("the effect of fees is the gap to no fees at all", () => {
  const p = plan({ initial: 50000, contribution: 500, perYear: 12, years: 30 });
  const funds = [{ ongoing: 0.01, entry: 0.02, fixed: 100 }, NONE];
  const r = calc(p, funds);
  close(r.funds[0].effect, r.noneEnd.balance - r.funds[0].end.balance, "effect");
  assert.ok(r.funds[0].effect > r.funds[0].end.paid, "lost returns count too");
  assert.equal(r.funds[1].effect, 0);
});

test("0 years leaves the starting amount as it is", () => {
  const r = calc(plan({ initial: 10000, contribution: 100, years: 0 }), [{ ongoing: 0.01, entry: 0.01, fixed: 50 }]);
  assert.equal(r.none.length, 1);
  assert.deepEqual(r.noneEnd, { t: 0, balance: 10000, paid: 0, contributed: 10000 });
  assert.equal(r.funds[0].effect, 0);
});

test("every figure is finite at the edges the page allows", () => {
  for (const [o, f] of [
    [{ initial: 0, contribution: 1, perYear: 52, years: 1, ret: 0 }, { ongoing: 0.1, entry: 0.1, fixed: 100000 }],
    [{ initial: 100e6, contribution: 10e6, perYear: 52, years: 60, ret: 0.2 }, NONE],
    [{ initial: 1, contribution: 0, perYear: 1, years: 60, ret: 0.2 }, { ongoing: 0.1, entry: 0, fixed: 0 }],
  ]) {
    const r = calc(plan(o), [f]);
    for (const pt of [...r.none, ...r.funds[0].points]) {
      for (const k of ["balance", "paid", "contributed"]) assert.ok(Number.isFinite(pt[k]), `${k} in ${JSON.stringify(o)}`);
    }
  }
});
