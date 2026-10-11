(function () {
  var button = document.getElementById("theme-toggle");
  if (!button) return;
  function current() {
    var set = document.documentElement.getAttribute("data-theme");
    if (set) return set;
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
  function label(theme) {
    button.textContent = theme === "light" ? "Dark mode" : "Light mode";
    button.setAttribute("aria-pressed", String(theme === "light"));
  }
  label(current());
  button.addEventListener("click", function () {
    var next = current() === "light" ? "dark" : "light";
    document.documentElement.setAttribute("data-theme", next);
    document.cookie = "kammon_theme=" + next + "; Path=/; Max-Age=31536000; SameSite=Lax";
    label(next);
  });
})();
