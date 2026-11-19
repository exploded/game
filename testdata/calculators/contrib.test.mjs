// /tools/co-contribution and /tools/spouse-contribution
// (static/js/contrib-calc.js) against the 2026-27 rules and the ATO's worked
// examples. Co-contribution: $500 less 3.333 cents a dollar over $49,293,
// nil from $64,293, 50 cents a dollar contributed, rounded up to 5 cents, at
// least $20. Spouse offset: 18% of the lesser of the contribution and $3,000
// less the spouse's income over $37,000.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { LAW, coContribution, spouseOffset } = require("../../static/js/contrib-calc.js");

const co = (income, contributed, eligible) => coContribution({ income, contributed, eligible });
const sp = (spouseIncome, contributed, eligible) => spouseOffset({ spouseIncome, contributed, eligible });

test("the law is 2026-27's", () => {
  assert.equal(LAW.year, "2026–27");
  assert.equal(LAW.co.lower, 49293);
  assert.equal(LAW.co.higher, LAW.co.lower + 15000);
  assert.equal(LAW.spouse.cap * LAW.spouse.rate, 540);
});

test("the full $500 up to the lower threshold, for $1,000 in", () => {
  for (const income of [0, 20000, 45000, 49293]) {
    const r = co(income, 1000);
    assert.equal(r.amount, 500, `income ${income}`);
    assert.equal(r.maxForIncome, 500);
    assert.equal(r.bestContribution, 1000);
    assert.equal(r.reason, "");
  }
});

test("50 cents a dollar below the most", () => {
  assert.equal(co(45000, 100).amount, 50);
  assert.equal(co(45000, 999).amount, 499.5);
  assert.equal(co(55000, 600).amount, 300);
  assert.equal(co(45000, 5000).amount, 500, "capped at $500");
});

test("the taper is 3.333 cents a dollar, rounded up to 5 cents", () => {
  // $1 over: $499.99667 rounds up to $500.
  assert.equal(co(49294, 1000).maxForIncome, 500);
  // $7 over: $499.76669 rounds up to $499.80.
  assert.equal(co(49300, 1000).maxForIncome, 499.8);
  // $5,707 over: $309.78569 rounds up to $309.80 (Superguide's 2026-27 example).
  const r = co(55000, 1000);
  assert.equal(r.amount, 309.8);
  assert.equal(r.maxForIncome, 309.8);
  assert.equal(r.bestContribution, 620, "$619.58 rounded up to a dollar");
  assert.equal(co(55000, 620).amount, 309.8);
  // Half of $619 is $309.50, under the most.
  assert.equal(co(55000, 619).amount, 309.5);
});

test("at least $20, and nothing from the higher threshold", () => {
  assert.equal(co(45000, 30).amount, 20, "$15 is paid as $20");
  assert.equal(co(45000, 1).amount, 20, "50 cents is paid as $20");
  // $1 under the higher threshold the most is 8.333 cents, paid as $20.
  const near = co(64292, 1000);
  assert.equal(near.amount, 20);
  assert.equal(near.maxForIncome, 20);
  assert.equal(near.bestContribution, 1);
  for (const income of [64293, 64294, 100000]) {
    const r = co(income, 1000);
    assert.equal(r.amount, 0, `income ${income}`);
    assert.equal(r.maxForIncome, 0);
    assert.equal(r.bestContribution, 0);
    assert.equal(r.reason, "income");
  }
});

test("nothing put in, or a condition not met, pays nothing", () => {
  const none = co(45000, 0);
  assert.equal(none.amount, 0);
  assert.equal(none.reason, "none");
  assert.equal(none.maxForIncome, 500, "still shows the most at this income");
  const no = co(45000, 1000, false);
  assert.equal(no.amount, 0);
  assert.equal(no.reason, "ineligible");
});

test("the spouse offset is 18% of up to $3,000", () => {
  const full = sp(30000, 3000);
  assert.equal(full.offset, 540);
  assert.equal(full.counted, 3000);
  assert.equal(full.cost, 2460);
  assert.equal(full.bestContribution, 3000);
  // The ATO's example: $2,000 for a spouse under $37,000 gives $360.
  assert.equal(sp(20000, 2000).offset, 360);
  assert.equal(sp(37000, 3000).offset, 540);
  // Over $3,000 adds nothing.
  const over = sp(30000, 5000);
  assert.equal(over.offset, 540);
  assert.equal(over.counted, 3000);
  assert.equal(over.cost, 4460);
});

test("the $3,000 falls a dollar for each dollar over $37,000", () => {
  // $38,500: $3,000 - $1,500 = $1,500 counts, so $270.
  const r = sp(38500, 3000);
  assert.equal(r.bestContribution, 1500);
  assert.equal(r.offset, 270);
  assert.equal(sp(38500, 1000).offset, 180, "less than the limit counts in full");
  assert.equal(sp(39999, 3000).offset, 0.18);
  for (const income of [40000, 40001, 80000]) {
    const n = sp(income, 3000);
    assert.equal(n.offset, 0, `income ${income}`);
    assert.equal(n.bestContribution, 0);
    assert.equal(n.reason, "income");
    assert.equal(n.cost, 3000);
  }
});

test("no spouse contribution, or a condition not met, gives no offset", () => {
  assert.equal(sp(30000, 0).reason, "none");
  const no = sp(30000, 3000, false);
  assert.equal(no.offset, 0);
  assert.equal(no.reason, "ineligible");
  assert.equal(no.cost, 3000);
});
