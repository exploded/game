// Super co-contribution calculator (/tools/co-contribution). The sums and the
// 2026–27 rules are in contrib-calc.js, loaded first. Nothing typed is sent
// anywhere or saved.
(function () {
    "use strict";

    var C = window.ContribCalc;
    var root = document.querySelector("[data-kcalc-co]");
    if (!C || !root) return;

    var LAW = C.LAW, money = C.money;
    var out = function (name) { return root.querySelector('[data-out="' + name + '"]'); };
    var field = function (name) { return root.querySelector('[data-k="' + name + '"]'); };
    var chartEl = out("chart");
    var last = null;

    function check(p) {
        if (isNaN(p.income)) return "Enter your total income for the year.";
        if (p.income > 100e6) return "Enter an income of $100 million or less.";
        if (p.contributed > 10e6) return "Enter an amount of $10 million or less.";
        return "";
    }

    function setBanner(text, warn) {
        var b = out("banner");
        b.textContent = text;
        b.classList.toggle("kc-banner-warn", !!warn);
    }

    function update() {
        var p = { income: C.num(field("income")), contributed: C.num(field("contributed")) };
        if (isNaN(p.contributed)) p.contributed = 0;
        var err = check(p);
        var errEl = out("error");
        root.classList.toggle("mcalc-invalid", !!err);
        errEl.hidden = !err;
        errEl.textContent = err;
        if (err) return;

        var unticked = C.firstUnticked(root);
        p.eligible = !unticked;
        var r = C.coContribution(p);

        out("amount").textContent = money(r.amount);
        out("amount-note").textContent = r.amount > 0
            ? "Paid into your super fund, not your bank account, after you lodge your " + LAW.year + " tax return."
            : "";
        out("max").textContent = money(r.maxForIncome);
        out("best").textContent = money(r.bestContribution);
        out("max-note").textContent = r.maxForIncome > 0 && r.maxForIncome < LAW.co.max
            ? "Less than the full $500, because your income is over " + money(LAW.co.lower) + "."
            : "";

        if (r.reason === "ineligible") {
            setBanner("No co-contribution: " + unticked.getAttribute("data-why"), true);
        } else if (r.reason === "income") {
            setBanner("Your income is " + money(LAW.co.higher) + " or more, so there’s no co-contribution for " + LAW.year + ".", true);
        } else if (r.reason === "none") {
            setBanner("Enter what you’ll put in. At your income, " + money(r.bestContribution) + " gets the most: " + money(r.maxForIncome) + ".");
        } else if (r.amount < r.maxForIncome) {
            setBanner("Put in " + money(r.bestContribution - p.contributed) + " more, " + money(r.bestContribution) +
                " in all, to get the most your income allows: " + money(r.maxForIncome) + ".");
        } else if (p.contributed > r.bestContribution) {
            setBanner("You’d get the most your income allows. Anything over " + money(r.bestContribution) +
                " adds no more co-contribution, though it still goes into your super.");
        } else {
            setBanner("You’d get the most your income allows.");
        }

        last = { income: p.income, amount: r.amount };
        draw();
    }

    function draw() {
        if (!last) return;
        C.drawTaper(chartEl, {
            xMax: 70000, yMax: LAW.co.max, step: 50,
            at: function (inc) { return C.coContribution({ income: inc, contributed: 0 }).maxForIncome; },
            you: { x: last.income, y: last.amount },
            tip: function (inc, v) { return "Income " + money(inc) + ": up to " + money(v); }
        });
    }

    root.addEventListener("input", update);
    root.addEventListener("change", update);
    C.tidyMoney(root);
    C.onResize(chartEl, draw);
    update();
})();
