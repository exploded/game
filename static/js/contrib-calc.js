// The sums behind two calculators, loaded before each page's own script:
// - /tools/co-contribution (co-contribution.js): the government
//   co-contribution on a low or middle earner's after-tax super contributions.
// - /tools/spouse-contribution (spouse-contribution.js): the spouse
//   contribution tax offset, with a co-contribution panel for the spouse.
//
// The rules (ATO, checked 19 November 2026):
// - Co-contribution (Superannuation (Government Co-contribution for Low Income
//   Earners) Act 2003): 50 cents for each dollar of personal after-tax
//   contributions, up to $500. Above the lower income threshold the $500
//   falls by 3.333 cents for each dollar of total income, and nothing is paid
//   from the higher threshold up. The ATO rounds the payment up to the next 5
//   cents and pays $20 when the amount works out under $20.
// - Spouse offset (Income Tax Assessment Act 1997 s 290-230): 18% of the
//   lesser of the contributions and $3,000 less the amount the spouse's
//   income is over $37,000, so up to $540, and nothing from $40,000 up.
//
// LAW changes each 1 July: the co-contribution thresholds are indexed to
// wages (published each February), and the total super balance and
// non-concessional caps move from time to time. The spouse figures have been
// fixed since 2017. Update LAW and the "2026–27" wording in both templates
// together.
//
// Everything happens in the browser: nothing typed is sent anywhere or
// saved, not even to localStorage.
(function () {
    "use strict";

    var LAW = {
        year: "2026–27",
        yearEnd: "30 June 2027",
        co: {
            lower: 49293,   // lower income threshold: the full $500 up to here
            higher: 64293,  // higher income threshold: nothing from here up
            max: 500,       // the most paid in a year
            match: 0.5,     // paid per dollar contributed
            taper: 3.333,   // cents taken off the $500 per dollar over lower
            min: 20,        // the least paid, when anything is
            age: 71         // must be under this at the end of the year
        },
        spouse: {
            base: 37000,    // spouse's income up to which the offset is in full
            cut: 40000,     // spouse's income from which there's none
            cap: 3000,      // contributions that count, at most
            rate: 0.18,     // the offset per dollar that counts
            age: 75         // the spouse must be under this
        },
        tsb: 2100000,       // total super balance limit (general transfer balance cap)
        tsbDate: "30 June 2026",
        ncc: 130000         // non-concessional contributions cap
    };

    // dollars rounds income to whole dollars, which is what the tests use.
    function dollars(v) {
        return v > 0 ? Math.round(v) : 0;
    }

    // coMaxMilli is the most co-contribution an income allows, in thousandths
    // of a cent so the 3.333-cent taper stays exact: $500 is 50,000,000.
    function coMaxMilli(income) {
        var c = LAW.co;
        if (income >= c.higher) return 0;
        var over = Math.max(0, income - c.lower);
        return Math.max(0, c.max * 100000 - over * Math.round(c.taper * 1000));
    }

    // coPay turns an amount in thousandths of a cent into what the ATO pays:
    // rounded up to the next 5 cents, and at least $20 when it's anything.
    function coPay(milli) {
        if (milli <= 0) return 0;
        var cents = Math.ceil(milli / 5000) * 5;
        return Math.max(LAW.co.min, cents / 100);
    }

    // coContribution works out the co-contribution for one person:
    //   income       total income for the year, dollars
    //   contributed  personal after-tax contributions, dollars
    //   eligible     false when one of the other conditions fails
    // It returns:
    //   amount            what the government would pay
    //   maxForIncome      the most this income allows, contributing enough
    //   bestContribution  the least contribution that gets maxForIncome
    //   reason            "" when paid, else "ineligible", "income" or "none"
    function coContribution(p) {
        var income = dollars(p.income);
        var cents = p.contributed > 0 ? Math.round(p.contributed * 100) : 0;
        var maxMilli = coMaxMilli(income);
        var res = {
            amount: 0,
            maxForIncome: coPay(maxMilli),
            // Contributing maxMilli / 0.5 gets the whole maximum. In thousandths
            // of a cent the match is cents * 500, so that's maxMilli / 500 cents.
            bestContribution: maxMilli > 0 ? Math.ceil(Math.ceil(maxMilli / 500) / 100) : 0,
            reason: ""
        };
        if (p.eligible === false) res.reason = "ineligible";
        else if (maxMilli <= 0) res.reason = "income";
        else if (cents <= 0) res.reason = "none";
        else res.amount = coPay(Math.min(cents * 500, maxMilli));
        return res;
    }

    // spouseOffset works out the spouse contribution tax offset:
    //   spouseIncome  the spouse's income for the year, dollars
    //   contributed   what you put into the spouse's super, dollars
    //   eligible      false when one of the other conditions fails
    // It returns:
    //   offset            the tax offset, dollars and cents
    //   counted           the part of the contribution the offset is on
    //   bestContribution  contributions above this add no more offset
    //   cost              contributed less the offset
    //   reason            "" when there's an offset, else "ineligible",
    //                     "income" or "none"
    function spouseOffset(p) {
        var s = LAW.spouse;
        var income = dollars(p.spouseIncome);
        var contributed = p.contributed > 0 ? Math.round(p.contributed * 100) / 100 : 0;
        var limit = Math.max(0, s.cap - Math.max(0, income - s.base));
        var res = { offset: 0, counted: 0, bestContribution: limit, cost: contributed, reason: "" };
        if (p.eligible === false) res.reason = "ineligible";
        else if (limit <= 0) res.reason = "income";
        else if (contributed <= 0) res.reason = "none";
        else {
            res.counted = Math.min(contributed, limit);
            res.offset = Math.round(res.counted * s.rate * 100) / 100;
            res.cost = Math.round((contributed - res.offset) * 100) / 100;
        }
        return res;
    }

    var api = { LAW: LAW, coContribution: coContribution, spouseOffset: spouseOffset };

    if (typeof module !== "undefined") {
        module.exports = api;
        return;
    }

    // ---- Shared by both pages ----

    var whole = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", maximumFractionDigits: 0 });
    var cents = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", minimumFractionDigits: 2, maximumFractionDigits: 2 });

    // money shows whole dollars without cents and anything else with them.
    api.money = function (v) {
        return Math.round(v * 100) % 100 === 0 ? whole.format(Math.round(v)) : cents.format(v);
    };

    api.num = function (el) {
        var s = el.value.replace(/[^0-9.]/g, "");
        return s === "" ? NaN : parseFloat(s);
    };

    // firstUnticked is the first eligibility box left unticked, or null.
    api.firstUnticked = function (root) {
        var boxes = root.querySelectorAll("[data-elig]");
        for (var i = 0; i < boxes.length; i++) if (!boxes[i].checked) return boxes[i];
        return null;
    };

    // tidyMoney puts thousands separators back into a money box on leaving it.
    api.tidyMoney = function (root) {
        root.addEventListener("focusout", function (ev) {
            if (!ev.target.hasAttribute || !ev.target.hasAttribute("data-money")) return;
            var v = api.num(ev.target);
            if (!isNaN(v)) ev.target.value = Math.round(v).toLocaleString("en-AU");
        });
    };

    var NS = "http://www.w3.org/2000/svg";
    function svgEl(name, attrs, parent) {
        var e = document.createElementNS(NS, name);
        for (var k in attrs) e.setAttribute(k, attrs[k]);
        if (parent) parent.appendChild(e);
        return e;
    }

    // drawTaper draws what a top-up is worth at each income, as a line, with a
    // dot for the visitor's own result. o is
    //   { xMax, yMax, step (income between points), at (income -> value),
    //     you: { x, y } or null, tip (income, value -> text) }.
    // Pointing at the chart shows the value at that income.
    api.drawTaper = function (chartEl, o) {
        var W = Math.max(280, chartEl.clientWidth), H = W < 500 ? 220 : 280;
        var pad = { l: 46, r: 14, t: 14, b: 28 };
        var iw = W - pad.l - pad.r, ih = H - pad.t - pad.b;
        var x = function (v) { return pad.l + v / o.xMax * iw; };
        var y = function (v) { return pad.t + ih - v / o.yMax * ih; };

        chartEl.textContent = "";
        var svg = svgEl("svg", { width: W, height: H, viewBox: "0 0 " + W + " " + H }, chartEl);
        var yStep = o.yMax > 500 ? 135 : 100;
        for (var v = 0; v <= o.yMax + 0.5; v += yStep) {
            svgEl("line", { x1: pad.l, x2: W - pad.r, y1: y(v), y2: y(v), "class": "mc-grid" }, svg);
            svgEl("text", { x: pad.l - 8, y: y(v) + 4, "class": "mc-axis", "text-anchor": "end" }, svg).textContent = "$" + v;
        }
        var xStep = o.xMax > 50000 ? (W < 500 ? 20000 : 10000) : (W < 500 ? 10000 : 5000);
        for (var a = 0; a <= o.xMax; a += xStep) {
            svgEl("text", { x: x(a), y: H - 8, "class": "mc-axis", "text-anchor": a === 0 ? "start" : a === o.xMax ? "end" : "middle" }, svg)
                .textContent = "$" + a / 1000 + "k";
        }

        var pts = [];
        for (var i = 0; i <= o.xMax; i += o.step) pts.push(x(i).toFixed(1) + "," + y(o.at(i)).toFixed(1));
        svgEl("polygon", { points: pts.concat([x(o.xMax).toFixed(1) + "," + y(0), x(0) + "," + y(0)]).join(" "), "class": "mc-gap" }, svg);
        svgEl("polyline", { points: pts.join(" "), "class": "mc-line mc-line-extra" }, svg);

        if (o.you) {
            var yx = x(Math.min(o.you.x, o.xMax)), yy = y(o.you.y);
            svgEl("circle", { cx: yx, cy: yy, r: 7, "class": "mc-dot kc-dot" }, svg);
            // Above the dot, or below it when the dot is at the top.
            var label = svgEl("text", { x: yx, y: yy - 14 < pad.t + 10 ? yy + 24 : yy - 14, "class": "mc-label kc-label", "text-anchor": yx > W - 60 ? "end" : yx < pad.l + 40 ? "start" : "middle" }, svg);
            label.textContent = "You";
        }

        var guide = svgEl("line", { y1: pad.t, y2: pad.t + ih, "class": "mc-guide", visibility: "hidden" }, svg);
        var tip = document.createElement("div");
        tip.className = "mc-tip";
        tip.hidden = true;
        chartEl.appendChild(tip);
        var show = function (ev) {
            var box = svg.getBoundingClientRect();
            var inc = (ev.clientX - box.left - pad.l) / iw * o.xMax;
            inc = Math.max(0, Math.min(o.xMax, Math.round(inc / 500) * 500));
            var gx = x(inc);
            guide.setAttribute("x1", gx); guide.setAttribute("x2", gx);
            guide.setAttribute("visibility", "visible");
            tip.textContent = o.tip(inc, o.at(inc));
            tip.hidden = false;
            var left = gx + 12;
            if (left + tip.offsetWidth > W) left = gx - 12 - tip.offsetWidth;
            tip.style.left = Math.max(0, left) + "px";
        };
        var hide = function () {
            guide.setAttribute("visibility", "hidden");
            tip.hidden = true;
        };
        svg.addEventListener("pointermove", show);
        svg.addEventListener("pointerdown", show);
        svg.addEventListener("pointerleave", hide);
    };

    // onResize redraws a chart when its width changes.
    api.onResize = function (chartEl, draw) {
        var width = 0;
        if (!window.ResizeObserver) return;
        new ResizeObserver(function () {
            if (chartEl.clientWidth !== width) { width = chartEl.clientWidth; draw(); }
        }).observe(chartEl);
    };

    window.ContribCalc = api;
})();
