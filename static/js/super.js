// Start Early: Extra Super Calculator (/tools/super), rebuilt from Rob's
// spreadsheet "Super Starting Early Calculator Aug 2026". Everything happens
// here in the browser: nothing typed is sent anywhere or saved, not even to
// localStorage.
//
// The sums follow the spreadsheet's hidden "Calculations" sheet:
// - Each week's contribution goes in after 15% contribution tax.
// - The return after tax is the total return less tax on its income part:
//   return - income component x 15% (9% - 3% x 15% = 8.55%), compounded
//   weekly as (1 + that)^(1/52) - 1.
// - Contributions are paid at the end of each week for 20 years (an
//   annuity), then the balance grows yearly until age 60.
// - The inflation-adjusted figure divides by (1 + 2.6%)^(age - 20): today's
//   dollars for someone who is 20 now, as in the sheet.
(function () {
    "use strict";

    // The sheet's fixed assumptions (Calculator!F6, F8, I6, I8).
    var CONTRIB_TAX = 0.15, INCOME_PART = 0.03, INCOME_TAX = 0.15, INFLATION = 0.026;
    var YEARS = 20, FROM = 20, TO = 60, STARTS = [20, 30, 40];

    // balanceAt is one scenario's balance at an age, in future dollars.
    function balanceAt(weekly, ret, start, age) {
        var c = weekly * (1 - CONTRIB_TAX);
        var r = ret - INCOME_PART * INCOME_TAX;
        var w = Math.pow(1 + r, 1 / 52) - 1;
        if (age <= start) return 0;
        var paying = Math.min(age - start, YEARS);
        var weeks = 52 * paying;
        var saved = w === 0 ? c * weeks : c * (Math.pow(1 + w, weeks) - 1) / w;
        return saved * Math.pow(1 + r, age - start - paying);
    }

    function real(value, age) {
        return value / Math.pow(1 + INFLATION, age - FROM);
    }

    function calc(weekly, ret) {
        return STARTS.map(function (start) {
            var path = [];
            for (var age = FROM; age <= TO; age++) path.push(balanceAt(weekly, ret, start, age));
            var atEnd = path[path.length - 1];
            return { start: start, path: path, value: atEnd, real: real(atEnd, TO) };
        });
    }

    if (typeof module !== "undefined") {
        module.exports = { calc: calc };
        return;
    }

    var root = document.querySelector("[data-scalc]");
    if (!root) return;

    var money = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", maximumFractionDigits: 0 });
    var out = function (name) { return root.querySelector('[data-out="' + name + '"]'); };
    var weeklyInput = root.querySelector('[data-sc="weekly"]');
    var returnInput = root.querySelector('[data-sc="return"]');
    var slider = root.querySelector("[data-sc-slider]");
    var last = null;

    function num(el) {
        var s = el.value.replace(/[^0-9.]/g, "");
        return s === "" ? NaN : parseFloat(s);
    }

    function update() {
        var weekly = num(weeklyInput), ret = num(returnInput) / 100;
        if (isNaN(weekly)) weekly = 0;
        var err = "";
        if (weekly > 2000) err = "Enter a weekly contribution of $2,000 or less.";
        else if (!(ret >= 0 && ret <= 0.2)) err = "Enter a return between 0% and 20%.";
        var errEl = out("error");
        root.classList.toggle("mcalc-invalid", !!err);
        errEl.hidden = !err;
        errEl.textContent = err;
        if (err) return;

        var res = calc(weekly, ret);
        res.forEach(function (s) {
            out("v" + s.start).textContent = money.format(s.value);
            out("r" + s.start).textContent = "Approx. " + money.format(s.real) + " inflation adjusted";
        });
        var put = weekly * 52 * YEARS;
        var times = res[2].value > 0 ? res[0].value / res[2].value : 0;
        var banner = "Enter a weekly amount to compare the three scenarios.";
        if (weekly > 0) {
            banner = "Each scenario puts in the same " + money.format(put) + " (" +
                money.format(put * (1 - CONTRIB_TAX)) + " after contribution tax).";
            // A return at or below the tax drag leaves nothing to compound.
            if (times >= 1.05) banner += " Starting at 20 instead of 40 ends with " + times.toFixed(1) + " times as much at 60.";
        }
        out("banner").textContent = banner;
        last = res;
        drawChart();
    }

    // ---- Chart: inline SVG, redrawn when its box changes width ----

    var chartEl = out("chart");
    var NS = "http://www.w3.org/2000/svg";
    var SERIES = ["scalc-line20", "scalc-line30", "scalc-line40"];
    var SWATCH = ["scalc-sw20", "scalc-sw30", "scalc-sw40"];

    function el(name, attrs, parent) {
        var e = document.createElementNS(NS, name);
        for (var k in attrs) e.setAttribute(k, attrs[k]);
        if (parent) parent.appendChild(e);
        return e;
    }

    function niceStep(raw) {
        var p = Math.pow(10, Math.floor(Math.log10(raw)));
        var f = raw / p;
        return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10) * p;
    }

    function shortMoney(v) {
        if (v >= 1e6) return "$" + (v / 1e6).toFixed(v % 1e6 ? 1 : 0) + "m";
        if (v >= 1e3) return "$" + Math.round(v / 1e3) + "k";
        return "$" + Math.round(v);
    }

    function drawChart() {
        if (!last) return;
        var W = Math.max(280, chartEl.clientWidth), H = W < 500 ? 240 : 320;
        var pad = { l: 52, r: 14, t: 14, b: 28 };
        var iw = W - pad.l - pad.r, ih = H - pad.t - pad.b;
        var top = Math.max(1, last[0].value, last[1].value, last[2].value);
        var step = niceStep(top / 4), yMax = Math.ceil(top / step) * step;
        var x = function (age) { return pad.l + (age - FROM) / (TO - FROM) * iw; };
        var y = function (v) { return pad.t + ih - v / yMax * ih; };

        chartEl.textContent = "";
        var svg = el("svg", { width: W, height: H, viewBox: "0 0 " + W + " " + H }, chartEl);
        for (var v = 0; v <= yMax + 0.5; v += step) {
            el("line", { x1: pad.l, x2: W - pad.r, y1: y(v), y2: y(v), "class": "mc-grid" }, svg);
            el("text", { x: pad.l - 8, y: y(v) + 4, "class": "mc-axis", "text-anchor": "end" }, svg).textContent = shortMoney(v);
        }
        var xStep = W < 500 ? 10 : 5;
        for (var a = FROM; a <= TO; a += xStep) {
            el("text", { x: x(a), y: H - 8, "class": "mc-axis", "text-anchor": "middle" }, svg).textContent = a === FROM ? "Age " + a : a;
        }
        last.forEach(function (s, i) {
            var pts = s.path.map(function (val, k) { return x(FROM + k).toFixed(1) + "," + y(val).toFixed(1); }).join(" ");
            el("polyline", { points: pts, "class": "mc-line " + SERIES[i] }, svg);
            el("circle", { cx: x(TO), cy: y(s.value), r: 5, "class": "scalc-dot " + SERIES[i] }, svg);
        });

        // Hover or drag: a guide line and the three balances at that age.
        var guide = el("line", { y1: pad.t, y2: pad.t + ih, "class": "mc-guide", visibility: "hidden" }, svg);
        var dots = last.map(function (s, i) { return el("circle", { r: 4, "class": "scalc-hover " + SERIES[i], visibility: "hidden" }, svg); });
        var tip = document.createElement("div");
        tip.className = "mc-tip";
        tip.hidden = true;
        chartEl.appendChild(tip);

        var show = function (ev) {
            var box = svg.getBoundingClientRect();
            var age = Math.round(FROM + (ev.clientX - box.left - pad.l) / iw * (TO - FROM));
            age = Math.max(FROM, Math.min(TO, age));
            var gx = x(age);
            guide.setAttribute("x1", gx); guide.setAttribute("x2", gx);
            guide.setAttribute("visibility", "visible");
            tip.textContent = "";
            var head = document.createElement("strong");
            head.textContent = "At age " + age;
            tip.appendChild(head);
            last.forEach(function (s, i) {
                var val = s.path[age - FROM];
                dots[i].setAttribute("cx", gx); dots[i].setAttribute("cy", y(val));
                dots[i].setAttribute("visibility", "visible");
                var line = document.createElement("div");
                var sw = document.createElement("span");
                sw.className = "mcalc-swatch " + SWATCH[i];
                line.appendChild(sw);
                line.appendChild(document.createTextNode("Start " + s.start + ": " + money.format(val)));
                tip.appendChild(line);
            });
            tip.hidden = false;
            var left = gx + 12;
            if (left + tip.offsetWidth > W) left = gx - 12 - tip.offsetWidth;
            tip.style.left = left + "px";
        };
        var hide = function () {
            guide.setAttribute("visibility", "hidden");
            dots.forEach(function (d) { d.setAttribute("visibility", "hidden"); });
            tip.hidden = true;
        };
        svg.addEventListener("pointermove", show);
        svg.addEventListener("pointerdown", show);
        svg.addEventListener("pointerleave", hide);
    }

    // ---- Inputs ----

    root.addEventListener("input", function (ev) {
        if (ev.target === slider) {
            weeklyInput.value = slider.value;
        } else if (ev.target === weeklyInput) {
            var v = num(weeklyInput);
            slider.value = isNaN(v) ? 0 : Math.min(v, Number(slider.max));
        }
        update();
    });

    var width = 0;
    if (window.ResizeObserver) {
        new ResizeObserver(function () {
            if (chartEl.clientWidth !== width) { width = chartEl.clientWidth; drawChart(); }
        }).observe(chartEl);
    }
    update();
})();
