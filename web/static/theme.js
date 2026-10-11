(function () {
  var button = document.getElementById("theme-toggle");
  if (!button) return;
  function current() {
    var set = document.documentElement.getAttribute("data-theme");
    if (set) return set;
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
  function label(theme) {
    // Icon from the vendored sprite (web/static/icons.md); the text stays the accessible name.
    var icon = '<svg class="icon" aria-hidden="true"><use href="/static/icons.svg#' +
      (theme === "light" ? "moon" : "sun") + '"></use></svg>';
    var text = theme === "light" ? "Dark mode" : "Light mode";
    if (button.innerHTML !== icon + text) {
      button.innerHTML = icon + text;
    }
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
