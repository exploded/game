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
