// The advert map's client camera (MON-AD-15, D47): the server draws the whole frame as one SVG and
// opens it on a 100 km view around the centre of the located nodes; this script pans and zooms
// that viewBox inside the frame — mouse or touch drag with momentum, pinch, scroll wheel, and the
// +/−/Reset buttons. The pointer listeners ride the window, so the camera never needs pointer
// capture: a press that stays put must remain an ordinary click on its dot. The camera is kept in
// the module, so the 60 s refresh replays it onto the swapped-in SVG. Every bound is the frame's
// own rectangle: nothing outside it is ever drawn, so the view may hold it, no more.
(function () {
  "use strict";

  var MAX_ZOOM = 16;   // the deepest view, as a multiple of the opening view's side
  var FRICTION = 3.2;  // momentum decay per second (velocity *= e^(-FRICTION·dt))
  var DRAG_PX = 5;     // client px a press may travel before it counts as a pan, not a click

  var initialised = new WeakSet();
  var savedCamera = null; // the camera of the last map, so a live refresh keeps the view

  function clamp(value, low, high) { return Math.min(Math.max(value, low), high); }

  function readView(attribute) {
    var parts = (attribute || "").split(/[\s,]+/).map(Number);
    return { x: parts[0] || 0, y: parts[1] || 0, w: parts[2] || 0, h: parts[3] || 0 };
  }

  function initState(svg) {
    var opening = readView(svg.getAttribute("viewBox"));
    var frame = {
      w: Number(svg.getAttribute("data-frame-width")) || opening.w,
      h: Number(svg.getAttribute("data-frame-height")) || opening.h,
    };
    var state = {
      svg: svg, frame: frame, opening: opening,
      view: { x: opening.x, y: opening.y, w: opening.w, h: opening.h },
      // Zoom-out bound: the smallest zoom that still holds the whole frame; if the opening view
      // is wider than the frame (no located nodes), it is already that bound.
      min: Math.min(1, opening.w / frame.w, opening.h / frame.h),
      pointers: new Map(), drag: null, pinch: null, glide: 0, moved: 0,
    };
    // A swap may redraw the map: replay the camera only when the frame it rode on is the same.
    if (savedCamera && Math.abs(savedCamera.frame.w - frame.w) < 1 && Math.abs(savedCamera.frame.h - frame.h) < 1) {
      state.view = { x: savedCamera.x, y: savedCamera.y, w: savedCamera.w, h: savedCamera.h };
    }
    return state;
  }

  // client px per user-unit: the SVG letterboxes to preserve the viewBox's own shape (meet).
  function userPerPx(state) {
    var rect = state.svg.getBoundingClientRect();
    var drawn = Math.min(rect.width / state.view.w, rect.height / state.view.h);
    return drawn > 0 ? 1 / drawn : 1;
  }

  // The zoomed-out bound letterboxes the frame inside the view: centre beyond it, free inside.
  function apply(state) {
    var v = state.view, f = state.frame;
    v.x = v.w >= f.w ? (f.w - v.w) / 2 : clamp(v.x, 0, f.w - v.w);
    v.y = v.h >= f.h ? (f.h - v.h) / 2 : clamp(v.y, 0, f.h - v.h);
    state.svg.setAttribute("viewBox", v.x.toFixed(2) + " " + v.y.toFixed(2) + " " + v.w.toFixed(2) + " " + v.h.toFixed(2));
    savedCamera = { x: v.x, y: v.y, w: v.w, h: v.h, frame: { w: state.frame.w, h: state.frame.h } };
    var zoom = state.opening.w / v.w;
    var box = state.svg.closest(".advert-map-box");
    if (box) {
      var minus = box.querySelector('[data-map-zoom="out"]');
      var plus = box.querySelector('[data-map-zoom="in"]');
      if (plus) plus.disabled = zoom >= MAX_ZOOM - 1e-6;
      if (minus) minus.disabled = zoom <= state.min + 1e-6;
    }
    // Cards beside the map sit where their dots are; each camera move re-places the open ones.
    state.svg.dispatchEvent(new CustomEvent("advertsmapview", { bubbles: true }));
  }

  function stopGlide(state) {
    if (state.glide) cancelAnimationFrame(state.glide);
    state.glide = 0;
  }

  // Zoom by factor around a point in user units (the viewBox's own coordinates).
  function zoomAround(state, at, factor) {
    if (!at) return;
    var target = clamp(state.opening.w / state.view.w * factor, state.min, MAX_ZOOM);
    var w = state.opening.w / target, h = state.opening.h / target;
    state.view.x = at.x - (at.x - state.view.x) * (w / state.view.w);
    state.view.y = at.y - (at.y - state.view.y) * (h / state.view.h);
    state.view.w = w;
    state.view.h = h;
    apply(state);
  }

  function toUser(state, clientX, clientY) {
    var matrix = state.svg.getScreenCTM();
    if (!matrix) return null;
    var p = new DOMPoint(clientX, clientY).matrixTransform(matrix.inverse());
    return { x: p.x, y: p.y };
  }

  // A pressed pointer's newest 100 ms of travel names the flick's velocity.
  function flick(state) {
    var samples = state.drag ? state.drag.samples : [];
    if (samples.length < 2) return { vx: 0, vy: 0 };
    var last = samples[samples.length - 1];
    var first = null;
    for (var i = samples.length - 1; i >= 0; i--) {
      first = samples[i];
      if (last.t - first.t >= 100) break;
    }
    var dt = last.t - first.t;
    if (dt <= 0) return { vx: 0, vy: 0 };
    var perPx = userPerPx(state);
    return { vx: (last.x - first.x) * perPx / dt, vy: (last.y - first.y) * perPx / dt };
  }

  function glide(state, vx, vy) {
    stopGlide(state);
    if (window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    var last = performance.now();
    var step = function (now) {
      var dt = Math.min((now - last) / 1000, 0.05);
      last = now;
      state.view.x += vx * dt * 1000;
      state.view.y += vy * dt * 1000;
      var before = { x: state.view.x, y: state.view.y };
      apply(state);
      // A wall ate the pan: stop that axis rather than pressing against the frame edge.
      if (before.x !== state.view.x) vx = 0;
      if (before.y !== state.view.y) vy = 0;
      var decay = Math.exp(-FRICTION * dt);
      vx *= decay;
      vy *= decay;
      if (Math.abs(vx) > 0.01 || Math.abs(vy) > 0.01) {
        state.glide = requestAnimationFrame(step);
      } else {
        state.glide = 0;
      }
    };
    state.glide = requestAnimationFrame(step);
  }

  function pointersList(state) {
    var points = [];
    state.pointers.forEach(function (p) { points.push(p); });
    return points;
  }

  function pinchGeometry(a, b) {
    return {
      d: Math.hypot(b.x - a.x, b.y - a.y) || 1,
      x: (a.x + b.x) / 2, y: (a.y + b.y) / 2,
    };
  }

  function init(svg) {
    if (initialised.has(svg)) return;
    initialised.add(svg);
    var state = initState(svg);
    apply(state);

    // A press on the map itself: register it and watch it on the window, so a drag may leave the
    // SVG without losing the pointer. Nothing is captured, so a still press clicks as before.
    state.svg.addEventListener("pointerdown", function (evt) {
      if (evt.pointerType === "mouse" && evt.button !== 0) return;
      stopGlide(state);
      state.pointers.set(evt.pointerId, { x: evt.clientX, y: evt.clientY });
      state.moved = 0;
      var pair = pointersList(state);
      if (pair.length >= 2) {
        state.drag = null;
        state.pinch = pinchGeometry(pair[0], pair[1]);
      } else {
        state.drag = { lastX: evt.clientX, lastY: evt.clientY, samples: [{ t: performance.now(), x: evt.clientX, y: evt.clientY }] };
      }
    });

    window.addEventListener("pointermove", function (evt) {
      if (!state.pointers.has(evt.pointerId)) return;
      state.pointers.set(evt.pointerId, { x: evt.clientX, y: evt.clientY });
      var pair = pointersList(state);
      if (pair.length >= 2) {
        // Pinch: pan by the centre's travel, then scale by the pointer distance, keeping the
        // content under that centre. Incremental, so each move re-anchors on the present camera.
        var now = pinchGeometry(pair[0], pair[1]);
        if (state.pinch) {
          var perPx = userPerPx(state);
          state.view.x -= (now.x - state.pinch.x) * perPx;
          state.view.y -= (now.y - state.pinch.y) * perPx;
          zoomAround(state, toUser(state, now.x, now.y), clamp(now.d / state.pinch.d, 0.25, 4));
        }
        state.pinch = now;
        state.drag = null;
        return;
      }
      if (!state.drag) { state.pinch = null; return; }
      var drag = state.drag;
      state.moved += Math.hypot(evt.clientX - drag.lastX, evt.clientY - drag.lastY);
      var units = userPerPx(state);
      state.view.x -= (evt.clientX - drag.lastX) * units;
      state.view.y -= (evt.clientY - drag.lastY) * units;
      apply(state);
      drag.samples.push({ t: performance.now(), x: evt.clientX, y: evt.clientY });
      if (drag.samples.length > 8) drag.samples.shift();
      drag.lastX = evt.clientX;
      drag.lastY = evt.clientY;
    });
    var release = function (evt) {
      if (!state.pointers.delete(evt.pointerId)) return;
      var pair = pointersList(state);
      if (pair.length >= 2) {
        state.pinch = pinchGeometry(pair[0], pair[1]); // re-base on the pointers that remain
        return;
      }
      if (pair.length === 1) { // from a pinch back to a plain drag
        state.pinch = null;
        state.drag = { lastX: pair[0].x, lastY: pair[0].y, samples: [{ t: performance.now(), x: pair[0].x, y: pair[0].y }] };
        return;
      }
      if (state.drag) {
        var velocity = flick(state);
        state.drag = null;
        state.pinch = null;
        if (state.moved > DRAG_PX && (Math.abs(velocity.vx) > 0.05 || Math.abs(velocity.vy) > 0.05)) {
          glide(state, velocity.vx, velocity.vy);
        }
      }
    };
    window.addEventListener("pointerup", release);
    window.addEventListener("pointercancel", release);

    // A pan is not a click on a dot: a gesture past the threshold swallows the one click that
    // follows. The distance survives until that click, because it comes after the pointer rises.
    state.svg.addEventListener("click", function (evt) {
      if (state.moved > DRAG_PX) {
        evt.stopPropagation();
        evt.preventDefault();
      }
      state.moved = 0; // a later, genuine click must not pay for this drag
    }, true);

    state.svg.addEventListener("wheel", function (evt) {
      evt.preventDefault();
      stopGlide(state);
      // deltaMode 1 counts lines, 2 pages; 300 divides a comfortable notch into about a half step.
      var px = evt.deltaY * (evt.deltaMode === 1 ? 33 : evt.deltaMode === 2 ? 400 : 1);
      zoomAround(state, toUser(state, evt.clientX, evt.clientY), clamp(Math.pow(2, -px / 300), 0.25, 4));
    }, { passive: false });

    var box = state.svg.closest(".advert-map-box");
    if (!box) return;
    var center = function () {
      return { x: state.view.x + state.view.w / 2, y: state.view.y + state.view.h / 2 };
    };
    var plus = box.querySelector('[data-map-zoom="in"]');
    var minus = box.querySelector('[data-map-zoom="out"]');
    var reset = box.querySelector("[data-map-reset]");
    if (plus) plus.addEventListener("click", function () {
      stopGlide(state);
      zoomAround(state, center(), 2);
    });
    if (minus) minus.addEventListener("click", function () {
      stopGlide(state);
      zoomAround(state, center(), 0.5);
    });
    if (reset) reset.addEventListener("click", function () {
      stopGlide(state);
      state.view = { x: state.opening.x, y: state.opening.y, w: state.opening.w, h: state.opening.h };
      apply(state);
    });
  }

  function scan() {
    document.querySelectorAll(".advert-map").forEach(init);
  }

  scan();
  document.addEventListener("DOMContentLoaded", scan);
  document.addEventListener("htmx:afterSwap", scan);
})();
