// /tools/fhss (static/js/fhss.js) against Rob's spreadsheet,
// CPW_FHSS_Calculator_v2.xlsx.
//
// fhss-scenarios.json holds 90 plans (limits broken, every tax bracket, 1 to 6
// years, other earnings and bank rates). fhss-excel.json holds what Excel
// worked out for each, recalculated over COM on 19 November 2026. The
// spreadsheet itself isn't in git; the scripts that made these files are in
// .local/calculators/fhss-check (gen.js, excel.ps1). Rerun them when Rob sends
// a new version, then copy the two JSON files here.
//
// "Most you can add" and the contribution check are compared only where every
// earlier year was fully counted: the page counts earlier years as counted,
// where the sheet uses the amounts typed (see the top of fhss.js). The sheet's
// data validation stops an over-limit amount being typed, so this covers every
// plan Excel accepts.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { calc } = require("../../static/js/fhss.js");

const load = (name) =>
  JSON.parse(readFileSync(new URL(name, import.meta.url), "utf8").replace(/^﻿/, ""));
const scenarios = load("fhss-scenarios.json");
const excel = load("fhss-excel.json");

// The sheet's check column, as fhss.js names it.
const code = (t) =>
  t.startsWith("✓") ? "ok"
    : t.includes("not counted") ? "after-entered"
      : t.startsWith("After") ? "after"
        : t.includes("32,500") ? "cap"
          : t.includes("15,000") ? "yearly"
            : t.includes("50,000") ? "total"
              : "?" + t;

test("every FHSS figure matches Excel", () => {
  assert.equal(scenarios.length, 90);
  assert.equal(excel.length, scenarios.length);

  let figures = 0, worst = 0;
  const bad = [];
  const near = (js, xl, what, i) => {
    figures++;
    const d = Math.abs(js - xl);
    if (!(d <= 1e-6)) bad.push(`#${i} ${what}: js ${js}, excel ${xl}`);
    else worst = Math.max(worst, d);
  };

  scenarios.forEach((s, i) => {
    const r = calc(
      { salary: s.salary, sgRate: s.sg, payRise: s.rise, years: s.years, already: s.already, contrib: s.c },
      { earnRate: s.earn, bankRate: s.bank },
    );
    const x = excel[i];
    for (const k of ["deposit", "bank", "extra", "counted", "earnings", "tax", "buySalary", "buyRate"]) {
      near(r[k], x[k], k, i);
    }
    if (r.allOK !== x.banner.startsWith("✓")) bad.push(`#${i} banner: js ${r.allOK}, excel ${x.banner}`);

    let valid = true; // the sheet's data validation would allow every earlier year
    r.rows.forEach((row, j) => {
      const xr = x.rows[j];
      for (const k of ["left", "counted", "earnings", "rate", "bank"]) near(row[k], xr[k], `y${j + 1} ${k}`, i);
      if (valid) {
        near(row.most, xr.most, `y${j + 1} most`, i);
        if (row.check !== code(xr.check)) bad.push(`#${i} y${j + 1} check: js ${row.check}, excel ${xr.check}`);
      }
      if (row.check === "total" && row.counted === row.entered) {
        bad.push(`#${i} y${j + 1} flagged total but fully counted`);
      }
      if (!row.after && row.counted !== row.entered) valid = false;
    });
  });

  assert.deepEqual(bad.slice(0, 20), [], `${bad.length} mismatches`);
  assert.equal(figures, 3661, "figures compared");
  assert.ok(worst < 1e-6, `largest gap ${worst}`);
});
