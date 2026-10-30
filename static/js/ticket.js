// The order ticket on the stock page (stocks/_ticket.html). It keeps the
// shares box, slider and estimate in step, and on a phone opens the ticket
// as a sheet from the Buy and Sell bar. A trade redraws the ticket (htmx),
// so setup runs again on each new form.
(function() {
    function fmt(cents) {
        const abs = Math.abs(cents);
        const s = "$" + Math.floor(abs / 100).toLocaleString("en-US") + "." + String(abs % 100).padStart(2, "0");
        return cents < 0 ? "-" + s : s;
    }

    function setup() {
        const form = document.getElementById("ticket-form");
        if (!form || form.dataset.ready) return;
        form.dataset.ready = "1";

        // All money in cents. rate is AUD/USD * 1,000,000.
        const d = form.dataset;
        const cashCents = parseInt(d.cash, 10);
        const held = parseInt(d.held, 10);
        const feeCents = parseInt(d.fee, 10);
        const allowShort = "short" in d;
        const stockCurrency = d.currency;
        const baseCurrency = d.base;
        const exchangeRate = parseInt(d.rate, 10);
        // Market closed: a market order queues for the open (see _ticket.html).
        const queued = "queued" in d;

        const qty = document.getElementById("qty");
        const slider = document.getElementById("qty-slider");
        const maxLabel = document.getElementById("qty-max");
        const btn = document.getElementById("ticket-btn");
        const orderType = document.getElementById("order-type");
        const limitFields = document.getElementById("limit-fields");
        const limitPrice = document.getElementById("limit-price");
        const queuedNote = document.getElementById("queued-note");

        function toBase(cents) {
            if (stockCurrency === baseCurrency) return cents;
            if (stockCurrency === "AUD" && baseCurrency === "USD") return Math.round(cents * exchangeRate / 1000000);
            if (stockCurrency === "USD" && baseCurrency === "AUD") return Math.round(cents * 1000000 / exchangeRate);
            return cents;
        }

        function side() { return form.elements.side.value; }
        function isLimit() { return !!orderType && orderType.value === "limit"; }
        function price() {
            return isLimit() ? Math.round((parseFloat(limitPrice.value) || 0) * 100) : parseInt(d.price, 10);
        }

        // The most you can buy with your cash, or sell from what you hold. In a
        // short-selling game a market sell can go past your holding by as much
        // as your cash would buy.
        function maxShares() {
            const per = toBase(price());
            const affordable = per > 0 ? Math.max(0, Math.floor((cashCents - feeCents) / per)) : 0;
            if (side() === "buy") return affordable;
            if (allowShort && !isLimit()) return Math.max(0, held) + affordable;
            return Math.max(0, held);
        }

        function update() {
            const max = maxShares();
            qty.max = max;
            slider.max = max;
            maxLabel.textContent = max.toLocaleString();
            let n = parseInt(qty.value, 10) || 0;
            n = Math.min(Math.max(n, 0), max);
            qty.value = n;
            slider.value = n;

            const buying = side() === "buy";
            const native = n * price();
            const base = toBase(native);
            const total = n > 0 ? (buying ? base + feeCents : Math.max(0, base - feeCents)) : 0;
            const after = cashCents + (buying ? -total : total);

            const word = buying ? "Cost" : "Proceeds";
            document.getElementById("est-native-label").textContent = word + " (" + stockCurrency + ")";
            document.getElementById("est-native").textContent = fmt(native);
            const convLabel = document.getElementById("est-converted-label");
            if (convLabel) {
                convLabel.textContent = word + " (" + baseCurrency + ")";
                document.getElementById("est-converted").textContent = fmt(base);
            }
            document.getElementById("est-total-label").textContent = (buying ? "Total" : "Net proceeds") + " (" + baseCurrency + ")";
            document.getElementById("est-total").textContent = fmt(total);
            const rem = document.getElementById("est-remaining");
            rem.textContent = fmt(after);
            rem.className = after < 0 ? "text-red" : "";

            if (limitFields) limitFields.hidden = !isLimit();
            if (limitPrice) limitPrice.required = isLimit();
            if (queuedNote) queuedNote.hidden = isLimit();

            const shares = n.toLocaleString() + " share" + (n === 1 ? "" : "s");
            let action = buying ? "Buy " : "Sell ";
            if (isLimit()) action = "Place limit " + side() + ": ";
            else if (queued) action = "Queue " + side() + ": ";
            btn.textContent = action + shares;
            btn.className = "btn " + (buying ? "btn-primary" : "btn-danger");
            btn.disabled = n <= 0 || after < 0 || (isLimit() && price() <= 0);
        }

        qty.addEventListener("input", function() { slider.value = qty.value; update(); });
        slider.addEventListener("input", function() { qty.value = slider.value; update(); });
        form.addEventListener("change", update);
        if (limitPrice) limitPrice.addEventListener("input", update);
        form.addEventListener("submit", function(e) {
            if (btn.disabled) e.preventDefault();
        });
        update();

        // A trade can make Sell possible (the first shares bought) or not
        // (the last ones sold): the bar's Sell follows the ticket's.
        const barSell = document.querySelector('.trade-bar [data-ticket-open="sell"]');
        if (barSell) barSell.disabled = document.getElementById("side-sell").disabled;
    }

    // The price header refreshes while the market is open; keep the estimate current.
    document.addEventListener("price:update", function(e) {
        const form = document.getElementById("ticket-form");
        if (!form) return;
        form.dataset.price = e.detail.close;
        form.dispatchEvent(new Event("change"));
    });

    // The confirmation after a trade hides the form until "Trade again".
    function clearDone() {
        const ticket = document.getElementById("ticket");
        if (!ticket) return;
        ticket.classList.remove("is-done");
        const done = ticket.querySelector(".ticket-done");
        if (done) done.remove();
    }

    // The sheet (phones only; on a desktop the ticket is always showing).
    function openSheet(side) {
        clearDone();
        const radio = document.getElementById(side === "sell" ? "side-sell" : "side-buy");
        if (radio && !radio.disabled) {
            radio.checked = true;
            radio.dispatchEvent(new Event("change", {bubbles: true}));
        }
        document.body.classList.add("ticket-open");
    }
    function closeSheet() {
        if (!document.body.classList.contains("ticket-open")) return;
        document.body.classList.remove("ticket-open");
        clearDone();
    }

    document.addEventListener("click", function(e) {
        const open = e.target.closest("[data-ticket-open]");
        if (open) return openSheet(open.getAttribute("data-ticket-open"));
        if (e.target.closest("[data-ticket-close]")) return closeSheet();
        if (e.target.closest("[data-ticket-again]")) return clearDone();
    });
    document.addEventListener("keydown", function(e) {
        if (e.key === "Escape") closeSheet();
    });

    setup();
    document.body.addEventListener("htmx:after:swap", setup);
})();
