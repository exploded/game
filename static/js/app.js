// The CSP blocks inline event handlers (onclick="..."), so page behaviour
// lives here, hooked up through data- attributes.

// Toast notification listener
document.body.addEventListener("showToast", function(e) {
    const { msg, type } = e.detail;
    showToast(msg, type);
});

function showToast(msg, type) {
    const container = document.getElementById("toast-container");
    if (!container) return;
    const toast = document.createElement("div");
    toast.className = "toast toast-" + type;
    toast.textContent = msg;
    container.appendChild(toast);
    setTimeout(() => toast.classList.add("toast-show"), 10);
    setTimeout(() => {
        toast.classList.remove("toast-show");
        setTimeout(() => toast.remove(), 300);
    }, 3000);
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

// data-confirm on a submit button or a form: ask before submitting.
document.addEventListener("submit", function(e) {
    const el = (e.submitter && e.submitter.closest("[data-confirm]")) || e.target.closest("form[data-confirm]");
    if (el && !confirm(el.getAttribute("data-confirm"))) {
        e.preventDefault();
    }
});

// data-autosubmit on a select: submit its form when it changes.
document.addEventListener("change", function(e) {
    if (e.target.matches("select[data-autosubmit]")) {
        e.target.form.requestSubmit();
    }
});

// data-nav-toggle: open and close the mobile nav.
document.addEventListener("click", function(e) {
    const btn = e.target.closest("[data-nav-toggle]");
    if (btn) {
        btn.parentElement.classList.toggle("nav-open");
    }
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

// Price status countdown: ticks every second between HTMX 30s refreshes.
setInterval(function() {
    var el = document.querySelector(".price-status.countdown");
    if (!el) return;
    var target = parseInt(el.getAttribute("data-target"), 10);
    if (!target) return;
    var remaining = target - Math.floor(Date.now() / 1000);
    if (remaining <= 0) {
        el.textContent = "Updating soon…";
        return;
    }
    var h = Math.floor(remaining / 3600);
    var m = Math.floor((remaining % 3600) / 60);
    el.textContent = h > 0 ? "Prices in " + h + "h " + m + "m" : "Prices in " + m + "m";
}, 60000);

// Promote flash banners into toasts.
document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll(".flash").forEach(function(el) {
        const type = el.classList.contains("flash-success") ? "success"
                   : el.classList.contains("flash-error") ? "error" : "info";
        showToast(el.textContent.trim(), type);
        el.remove();
    });
});
