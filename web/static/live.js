// Live refresh pause for the polled regions of a page (MON-OV-11, MON-TR-3). The pause control
// stops the automatic two-second refresh so rows can be read; the filter and range controls still
// work. Pages identify their polled region with an id ending in "-live".
(function () {
  var paused = false;

  // htmx 2.0.4 fires htmx:beforeRequest for every polling round and every manual refresh.
  // Automatic rounds carry no triggeringEvent; preventing default cancels only that round, and
  // the two-second polling continues, so unpausing is instant.
  document.addEventListener("htmx:beforeRequest", function (evt) {
    var d = evt.detail || {};
    if (paused && d.elt && d.elt.id && d.elt.id.slice(-5) === "-live" && !d.triggeringEvent) {
      evt.preventDefault();
    }
  });

  function paint() {
    var button = document.getElementById("pause");
    if (!button) return;
    var icon = '<svg class="icon" aria-hidden="true"><use href="/static/icons.svg#' +
      (paused ? "play" : "pause") + '"></use></svg>';
    var text = paused ? "Resume" : "Pause";
    var title = paused ? "Live refresh is paused; click to resume" : "Pause live refresh";
    if (button.innerHTML !== icon + text || button.title !== title) {
      button.innerHTML = icon + text;
      button.title = title;
    }
    button.setAttribute("aria-pressed", String(paused));
  }

  // The pause button is re-rendered by every swap; repaint it from the state this script keeps.
  document.addEventListener("htmx:afterSwap", function () { paint(); });

  document.addEventListener("click", function (evt) {
    if (!evt.target.closest) return;
    if (evt.target.closest("#pause")) {
      paused = !paused;
      paint();
      return;
    }
    var copy = evt.target.closest("[data-copy]");
    if (copy && navigator.clipboard) {
      navigator.clipboard.writeText(copy.getAttribute("data-copy")).then(function () {
        copy.title = "Copied";
        window.setTimeout(function () { copy.title = "Copy full public key"; }, 1200);
      });
    }
  });

  paint();
})();