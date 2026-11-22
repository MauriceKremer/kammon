// Advert map cards (MON-AD-16, D57): MapLibre draws the dots in a canvas, so the editable cards stay
// as server-escaped HTML beside the map. adverts-map.js announces a dot under the pointer or a
// picked dot with an event carrying its screen spot; this script shows and places the matching
// card. The listeners are delegated, so the one-minute live refresh rebinds nothing (MON-AD-13).
(function () {
  "use strict";

  var pinned = new WeakMap(); // map box -> the row number whose full card is pinned

  function boxOf(target) {
    return target && target.closest ? target.closest(".advert-map-box") : null;
  }

  function cardFor(box, number) {
    if (!box || number == null) return null;
    return box.querySelector('.map-card[data-node="' + number + '"]');
  }

  function hide(card) {
    if (!card) return;
    card.classList.remove("pinned");
    card.hidden = true;
  }

  function place(box, card, x, y) {
    if (!card) return;
    var width = card.offsetWidth || 220;
    var left = x + 12;
    if (left + width > box.clientWidth - 6) left = Math.max(6, x - width - 12);
    var top = Math.min(Math.max(6, y - 8), Math.max(6, box.clientHeight - card.offsetHeight - 6));
    card.style.left = left + "px";
    card.style.top = top + "px";
  }

  function show(box, number, x, y, pin) {
    var card = cardFor(box, number);
    if (!card) return;
    if (pin) {
      box.querySelectorAll(".map-card").forEach(function (other) {
        if (other !== card) hide(other);
      });
      card.classList.add("pinned");
      pinned.set(box, number);
    }
    card.hidden = false;
    place(box, card, x, y);
  }

  function unpin(box) {
    box.querySelectorAll(".map-card").forEach(hide);
    pinned.delete(box);
  }

  function unpinAll() {
    document.querySelectorAll(".advert-map-box").forEach(unpin);
  }

  // A dot under the pointer: the mini card follows it. A pinned card outranks the hover.
  document.addEventListener("advertsmaphover", function (evt) {
    var box = boxOf(evt.target);
    if (!box || pinned.has(box)) return;
    if (evt.detail.number == null) {
      box.querySelectorAll(".map-card").forEach(hide);
      return;
    }
    show(box, evt.detail.number, evt.detail.x, evt.detail.y, false);
  });

  // A picked dot: pin its full card, one at a time.
  document.addEventListener("advertsmappin", function (evt) {
    var box = boxOf(evt.target);
    if (!box) return;
    show(box, evt.detail.number, evt.detail.x, evt.detail.y, true);
  });

  // The camera moved: a pinned card follows its dot (its coordinates are re-projected by
  // adverts-map.js, which sends the new screen spot).
  document.addEventListener("advertsmapposition", function (evt) {
    var box = boxOf(evt.target);
    if (!box) return;
    var number = pinned.get(box);
    if (number == null || String(number) !== String(evt.detail.number)) return;
    place(box, cardFor(box, number), evt.detail.x, evt.detail.y);
  });

  document.addEventListener("advertsmapunpin", function (evt) {
    var box = boxOf(evt.target);
    if (box) unpin(box);
  });

  // Spiderfying hides the stacked dots; its cards must close without clearing the spider itself.
  document.addEventListener("advertsmapclosecards", function (evt) {
    var box = boxOf(evt.target);
    if (box) unpin(box);
  });

  // Clicking the page away from a card closes it; a map click is decided by adverts-map.js, which
  // knows whether it hit a dot or empty ground.
  document.addEventListener("click", function (evt) {
    if (evt.target.closest && evt.target.closest(".map-card")) return;
    if (evt.target.closest && evt.target.closest(".advert-map")) return;
    unpinAll();
  });

  document.addEventListener("keydown", function (evt) {
    if (evt.key !== "Escape") return;
    document.querySelectorAll(".map-card.pinned").forEach(function (card) {
      var box = card.closest(".advert-map-box");
      if (box) box.dispatchEvent(new CustomEvent("advertsmapunpin", { bubbles: true }));
    });
    unpinAll();
  });

  // Copy Public Key (D53): the same clipboard fallback as before, so plain-HTTP LAN pages work.
  function copyKey(text, button) {
    var done = function () {
      button.textContent = "Copied";
      button.disabled = true;
      setTimeout(function () {
        button.textContent = "Copy Public Key";
        button.disabled = false;
      }, 1400);
    };
    var fallback = function () {
      var area = document.createElement("textarea");
      area.value = text;
      area.setAttribute("readonly", "");
      area.style.position = "fixed";
      area.style.opacity = "0";
      document.body.appendChild(area);
      area.select();
      try {
        if (document.execCommand("copy")) done();
      } catch (ignore) {}
      area.remove();
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, fallback);
      return;
    }
    fallback();
  }

  document.addEventListener("click", function (evt) {
    var copy = evt.target.closest ? evt.target.closest("[data-copy-key]") : null;
    if (copy) {
      evt.preventDefault();
      copyKey(copy.getAttribute("data-copy-key"), copy);
      return;
    }
    // Draw Line to Base is a camera action: adverts-map.js owns the line layer and the button state.
    var draw = evt.target.closest ? evt.target.closest("[data-draw-base]") : null;
    if (draw) {
      evt.preventDefault();
      var box = draw.closest(".advert-map-box");
      if (box) {
        box.dispatchEvent(new CustomEvent("advertsmapline", {
          bubbles: true,
          detail: { number: draw.getAttribute("data-draw-base") },
        }));
      }
    }
  });
})();
