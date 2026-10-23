// Row details on the Traffic page (MON-TR-14): a click on a table row opens or closes the detail
// line under it. Open rows are remembered by their stable key, so the two-second live refresh
// re-opens them instead of closing them. A page marks its rows with tr.traffic-row, each followed
// by a tr.traffic-detail line; the detail content is rendered by the server and only shown here.
(function () {
  var open = {};

  function detailOf(row) {
    var next = row.nextElementSibling;
    return next && next.classList && next.classList.contains("traffic-detail") ? next : null;
  }

  function setOpen(row, want) {
    var detail = detailOf(row);
    if (!detail) return;
    detail.hidden = !want;
    row.classList.toggle("open", want);
    var button = row.querySelector(".row-toggle");
    if (button) button.setAttribute("aria-expanded", String(want));
    if (want) {
      // The memory of open rows stays bounded (docs/requirements/DataStore.md §3).
      var keys = Object.keys(open);
      if (keys.length > 255) delete open[keys[0]];
      open[row.getAttribute("data-key")] = true;
    } else {
      delete open[row.getAttribute("data-key")];
    }
  }

  document.addEventListener("click", function (evt) {
    if (!evt.target.closest) return;
    var row = evt.target.closest("tr.traffic-row");
    if (!row) return;
    var detail = detailOf(row);
    if (!detail) return;
    setOpen(row, detail.hidden);
  });

  // The live refresh swaps the polled region; open the remembered rows again in the new content.
  // The event detail's target is the old, already-detached element, so scan the document itself.
  document.addEventListener("htmx:afterSwap", function () {
    document.querySelectorAll("tr.traffic-row").forEach(function (row) {
      if (open[row.getAttribute("data-key")]) setOpen(row, true);
    });
  });
})();