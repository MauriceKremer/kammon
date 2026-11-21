// Live refresh pause for the polled regions of a page (MON-OV-11, MON-TR-3, MON-CH-14). The
// overview page refreshes in five independent regions, the traffic page in one and the channels
// page in its metric and message strips; every polled region is either an id ending in "-live"
// or carries data-live. The pause control sits in static markup, so it is never re-rendered by a
// swap, and it stops the automatic rounds so rows can be read; filters and manual actions still
// work. Resuming refreshes the regions at once instead of waiting for the next timer tick.
(function () {
  var paused = false;

  function isLive(elt) {
    if (!elt || !elt.hasAttribute) return false;
    if (elt.hasAttribute("data-live")) return true;
    return !!elt.id && elt.id.slice(-5) === "-live";
  }

  // htmx 2.0.4 fires htmx:beforeRequest for every region's polling round and every manual refresh.
  // Automatic rounds carry no triggeringEvent; preventing default cancels only that round, and
  // the polling continues, so unpausing is instant.
  document.addEventListener("htmx:beforeRequest", function (evt) {
    var d = evt.detail || {};
    if (paused && !d.triggeringEvent && isLive(d.elt)) {
      evt.preventDefault();
    }
  });

  // A filter form marked data-apply-on-blur applies itself as soon as one of its controls commits:
  // a text field when it loses focus, a select or checkbox at once. requestSubmit keeps htmx's
  // submit handling when the form is an htmx form.
  document.addEventListener("change", function (evt) {
    var form = evt.target && evt.target.form;
    if (!form || !form.hasAttribute("data-apply-on-blur")) return;
    if (form.requestSubmit) {
      form.requestSubmit();
    } else {
      form.submit();
    }
  });

  function refreshLive() {
    if (!window.htmx || !window.htmx.ajax) return;
    var regions = document.querySelectorAll("[data-live], [id$='-live']");
    for (var i = 0; i < regions.length; i++) {
      var url = regions[i].getAttribute("hx-get");
      if (url) window.htmx.ajax("GET", url, {target: regions[i], swap: "outerHTML"});
    }
  }

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

  document.addEventListener("click", function (evt) {
    if (!evt.target.closest) return;
    if (evt.target.closest("#pause")) {
      paused = !paused;
      paint();
      if (!paused) refreshLive();
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
