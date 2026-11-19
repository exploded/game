// Writes the inputs for the mortgage and super spreadsheet checks:
// mortgage-inputs.json and super-inputs.json, next to this file. Edge cases
// first, then seeded random plans, so a rerun gives the same cases.
//
// Then excel-capture.ps1 runs each through Rob's spreadsheet in Excel and
// writes ../mortgage-excel.json and ../super-excel.json, which
// mortgage.test.mjs and super.test.mjs compare against. See excel-capture.ps1.
import { writeFileSync } from "node:fs";

let seed = 20261119;
const rnd = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648;
const pick = (a) => a[Math.floor(rnd() * a.length)];

// Mortgage Freedom Calculator: loan, rate (fraction), term (years),
// freq (the sheet's words), extra (dollars each repayment).
const mortgage = [
  { loan: 650000, rate: 0.06, term: 30, freq: "Fortnightly", extra: 175 }, // the sheet's example
  { loan: 650000, rate: 0.06, term: 30, freq: "Fortnightly", extra: 0 },
  { loan: 650000, rate: 0.06, term: 30, freq: "Monthly", extra: 0 },
  { loan: 650000, rate: 0.06, term: 30, freq: "Weekly", extra: 0 },
  { loan: 400000, rate: 0, term: 25, freq: "Monthly", extra: 0 },
  { loan: 400000, rate: 0, term: 25, freq: "Monthly", extra: 500 },
  { loan: 400000, rate: 0, term: 25, freq: "Weekly", extra: 100 },
  { loan: 1000, rate: 0.05, term: 1, freq: "Monthly", extra: 0 },
  { loan: 1000, rate: 0.05, term: 1, freq: "Weekly", extra: 2000 },
  { loan: 100000000, rate: 0.3, term: 50, freq: "Weekly", extra: 0 },
  { loan: 100000000, rate: 0.3, term: 50, freq: "Weekly", extra: 999999 },
  { loan: 500000, rate: 0.065, term: 50, freq: "Weekly", extra: 50 },
  { loan: 500000, rate: 0.065, term: 50, freq: "Monthly", extra: 0 },
  { loan: 300000, rate: 0.055, term: 30, freq: "Monthly", extra: 300000 },
  { loan: 750000, rate: 0.0625, term: 30, freq: "Monthly", extra: 1000 },
  { loan: 250000, rate: 0.12, term: 10, freq: "Fortnightly", extra: 25 },
];
const loans = [1000, 50000, 200000, 350000, 500000, 650000, 800000, 1200000, 2500000];
const rates = [0, 0.02, 0.035, 0.0499, 0.055, 0.06, 0.0675, 0.08, 0.1, 0.15];
const extras = [0, 0, 25, 50, 100, 175, 250, 500, 1000, 5000];
while (mortgage.length < 60) {
  mortgage.push({
    loan: rnd() < 0.6 ? pick(loans) : 1000 + Math.round(rnd() * 3000000),
    rate: rnd() < 0.7 ? pick(rates) : Math.round(rnd() * 3000) / 10000,
    term: 1 + Math.floor(rnd() * 50),
    freq: pick(["Weekly", "Fortnightly", "Monthly"]),
    extra: rnd() < 0.8 ? pick(extras) : Math.round(rnd() * 3000),
  });
}

// Start Early: Extra Super Calculator: weekly (dollars), ret, ctax,
// income (part of the return), itax, inflation (all fractions). The page
// calls inflation "wage", and defaults it to 3.7% where the sheet used 2.6%.
const sup = [
  { weekly: 20, ret: 0.09, ctax: 0.15, income: 0.03, itax: 0.15, inflation: 0.026 }, // the sheet's example
  { weekly: 20, ret: 0.09, ctax: 0.15, income: 0.03, itax: 0.15, inflation: 0.037 },
  { weekly: 0, ret: 0.09, ctax: 0.15, income: 0.03, itax: 0.15, inflation: 0.026 },
  { weekly: 50, ret: 0, ctax: 0.15, income: 0, itax: 0.15, inflation: 0.026 },
  { weekly: 50, ret: 0.0045, ctax: 0.15, income: 0.03, itax: 0.15, inflation: 0.026 }, // return after tax is exactly 0
  { weekly: 2000, ret: 0.2, ctax: 0, income: 0, itax: 0, inflation: 0 },
  { weekly: 2000, ret: 0.2, ctax: 0.5, income: 0.2, itax: 0.5, inflation: 0.1 },
  { weekly: 1, ret: 0.07, ctax: 0.15, income: 0.07, itax: 0.15, inflation: 0.025 },
];
while (sup.length < 40) {
  const ret = pick([0, 0.03, 0.05, 0.06, 0.07, 0.08, 0.09, 0.1, 0.12, 0.2]);
  sup.push({
    weekly: rnd() < 0.7 ? pick([5, 10, 20, 25, 50, 100, 250, 500]) : Math.round(rnd() * 200000) / 100,
    ret,
    ctax: pick([0, 0.15, 0.15, 0.3, 0.47]),
    income: Math.round(rnd() * ret * 1000) / 1000,
    itax: pick([0, 0.15, 0.15, 0.3, 0.5]),
    inflation: pick([0, 0.025, 0.026, 0.037, 0.05]),
  });
}

const here = (name) => new URL(name, import.meta.url);
writeFileSync(here("mortgage-inputs.json"), JSON.stringify(mortgage, null, 1) + "\n");
writeFileSync(here("super-inputs.json"), JSON.stringify(sup, null, 1) + "\n");
console.log(`${mortgage.length} mortgage cases, ${sup.length} super cases`);
