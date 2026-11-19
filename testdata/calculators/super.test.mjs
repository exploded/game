// /tools/super (static/js/super.js) against Rob's spreadsheet,
// "Super Starting Early Calculator Aug 2026".
//
// super-excel.json holds 40 cases (excel/gen-cases.mjs) and the sheet's
// "Calculations" figures for each, ages 20 to 60, recalculated over COM on
// 19 November 2026 (excel/excel-capture.ps1, which says how to rerun it). The
// spreadsheet itself isn't in git. The page's "wage" assumption is the sheet's
// inflation (the page defaults it to ASIC's 3.7%, the sheet used 2.6%).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { calc, DEFAULTS } = require("../../static/js/super.js");

const cases = JSON.parse(readFileSync(new URL("super-excel.json", import.meta.url), "utf8"));
const HALF_CENT = 0.005;
const assumptions = (o) => ({ contribTax: o.ctax, incomePart: o.income, incomeTax: o.itax, wage: o.inflation });

test("every super figure matches Excel to the cent", () => {
  assert.equal(cases.length, 40);
  let figures = 0;
  const bad = [];
  cases.forEach((c, i) => {
    const o = c.input;
    const r = calc(o.weekly, o.ret, assumptions(o));
    r.forEach((s, j) => {
      assert.equal(s.path.length, 41);
      s.path.forEach((v, a) => {
        figures++;
        if (!(Math.abs(v - c.paths[j][a]) < HALF_CENT)) bad.push(`#${i} start ${s.start} age ${20 + a}: js ${v}, excel ${c.paths[j][a]}`);
      });
      figures += 2;
      if (!(Math.abs(s.value - c.paths[j][40]) < HALF_CENT)) bad.push(`#${i} start ${s.start} value`);
      if (!(Math.abs(s.real - c.reals[j][40]) < HALF_CENT)) bad.push(`#${i} start ${s.start} today's dollars: js ${s.real}, excel ${c.reals[j][40]}`);
    });
  });
  assert.deepEqual(bad.slice(0, 20), [], `${bad.length} mismatches`);
  assert.equal(figures, 40 * 3 * 43);
});

test("the sheet's own example: $20 a week at 9%", () => {
  // As the spreadsheet shows them: $231,049, $101,720 and $44,782 at 60, and
  // $82,758, $36,434 and $16,040 in today's dollars at its 2.6%.
  const r = calc(20, 0.09, { wage: 0.026 });
  assert.deepEqual(r.map((s) => Math.round(s.value)), [231049, 101720, 44782]);
  assert.deepEqual(r.map((s) => Math.round(s.real)), [82758, 36434, 16040]);
  assert.equal(DEFAULTS.wage, 0.037);
});

test("a return that tax cancels out just adds up the contributions", () => {
  // 0.45% less 3% x 15% is exactly 0 after tax.
  const r = calc(50, 0.0045, { wage: 0 });
  for (const s of r) assert.ok(Math.abs(s.value - 50 * 0.85 * 52 * 20) < 1e-6, `start ${s.start}: ${s.value}`);
  assert.deepEqual(calc(0, 0).map((s) => s.value), [0, 0, 0]);
});

test("every figure is finite at the edges the page allows", () => {
  for (const [weekly, ret, a] of [
    [0, 0, { incomePart: 0 }],
    [2000, 0.2, { contribTax: 0, incomePart: 0, incomeTax: 0, wage: 0 }],
    [2000, 0.2, { contribTax: 0.5, incomePart: 0.2, incomeTax: 0.5, wage: 0.1 }],
  ]) {
    for (const s of calc(weekly, ret, a)) {
      assert.ok(s.path.every(Number.isFinite) && Number.isFinite(s.real), `${weekly} ${ret} ${JSON.stringify(a)}`);
    }
  }
});
