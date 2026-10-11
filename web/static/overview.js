// Live refresh pause for the Overview (MON-OV-11). The pause control stops the automatic
// two-second refresh of the metric strip, chart and recent packets; the range switch still works.
(function () {
  var paused = false;

  // htmx 2.0.4 fires htmx:beforeRequest for every round and every manual refresh. Automatic
  // rounds have no triggeringEvent; preventing default cancels only that round, and the
  // two-second polling continues, so unpausing is instant.
  document.addEventListener("htmx:beforeRequest", function (evt) {
    var d = evt.detail || {};
    if (paused && d.elt && d.elt.id === "overview-live" && !d.triggeringEvent) {
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
    if (!evt.target.closest || !evt.target.closest("#pause")) return;
    paused = !paused;
    paint();
  });

  paint();
})();