// Advert map cards (MON-AD-16): the map's SVG carries numbers only — its dots identify themselves
// by row number — so hover, focus and click reveal the server-escaped cards that sit beside the
// map. A mouseover opens the mini card; a click or Enter pins the full card, and any other click
// or Escape un-pins. The listeners are delegated, so the one-minute live refresh rebinds nothing
// (MON-AD-13).
(function () {
  "use strict";

  function cardFor(dot) {
    var box = dot.closest(".advert-map-box");
    var card = box && box.querySelector('.map-card[data-node="' + dot.getAttribute("data-n") + '"]');
    return { box: box, card: card };
  }

  // The SVG scales with its box, so the dot's pixel spot is measured against the box each time.
  function place(dot, card) {
    var dotRect = dot.getBoundingClientRect();
    // A clustered dot is hidden; its card has no place to sit until the cluster fans out.
    if (!dotRect.width && !dotRect.height) {
      card.classList.remove("pinned");
      card.hidden = true;
      return;
    }
    var boxRect = dot.closest(".advert-map-box").getBoundingClientRect();
    var x = dotRect.left - boxRect.left + dotRect.width / 2;
    var y = dotRect.top - boxRect.top;
    card.hidden = false;
    var width = card.offsetWidth || 220;
    var left = x + 10;
    if (left + width > boxRect.width - 6) left = Math.max(6, x - width - 10);
    var top = Math.min(Math.max(6, y - 8), boxRect.height - card.offsetHeight - 6);
    card.style.left = left + "px";
    card.style.top = Math.max(6, top) + "px";
  }

  function unpin() {
    document.querySelectorAll(".map-card.pinned").forEach(function (card) {
      card.classList.remove("pinned");
      card.hidden = true;
    });
  }

  function open(dot, pinned) {
    var found = cardFor(dot);
    if (!found.card) return;
    if (pinned) {
      found.box.querySelectorAll(".map-card.pinned").forEach(function (card) {
        if (card !== found.card) {
          card.classList.remove("pinned");
          card.hidden = true;
        }
      });
      found.card.classList.add("pinned");
    }
    place(dot, found.card);
  }

  function closeUnlessPinned(dot) {
    var found = cardFor(dot);
    if (found.card && !found.card.classList.contains("pinned")) found.card.hidden = true;
  }

  // The map's client camera (adverts-map.js) moves the dots inside their SVG; every move re-places
  // the cards it leaves open, so a pinned card follows its dot through pan and zoom (MON-AD-16).
  document.addEventListener("advertsmapview", function (evt) {
    var svg = evt.target && evt.target.closest ? evt.target.closest(".advert-map") : null;
    if (!svg) return;
    var box = svg.closest(".advert-map-box");
    if (!box) return;
    box.querySelectorAll(".map-card:not([hidden])").forEach(function (card) {
      var node = svg.querySelector('[data-n="' + card.getAttribute("data-node") + '"]');
      if (node) place(node, card);
    });
  });

  var dot = function (target) {
    return target && target.closest ? target.closest(".map-node") : null;
  };

  document.addEventListener("mouseover", function (evt) {
    var node = dot(evt.target);
    if (node) open(node, false);
  });
  document.addEventListener("mouseout", function (evt) {
    var node = dot(evt.target);
    if (node) closeUnlessPinned(node);
  });
  document.addEventListener("focusin", function (evt) {
    var node = dot(evt.target);
    if (node) open(node, false);
  });
  document.addEventListener("focusout", function (evt) {
    // Focus moving to a card's own link must keep it; the card shows only near its dot.
    var node = dot(evt.target);
    if (node) setTimeout(function () { closeUnlessPinned(node); }, 0);
  });

  document.addEventListener("click", function (evt) {
    var node = dot(evt.target);
    if (!node) {
      if (evt.target.closest && !evt.target.closest(".map-card")) unpin();
      return;
    }
    var found = cardFor(node);
    if (!found.card) return;
    if (found.card.classList.contains("pinned")) {
      found.card.classList.remove("pinned");
      found.card.hidden = true;
      return;
    }
    open(node, true);
  });

  document.addEventListener("keydown", function (evt) {
    if (evt.key === "Escape") unpin();
  });
})();
