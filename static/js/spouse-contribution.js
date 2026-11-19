// Spouse contribution tax offset calculator (/tools/spouse-contribution). The
// sums and the 2026–27 rules are in contrib-calc.js, loaded first. The panel
// under the results uses the same co-contribution sums as
// /tools/co-contribution, on the spouse's income. Nothing typed is sent
// anywhere or saved.
(function () {
    "use strict";

    var C = window.ContribCalc;
    var root = document.querySelector("[data-kcalc-spouse]");
    if (!C || !root) return;

    var LAW = C.LAW, money = C.money;
    var FULL = LAW.spouse.cap * LAW.spouse.rate; // $540
    var out = function (name) { return root.querySelector('[data-out="' + name + '"]'); };
    var field = function (name) { return root.querySelector('[data-k="' + name + '"]'); };
    var chartEl = out("chart");
    var last = null;

    function check(p) {
        if (isNaN(p.spouseIncome)) return "Enter your spouse’s income for the year.";
        if (p.spouseIncome > 100e6) return "Enter an income of $100 million or less.";
        if (p.contributed > 10e6) return "Enter an amount of $10 million or less.";
        return "";
    }

    function setBanner(text, warn) {
        var b = out("banner");
        b.textContent = text;
        b.classList.toggle("kc-banner-warn", !!warn);
    }

    function update() {
        var p = { spouseIncome: C.num(field("income")), contributed: C.num(field("contributed")) };
        if (isNaN(p.contributed)) p.contributed = 0;
        var err = check(p);
        var errEl = out("error");
        root.classList.toggle("mcalc-invalid", !!err);
        errEl.hidden = !err;
        errEl.textContent = err;
        if (err) return;

        var unticked = C.firstUnticked(root);
        p.eligible = !unticked;
        var r = C.spouseOffset(p);

        out("offset").textContent = money(r.offset);
        out("offset-note").textContent = r.offset > 0
            ? "Comes off the tax you pay for " + LAW.year + " when you lodge your return. If your tax is less, you get only that much: it isn’t refunded."
            : "";
        out("best").textContent = money(r.bestContribution);
        out("best-note").textContent = r.bestContribution > 0 && r.bestContribution < LAW.spouse.cap
            ? "Less than $3,000, because their income is over " + money(LAW.spouse.base) + "."
            : "";
        out("cost").textContent = money(r.cost);
        out("cost-note").textContent = r.offset > 0
            ? "Each $1 in their super costs you about " + Math.round((1 - r.offset / p.contributed) * 100) + " cents."
            : "";

        if (r.reason === "ineligible") {
            setBanner("No tax offset: " + unticked.getAttribute("data-why"), true);
        } else if (r.reason === "income") {
            setBanner("Your spouse’s income is " + money(LAW.spouse.cut) + " or more, so there’s no tax offset.", true);
        } else if (r.reason === "none") {
            setBanner("Enter what you’ll put in. At their income, up to " + money(r.bestContribution) +
                " counts towards the offset.");
        } else if (p.contributed > r.bestContribution) {
            setBanner("Only the first " + money(r.bestContribution) + " counts towards the offset. The rest still goes into your spouse’s super.");
        } else if (r.offset >= FULL) {
            setBanner("You’d get the full " + money(FULL) + " offset.");
        } else if (r.counted < r.bestContribution) {
            setBanner("Put in " + money(r.bestContribution - p.contributed) + " more, " + money(r.bestContribution) +
                " in all, to get the most offset at their income: " + money(r.bestContribution * LAW.spouse.rate) + ".");
        } else {
            setBanner("You’d get the most offset at their income.");
        }

        // The spouse's own co-contribution: only for some work income under
        // the higher threshold. Shown whatever the offset's ticks say, since
        // its conditions are different.
        var co = C.coContribution({ income: p.spouseIncome, contributed: 0 });
        var panel = out("co-panel");
        panel.hidden = !(p.spouseIncome > 0 && co.maxForIncome > 0);
        if (!panel.hidden) {
            out("co-text").textContent = "If your spouse also puts " + money(co.bestContribution) +
                " of their own after-tax money into their super, the government could add up to " +
                money(co.maxForIncome) + " to it. That needs at least 10% of their income to come from work or a business, and the co-contribution’s other conditions.";
        }

        last = { income: p.spouseIncome, offset: r.offset };
        draw();
    }

    function draw() {
        if (!last) return;
        C.drawTaper(chartEl, {
            xMax: 45000, yMax: FULL, step: 100,
            at: function (inc) { return C.spouseOffset({ spouseIncome: inc, contributed: LAW.spouse.cap }).offset; },
            you: { x: last.income, y: last.offset },
            tip: function (inc, v) { return "Spouse’s income " + money(inc) + ": up to " + money(v); }
        });
    }

    root.addEventListener("input", update);
    root.addEventListener("change", update);
    C.tidyMoney(root);
    C.onResize(chartEl, draw);
    update();
})();
