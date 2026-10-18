// The sidebar's collapsible section carries the name "sidebar". Which section is
// open belongs to the page being served: the back/forward cache restores a page
// as it was left, which can disagree with the served state; so on first load
// this records the served state of every named section, and on a page restored
// from the cache it puts the served state back.
(function () {
  function serve(d) {
    d.addEventListener("toggle", function () {
      if (!d.open) return;
      document
        .querySelectorAll('details[name="' + d.name + '"]')
        .forEach(function (other) {
          if (other !== d) other.open = false;
        });
    });
  }
  var sections = document.querySelectorAll("details[name]");
  sections.forEach(function (d) {
    d.dataset.servedOpen = d.open ? "1" : "0";
    serve(d);
  });
  window.addEventListener("pageshow", function (e) {
    if (!e.persisted) return;
    sections.forEach(function (d) {
      d.open = d.dataset.servedOpen === "1";
    });
  });
})();