// The advert map's client camera (MON-AD-15, D50): the server draws the whole frame as one SVG and
// opens it on a 100 km view around the configured base node (the located nodes' centre when there
// is no `location`); this script pans and zooms that viewBox inside the frame — mouse or touch drag
// with momentum, pinch, scroll wheel, and the +/−/Reset buttons. The pointer listeners ride the
// window, so the camera never needs pointer capture: a press that stays put must remain an ordinary
// click on its dot. The camera is kept in the module, so the 60 s refresh replays it onto the
// swapped-in SVG. Every bound is the frame's own rectangle: nothing outside it is ever drawn, so the
// view may hold it, no more.
//
// The same script groups nearby dots into numbered cluster badges (MON-AD-17), keeps the badges and
// spider legs behind the dots, fans overlapping dots out with a smooth spiral at the deepest zoom
// (D52), filters layers through the chips, and runs the inspector card's copy and base-line actions
// (D53).
(function () {
  "use strict";

  var MAX_ZOOM = 16;   // the deepest view, as a multiple of the opening view's side
  var FRICTION = 3.2;  // momentum decay per second (velocity *= e^(-FRICTION·dt))
  var DRAG_PX = 5;     // client px a press may travel before it counts as a pan, not a click

  var MARKER_PX = 4.5;      // dot radius in screen px, so zoom does not swell the markers
  var CITY_LABEL_PX = 9.5;  // city name size in screen px
  var CITY_DOT_PX = 1.8;    // city marker radius in screen px
  var CITY_HALO_PX = 2.4;   // city name halo width in screen px
  var CLUSTER_PX = 42;      // dots nearer than this on screen group into a badge
  var SPIDER_PX = 24;       // at the deepest zoom, dots nearer than this fan out
  var BADGE_PX = 12;        // cluster badge radius on screen
  var SPIDER_STEP_PX = 26;  // spiral ring spacing on screen
  var GOLDEN_ANGLE = 2.399963229728653;

  var SVG_NS = "http://www.w3.org/2000/svg";

  var initialised = new WeakSet();
  var savedCamera = null; // the camera of the last map, so a live refresh keeps the view

  function clamp(value, low, high) { return Math.min(Math.max(value, low), high); }

  function reducedMotion() {
    return window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  }

  function svgElement(name) {
    return document.createElementNS(SVG_NS, name);
  }

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
      anim: 0, clusterQueued: false, nodes: [], clusterLayer: null, lineLayer: null,
      home: null, spiders: new Map(), spiderFrame: 0, baseLines: new Map(),
      filters: { advertised: true, silent: false, base: true },
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
    updateCityLabels(state);
    updateHomeMarker(state);
    scheduleCluster(state);
  }

  // City names stay subtle and legible: one screen size at every zoom, with the lower Natural Earth
  // ranks hidden on the wide opening view and revealed as the camera closes in (D51).
  function updateCityLabels(state) {
    var upp = userPerPx(state);
    if (!isFinite(upp) || upp <= 0) return;
    var zoom = state.opening.w / state.view.w;
    var maxRank = zoom >= 4 ? 99 : zoom >= 2.5 ? 9 : zoom >= 1.7 ? 8 : 7;
    var size = (CITY_LABEL_PX * upp).toFixed(3) + "px";
    var halo = (CITY_HALO_PX * upp).toFixed(3) + "px";
    var radius = (CITY_DOT_PX * upp).toFixed(3);
    state.svg.querySelectorAll(".map-city-group").forEach(function (group) {
      var label = group.querySelector(".map-city-name");
      var dot = group.querySelector(".map-city");
      if (!label) return;
      label.style.fontSize = size;
      label.style.strokeWidth = halo;
      group.classList.toggle("city-hidden", (Number(group.getAttribute("data-rank")) || 0) > maxRank);
      if (dot) dot.setAttribute("r", radius);
    });
  }

  function readNodes(state) {
    state.nodes = Array.prototype.map.call(state.svg.querySelectorAll(".map-node"), function (el) {
      var activity = "";
      ["fresh", "recent", "older", "stale", "unknown"].forEach(function (name) {
        if (el.classList.contains(name)) activity = name;
      });
      return {
        el: el,
        number: Number(el.getAttribute("data-n")) || 0,
        x: parseFloat(el.getAttribute("cx")) || 0,
        y: parseFloat(el.getAttribute("cy")) || 0,
        activity: activity,
        silent: activity === "stale",
      };
    });
  }

  function readHome(state) {
    var el = state.svg.querySelector(".map-home");
    state.home = el ? {
      el: el,
      x: parseFloat(el.getAttribute("data-x")) || 0,
      y: parseFloat(el.getAttribute("data-y")) || 0,
    } : null;
  }

  // Layers below the dots: cluster badges and the spider legs/base lines. Keeping them behind the
  // node circles means a badge never covers a standard marker, and hidden members truly vanish (D52).
  function ensureClusterLayer(state) {
    var anchor = (state.home && state.home.el) || state.svg.querySelector(".map-home") || state.svg.querySelector(".map-node");
    state.lineLayer = svgElement("g");
    state.lineLayer.setAttribute("class", "map-lines");
    state.clusterLayer = svgElement("g");
    state.clusterLayer.setAttribute("class", "map-clusters");
    if (anchor) {
      state.svg.insertBefore(state.lineLayer, anchor);
      state.svg.insertBefore(state.clusterLayer, anchor);
    } else {
      state.svg.appendChild(state.lineLayer);
      state.svg.appendChild(state.clusterLayer);
    }
  }

  function updateHomeMarker(state) {
    if (!state.home) return;
    var upp = userPerPx(state);
    if (!isFinite(upp) || upp <= 0) return;
    state.home.el.setAttribute("transform", "translate(" + state.home.x.toFixed(2) + " " + state.home.y.toFixed(2) + ") scale(" + upp.toFixed(4) + ")");
  }

  function clearLayer(state) {
    var layer = state.clusterLayer;
    while (layer && layer.firstChild) layer.removeChild(layer.firstChild);
  }

  function copyKey(text, button) {
    var done = function () {
      button.textContent = "Copied";
      button.disabled = true;
      setTimeout(function () {
        button.textContent = "Copy Public Key";
        button.disabled = false;
      }, 1400);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () { fallbackCopy(text, done); });
      return;
    }
    fallbackCopy(text, done);
  }

  function fallbackCopy(text, done) {
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
  }

  // One line at a time, so the map never fills with a web of base links (D53).
  function toggleBaseLine(state, box, button) {
    var number = button.getAttribute("data-draw-base");
    var active = state.baseLines.has(number);
    state.baseLines.forEach(function (line) { line.remove(); });
    state.baseLines.clear();
    box.querySelectorAll("[data-draw-base]").forEach(function (other) {
      other.setAttribute("aria-pressed", "false");
      other.textContent = "Draw Line to Base";
    });
    if (active || !state.home) return;
    var node = state.nodes.filter(function (candidate) { return String(candidate.number) === number; })[0];
    if (!node) return;
    var line = svgElement("line");
    line.setAttribute("class", "map-base-line");
    line.setAttribute("x1", state.home.x);
    line.setAttribute("y1", state.home.y);
    line.setAttribute("x2", node.x);
    line.setAttribute("y2", node.y);
    state.lineLayer.appendChild(line);
    state.baseLines.set(number, line);
    button.setAttribute("aria-pressed", "true");
    button.textContent = "Hide Line to Base";
  }

  // Union-find over a screen-sized grid: only nodes in neighbouring cells can be near each other,
  // so a thousand dots still cluster in linear time rather than a million pair checks.
  function clusterGroups(nodes, cell, near) {
    var count = nodes.length;
    var parent = new Array(count);
    for (var i = 0; i < count; i++) parent[i] = i;
    function find(index) {
      while (parent[index] !== index) {
        parent[index] = parent[parent[index]];
        index = parent[index];
      }
      return index;
    }
    function union(a, b) {
      a = find(a);
      b = find(b);
      if (a !== b) parent[b] = a;
    }
    cell = Math.max(cell, 1e-9);
    var buckets = new Map();
    nodes.forEach(function (node, index) {
      var key = Math.floor(node.x / cell) + ":" + Math.floor(node.y / cell);
      var list = buckets.get(key);
      if (!list) {
        list = [];
        buckets.set(key, list);
      }
      list.push(index);
    });
    nodes.forEach(function (node, index) {
      var gx = Math.floor(node.x / cell), gy = Math.floor(node.y / cell);
      for (var dx = -1; dx <= 1; dx++) {
        for (var dy = -1; dy <= 1; dy++) {
          var list = buckets.get((gx + dx) + ":" + (gy + dy));
          if (!list) continue;
          for (var k = 0; k < list.length; k++) {
            var other = list[k];
            if (other <= index) continue;
            if (near(node, nodes[other])) union(index, other);
          }
        }
      }
    });
    var groups = new Map();
    for (var i = 0; i < count; i++) {
      var root = find(i);
      var group = groups.get(root);
      if (!group) {
        group = [];
        groups.set(root, group);
      }
      group.push(nodes[i]);
    }
    return Array.from(groups.values());
  }

  function centroid(group) {
    var x = 0, y = 0;
    group.forEach(function (node) { x += node.x; y += node.y; });
    return { x: x / group.length, y: y / group.length };
  }

  // A badge is a numbers-only SVG group: a disc, its count and a larger transparent hit area.
  function addBadge(state, group, upp) {
    var at = centroid(group);
    var badge = svgElement("g");
    badge.setAttribute("class", "map-cluster");
    badge.setAttribute("tabindex", "0");
    badge.setAttribute("role", "button");
    badge.setAttribute("aria-label", group.length + " nodes here; activate to zoom in");
    badge.__nodes = group;

    var hit = svgElement("circle");
    hit.setAttribute("class", "map-cluster-hit");
    hit.setAttribute("cx", at.x);
    hit.setAttribute("cy", at.y);
    hit.setAttribute("r", (BADGE_PX + 6) * upp);

    var disc = svgElement("circle");
    disc.setAttribute("class", "map-cluster-disc");
    disc.setAttribute("cx", at.x);
    disc.setAttribute("cy", at.y);
    disc.setAttribute("r", BADGE_PX * upp);

    var label = svgElement("text");
    label.setAttribute("class", "map-cluster-count");
    label.setAttribute("x", at.x);
    label.setAttribute("y", at.y);
    label.setAttribute("font-size", 11 * upp);
    label.textContent = group.length > 99 ? "99+" : String(group.length);

    badge.appendChild(hit);
    badge.appendChild(disc);
    badge.appendChild(label);
    state.clusterLayer.appendChild(badge);
  }

  // Spiderfy: at the deepest zoom, overlapping dots glide out along a spiral and keep a thin leg
  // home. The offsets ease into place, so a cluster click fans out instead of snapping (D52).
  function setSpider(state, group, upp) {
    var at = centroid(group);
    var step = SPIDER_STEP_PX * upp;
    group.forEach(function (node, index) {
      var angle = index * GOLDEN_ANGLE;
      var radius = step * Math.sqrt(index + 1);
      var entry = state.spiders.get(node.el);
      if (!entry) {
        entry = { node: node, dx: 0, dy: 0, tx: 0, ty: 0, ax: at.x, ay: at.y, leg: null };
        state.spiders.set(node.el, entry);
      }
      entry.tx = Math.cos(angle) * radius;
      entry.ty = Math.sin(angle) * radius;
      entry.ax = at.x;
      entry.ay = at.y;
      if (!entry.leg) {
        var leg = svgElement("line");
        leg.setAttribute("class", "map-spider");
        entry.leg = leg;
        state.lineLayer.appendChild(leg);
        requestAnimationFrame(function () { leg.classList.add("visible"); });
      }
    });
    animateSpiders(state);
  }

  function removeSpider(state, el) {
    var entry = state.spiders.get(el);
    if (!entry) return;
    if (entry.leg) entry.leg.remove();
    entry.node.el.removeAttribute("transform");
    state.spiders.delete(el);
    if (!state.spiders.size) stopSpiderFrame(state);
  }

  function clearSpiders(state) {
    Array.from(state.spiders.keys()).forEach(function (el) { removeSpider(state, el); });
  }

  function stopSpiderFrame(state) {
    if (state.spiderFrame) cancelAnimationFrame(state.spiderFrame);
    state.spiderFrame = 0;
  }

  function animateSpiders(state) {
    if (state.spiderFrame) return;
    var last = performance.now();
    var step = function () {
      var now = performance.now();
      var dt = Math.min(Math.max((now - last) / 1000, 0), 0.05);
      last = now;
      var ease = 1 - Math.exp(-16 * dt);
      var moving = false;
      state.spiders.forEach(function (entry) {
        entry.dx += (entry.tx - entry.dx) * ease;
        entry.dy += (entry.ty - entry.dy) * ease;
        entry.node.el.setAttribute("transform", "translate(" + entry.dx.toFixed(3) + " " + entry.dy.toFixed(3) + ")");
        if (entry.leg) {
          entry.leg.setAttribute("x1", entry.ax);
          entry.leg.setAttribute("y1", entry.ay);
          entry.leg.setAttribute("x2", entry.ax + entry.dx);
          entry.leg.setAttribute("y2", entry.ay + entry.dy);
        }
        var baseLine = state.baseLines.get(String(entry.node.number));
        if (baseLine) {
          baseLine.setAttribute("x2", entry.node.x + entry.dx);
          baseLine.setAttribute("y2", entry.node.y + entry.dy);
        }
        if (Math.abs(entry.tx - entry.dx) > 0.005 || Math.abs(entry.ty - entry.dy) > 0.005) moving = true;
      });
      if (moving) {
        state.spiderFrame = requestAnimationFrame(step);
      } else {
        state.spiderFrame = 0;
      }
    };
    state.spiderFrame = requestAnimationFrame(step);
  }

  function clusterNow(state) {
    var rect = state.svg.getBoundingClientRect();
    if (!rect.width || !rect.height) return; // the map is not laid out; wait for a resize
    var upp = userPerPx(state);
    if (!isFinite(upp) || upp <= 0) return;

    // Markers and badges keep one screen size while the viewBox moves, so zooming reveals
    // geography instead of inflating the dots. Filtered and clustered dots are removed from the
    // canvas rather than left under a badge (D52).
    var marker = MARKER_PX * upp;
    state.nodes.forEach(function (node) {
      node.el.setAttribute("r", marker.toFixed(3));
      node.el.classList.remove("clustered");
      var visible = node.silent ? state.filters.silent : state.filters.advertised;
      node.el.classList.toggle("filtered", !visible);
      node.el.style.display = visible ? "" : "none"; // inline too, so no badge ever sits on a dot
      if (!state.spiders.has(node.el)) node.el.removeAttribute("transform");
    });
    clearLayer(state);
    if (!state.nodes.length) {
      clearSpiders(state);
      return;
    }

    var active = state.nodes.filter(function (node) {
      return node.silent ? state.filters.silent : state.filters.advertised;
    });
    var zoom = state.opening.w / state.view.w;
    if (zoom >= MAX_ZOOM - 0.02) {
      var spider = SPIDER_PX * upp;
      var grouped = new Set();
      clusterGroups(active, spider, function (a, b) {
        return Math.hypot(a.x - b.x, a.y - b.y) <= spider;
      }).forEach(function (group) {
        if (group.length < 2) return;
        group.forEach(function (node) { grouped.add(node); });
        setSpider(state, group, upp);
      });
      Array.from(state.spiders.keys()).forEach(function (el) {
        if (!grouped.has(state.spiders.get(el).node)) removeSpider(state, el);
      });
      return;
    }

    clearSpiders(state);
    var radius = CLUSTER_PX * upp;
    clusterGroups(active, radius, function (a, b) {
      return Math.hypot(a.x - b.x, a.y - b.y) <= radius;
    }).forEach(function (group) {
      if (group.length < 2) return;
      group.forEach(function (node) {
        node.el.classList.add("clustered");
        node.el.style.display = "none";
      });
      addBadge(state, group, upp);
    });
  }

  function scheduleCluster(state) {
    if (state.clusterQueued) return;
    state.clusterQueued = true;
    requestAnimationFrame(function () {
      state.clusterQueued = false;
      clusterNow(state);
    });
  }

  function stopGlide(state) {
    if (state.glide) cancelAnimationFrame(state.glide);
    state.glide = 0;
  }

  function stopAnim(state) {
    if (state.anim) cancelAnimationFrame(state.anim);
    state.anim = 0;
  }

  // Glide the view into a cluster's bounds, keeping the opening view's shape.
  function zoomToNodes(state, nodes) {
    if (!nodes || !nodes.length) return;
    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    nodes.forEach(function (node) {
      minX = Math.min(minX, node.x);
      minY = Math.min(minY, node.y);
      maxX = Math.max(maxX, node.x);
      maxY = Math.max(maxY, node.y);
    });
    var aspect = state.opening.w / state.opening.h;
    var span = Math.max(maxX - minX, (maxY - minY) * aspect, 1e-6) * 1.8;
    span = Math.max(span, state.opening.w / MAX_ZOOM); // never past the deepest zoom
    span = Math.min(span, state.view.w);               // a click zooms in, never out
    if (!isFinite(span) || span <= 0) return;
    var width = span, height = span / aspect;
    animateTo(state, {
      x: (minX + maxX) / 2 - width / 2,
      y: (minY + maxY) / 2 - height / 2,
      w: width, h: height,
    });
  }

  function animateTo(state, target) {
    if (!isFinite(target.x) || !isFinite(target.y) || !isFinite(target.w) || !isFinite(target.h) || target.w <= 0 || target.h <= 0) return;
    stopGlide(state);
    stopAnim(state);
    if (reducedMotion()) {
      state.view = target;
      apply(state);
      return;
    }
    var from = { x: state.view.x, y: state.view.y, w: state.view.w, h: state.view.h };
    var start = performance.now();
    var duration = 340;
    var step = function () {
      // performance.now() inside the frame, not the rAF timestamp: some browser contexts
      // deliver a timestamp on a different origin, which used to send the viewBox negative.
      var t = (performance.now() - start) / duration;
      t = Math.min(Math.max(isFinite(t) ? t : 1, 0), 1);
      var eased = t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
      state.view.x = from.x + (target.x - from.x) * eased;
      state.view.y = from.y + (target.y - from.y) * eased;
      state.view.w = from.w + (target.w - from.w) * eased;
      state.view.h = from.h + (target.h - from.h) * eased;
      apply(state);
      if (t < 1) {
        state.anim = requestAnimationFrame(step);
      } else {
        state.anim = 0;
      }
    };
    state.anim = requestAnimationFrame(step);
  }

  // Zoom by factor around a point in user units (the viewBox's own coordinates).
  function zoomAround(state, at, factor) {
    if (!at) return;
    stopAnim(state);
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
    if (reducedMotion()) return;
    var last = performance.now();
    var step = function () {
      var now = performance.now();
      var dt = Math.min(Math.max((now - last) / 1000, 0), 0.05);
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
    readNodes(state);
    readHome(state);
    ensureClusterLayer(state);
    apply(state);
    clusterNow(state);

    // A press on the map itself: register it and watch it on the window, so a drag may leave the
    // SVG without losing the pointer. Nothing is captured, so a still press clicks as before.
    state.svg.addEventListener("pointerdown", function (evt) {
      if (evt.pointerType === "mouse" && evt.button !== 0) return;
      stopGlide(state);
      stopAnim(state);
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

    // A cluster's click is a camera move, not a card: glide into its members' bounds.
    state.svg.addEventListener("click", function (evt) {
      var badge = evt.target.closest ? evt.target.closest(".map-cluster") : null;
      if (!badge) return;
      evt.preventDefault();
      evt.stopPropagation();
      zoomToNodes(state, badge.__nodes || []);
    });

    state.svg.addEventListener("keydown", function (evt) {
      if (evt.key !== "Enter" && evt.key !== " " && evt.key !== "Spacebar") return;
      var badge = evt.target.closest ? evt.target.closest(".map-cluster") : null;
      if (!badge) return;
      evt.preventDefault();
      zoomToNodes(state, badge.__nodes || []);
    });

    state.svg.addEventListener("wheel", function (evt) {
      evt.preventDefault();
      stopGlide(state);
      stopAnim(state);
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
      stopAnim(state);
      zoomAround(state, center(), 2);
    });
    if (minus) minus.addEventListener("click", function () {
      stopGlide(state);
      stopAnim(state);
      zoomAround(state, center(), 0.5);
    });
    if (reset) reset.addEventListener("click", function () {
      // Reset glides back to the opening view: the configured node's 100 km view, or the
      // located nodes' centre when there is no configured location (MON-AD-15, D50).
      animateTo(state, { x: state.opening.x, y: state.opening.y, w: state.opening.w, h: state.opening.h });
    });

    // Layer chips: active advertised nodes, silent nodes and the base marker (D53).
    box.querySelectorAll("[data-map-toggle]").forEach(function (chip) {
      var key = chip.getAttribute("data-map-toggle");
      chip.setAttribute("aria-pressed", state.filters[key] ? "true" : "false");
      chip.addEventListener("click", function () {
        var next = chip.getAttribute("aria-pressed") !== "true";
        chip.setAttribute("aria-pressed", next ? "true" : "false");
        state.filters[key] = next;
        if (key === "base") {
          if (state.home) state.home.el.classList.toggle("map-hidden", !next);
          return;
        }
        clusterNow(state);
      });
    });

    // Card actions: copy the public key and draw or erase a line to the base node (D53).
    box.addEventListener("click", function (evt) {
      var copy = evt.target.closest ? evt.target.closest("[data-copy-key]") : null;
      if (copy) {
        evt.preventDefault();
        copyKey(copy.getAttribute("data-copy-key"), copy);
        return;
      }
      var draw = evt.target.closest ? evt.target.closest("[data-draw-base]") : null;
      if (draw) {
        evt.preventDefault();
        toggleBaseLine(state, box, draw);
      }
    });

    // A responsive SVG changes user-per-pixel without a camera move, so re-cluster on resize.
    if (window.ResizeObserver) {
      new ResizeObserver(function () { scheduleCluster(state); }).observe(state.svg);
    }
  }

  function scan() {
    document.querySelectorAll(".advert-map").forEach(init);
  }

  scan();
  document.addEventListener("DOMContentLoaded", scan);
  document.addEventListener("htmx:afterSwap", scan);
})();
