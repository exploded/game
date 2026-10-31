// The CSP blocks inline event handlers (onclick="..."), so page behaviour
// lives here, hooked up through data- attributes.

// Toast notification listener
document.body.addEventListener("showToast", function(e) {
    const { msg, type } = e.detail;
    showToast(msg, type);
});

// Toasts stay long enough to read on a phone (errors longer), and a tap
// dismisses one early.
function showToast(msg, type) {
    const container = document.getElementById("toast-container");
    if (!container) return;
    const toast = document.createElement("div");
    toast.className = "toast toast-" + (type || "info");
    toast.setAttribute("role", type === "error" ? "alert" : "status");
    toast.textContent = msg;
    container.appendChild(toast);
    const hide = () => {
        clearTimeout(timer);
        toast.classList.remove("toast-show");
        setTimeout(() => toast.remove(), 300);
    };
    const timer = setTimeout(hide, type === "error" ? 9000 : 6000);
    toast.addEventListener("click", hide);
    setTimeout(() => toast.classList.add("toast-show"), 10);
}

// htmx config keeps 4xx/5xx responses out of the page, so say something.
document.body.addEventListener("htmx:response:error", function(e) {
    const status = e.detail.ctx.response.status;
    showToast(status === 429 ? "Slow down - too many requests"
            : status === 403 ? "Session expired - please reload the page"
            : status >= 500 ? "Server error - please try again"
            : "Request failed", "error");
});
document.body.addEventListener("htmx:error", function() {
    showToast("Connection problem - please try again", "error");
});

// askConfirm shows the site's own confirm dialog (the browser's confirm()
// box can't be styled) and resolves true if the player says yes. The yes
// button takes okLabel, or the label of the button that asked, and is red
// when that button is btn-danger, with No focused so Enter is safe.
let confirmDialog;
function askConfirm(msg, btn, okLabel) {
    if (!confirmDialog) {
        confirmDialog = document.createElement("dialog");
        confirmDialog.className = "confirm";
        confirmDialog.innerHTML = '<form method="dialog"><p class="confirm-msg"></p><div class="confirm-actions">'
            + '<button value="" class="btn btn-secondary"></button><button value="ok" class="btn"></button></div></form>';
        document.body.appendChild(confirmDialog);
        // A tap on the backdrop says no.
        confirmDialog.addEventListener("click", function(e) {
            if (e.target === confirmDialog) confirmDialog.close();
        });
    }
    const no = confirmDialog.querySelector('[value=""]');
    const yes = confirmDialog.querySelector('[value="ok"]');
    const danger = !!btn && btn.classList.contains("btn-danger");
    const label = okLabel || (btn && btn.textContent.trim()) || "OK";
    confirmDialog.querySelector(".confirm-msg").textContent = msg;
    yes.textContent = label;
    yes.className = "btn " + (danger ? "btn-danger" : "btn-primary");
    no.textContent = /^cancel\b/i.test(label) ? "Go back" : "Cancel";
    return new Promise(function(resolve) {
        confirmDialog.addEventListener("close", function() {
            resolve(confirmDialog.returnValue === "ok");
        }, {once: true});
        confirmDialog.returnValue = "";
        confirmDialog.showModal();
        (danger ? no : yes).focus();
    });
}

// data-confirm on a submit button or a form: ask before submitting.
// data-confirm-ok beside it overrides the yes button's label.
let confirmedForm = null;
document.addEventListener("submit", function(e) {
    const form = e.target;
    const el = (e.submitter && e.submitter.closest("[data-confirm]")) || form.closest("form[data-confirm]");
    if (!el) return;
    if (confirmedForm === form) {
        confirmedForm = null;
        return;
    }
    e.preventDefault();
    const btn = e.submitter;
    askConfirm(el.getAttribute("data-confirm"), btn, el.getAttribute("data-confirm-ok")).then(function(ok) {
        if (!ok) return;
        confirmedForm = form;
        form.requestSubmit(btn);
    });
});

// hx-confirm: the same dialog for htmx requests.
document.body.addEventListener("htmx:confirm", function(e) {
    e.preventDefault();
    const d = e.detail, src = d.ctx.sourceElement;
    askConfirm(d.ctx.confirm, src.closest("button"), src.getAttribute("data-confirm-ok")).then(function(ok) {
        if (ok) d.issueRequest(); else d.dropRequest();
    });
});

// data-autosubmit on a select: submit its form when it changes.
document.addEventListener("change", function(e) {
    if (e.target.matches("select[data-autosubmit]")) {
        e.target.form.requestSubmit();
    }
});

// data-prefill on a form (Add a stream): JSON of field values for each
// value of the select named by data-prefill-by. Choosing an option fills the
// fields, leaving alone any the admin has typed in.
document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll("form[data-prefill]").forEach(function(form) {
        let sets;
        try { sets = JSON.parse(form.getAttribute("data-prefill")); } catch (err) { return; }
        const select = form.elements[form.getAttribute("data-prefill-by")];
        if (!select) return;
        const names = new Set();
        Object.values(sets).forEach(function(set) { Object.keys(set).forEach(function(n) { names.add(n); }); });
        const fill = function() {
            const set = sets[select.value] || {};
            names.forEach(function(name) {
                const field = form.elements[name];
                if (!field || (field.value !== "" && field.value !== field.dataset.filled)) return;
                field.value = set[name] || "";
                field.dataset.filled = field.value;
            });
        };
        select.addEventListener("change", fill);
        fill();
    });
});

// data-clear-search on a form (the stocks page's chips): changing it
// empties the search box it names, so the chip's list shows, not a search.
document.addEventListener("change", function(e) {
    const form = e.target.closest("form[data-clear-search]");
    const box = form && document.querySelector(form.getAttribute("data-clear-search"));
    if (box) box.value = "";
});

// A row of chips scrolls sideways on a phone: bring the chosen one into view.
document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll(".chips .chip:has(:checked)").forEach(function(chip) {
        chip.scrollIntoView({block: "nearest", inline: "center"});
    });
});

// data-back on a link (the stock page's Back): the server points it at the
// page the player came from. When that's the page before this one in the
// browser's history, go back instead, so the list keeps its scroll position.
document.addEventListener("click", function(e) {
    const link = e.target.closest("a[data-back]");
    if (!link || e.metaKey || e.ctrlKey || e.shiftKey) return;
    let prev;
    try { prev = new URL(document.referrer); } catch (err) { return; }
    const target = new URL(link.href);
    if (history.length > 1 && prev.origin === location.origin
        && prev.pathname + prev.search === target.pathname + target.search) {
        e.preventDefault();
        history.back();
    }
});

// The Stocks tab goes back to the list as the player left it (chip, sector
// or search) rather than the default list. Remembered for this browser tab.
(function() {
    const m = location.pathname.match(/^\/games\/(\d+)(\/stocks\/?$)?/);
    if (!m) return;
    const key = "stocks-list:" + m[1];
    try {
        if (m[2]) {
            // The chips and search change the address without reloading.
            const save = function() { sessionStorage.setItem(key, location.pathname + location.search); };
            save();
            window.addEventListener("pagehide", save);
            document.body.addEventListener("htmx:after:swap", save);
            return;
        }
        const list = sessionStorage.getItem(key);
        if (list) document.querySelectorAll("a[data-stocks-tab]").forEach(function(a) { a.href = list; });
    } catch (err) {
        // Storage blocked: the tab opens the default list.
    }
})();

// data-nav-toggle: open and close the mobile nav.
document.addEventListener("click", function(e) {
    const btn = e.target.closest("[data-nav-toggle]");
    if (btn) {
        btn.parentElement.classList.toggle("nav-open");
    }
});

// details[data-dismiss] (the game switcher and the phone tab bar's More
// menu): close when tapping anywhere outside it.
document.addEventListener("click", function(e) {
    document.querySelectorAll("details[data-dismiss][open]").forEach(function(d) {
        if (!d.contains(e.target)) d.removeAttribute("open");
    });
});

// data-reset-on-success on an htmx form: clear it and refocus after a
// successful request (chat and lobby).
document.body.addEventListener("htmx:after:request", function(e) {
    const form = e.target.closest("form[data-reset-on-success]");
    if (!form || e.detail.ctx.response.status >= 400) return;
    form.reset();
    const input = form.querySelector("input[type=text]");
    if (input) input.focus();
});

// Auto-inject CSRF hidden field into all POST forms.
document.addEventListener("DOMContentLoaded", function() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    if (!meta) return;
    var token = meta.getAttribute("content");
    if (!token) return;
    document.querySelectorAll("form").forEach(function(form) {
        var method = (form.getAttribute("method") || "GET").toUpperCase();
        if (method !== "GET" && !form.querySelector('input[name="_csrf"]')) {
            var input = document.createElement("input");
            input.type = "hidden";
            input.name = "_csrf";
            input.value = token;
            form.appendChild(input);
        }
    });
});

// Promote flash banners into toasts.
document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll(".flash").forEach(function(el) {
        const type = el.classList.contains("flash-success") ? "success"
                   : el.classList.contains("flash-error") ? "error" : "info";
        showToast(el.textContent.trim(), type);
        el.remove();
    });
});

// data-play on a video's button: swap in the player only when it's tapped,
// so a page of videos doesn't load a dozen third-party players up front.
document.addEventListener("click", function(e) {
    const btn = e.target.closest("[data-play]");
    if (!btn) return;
    const src = new URL(btn.getAttribute("data-play"));
    src.searchParams.set("autoplay", "1");
    const frame = document.createElement("iframe");
    frame.src = src.toString();
    frame.title = btn.getAttribute("data-title") || "Video";
    frame.allow = "accelerometer; autoplay; encrypted-media; gyroscope; picture-in-picture; fullscreen";
    frame.referrerPolicy = "strict-origin-when-cross-origin";
    btn.replaceWith(frame);
});
