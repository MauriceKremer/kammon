// The advert map (MON-AD-6/15/17, D57): MapLibre GL JS draws OpenFreeMap's Positron basemap in the
// browser, and the located adverts ride a clustered GeoJSON source. Only advert positions that KAM
// stored become features; a node without a position, or with a 0°N 0°E no-fix coordinate, is
// counted in the notes and never guessed onto the map. Radio names ride as GeoJSON properties and
// are only ever written as text — never as inner HTML (AGENTS rule 5).
//
// The camera opens on a 100 km radius around the configured node (the located nodes' centre
// without one, the whole world without either), survives the 60 s live refresh, and persists in
// localStorage across reloads when the opening anchor still matches (MON-AD-15, D56). The flat
// inspector cards, zoom buttons and layer chips from D55 are unchanged in spirit: this script owns
// the camera, the dots and the line to the base, while map.js owns the cards' DOM.
(function () {
  "use strict";

  var POSITRON = "https://tiles.openfreemap.org/styles/positron";
  var STORE_KEY = "kammon:adverts:camera";
  var MAX_ZOOM = 18;
  var CLUSTER_RADIUS = 42;
  var CLUSTER_MAX_ZOOM = 14;
  var DOT_PX = 5;
  var GOLDEN_ANGLE = 2.399963229728653;

  var SRC_ACTIVE = "adverts-active";
  var SRC_SILENT = "adverts-silent";
  var SRC_SPIDER = "adverts-spider";
  var SRC_LEGS = "adverts-spider-legs";
  var SRC_HOME = "adverts-home";
  var SRC_LINE = "adverts-base-line";
  var LYR_CLUSTERS = "advert-clusters";
  var LYR_CLUSTER_COUNT = "advert-cluster-count";
  var LYR_CLUSTERS_SILENT = "advert-clusters-silent";
  var LYR_CLUSTER_COUNT_SILENT = "advert-cluster-count-silent";
  var LYR_DOTS = "advert-dots";
  var LYR_DOTS_SILENT = "advert-dots-silent";
  var LYR_SPIDER_LEGS = "advert-spider-legs";
  var LYR_SPIDER = "advert-spider-dots";
  var LYR_HOME = "advert-home";
  var LYR_LINE = "advert-base-line";
  var CLICKABLE = [LYR_CLUSTERS, LYR_CLUSTERS_SILENT, LYR_DOTS, LYR_DOTS_SILENT, LYR_SPIDER];

  var initialised = new WeakSet();
  var lastView = null;   // the last camera, so the live refresh keeps the view
  var lastFilters = null;
  var persistTimer = 0;

  function reducedMotion() {
    return window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  }

  function emit(box, name, detail) {
    box.dispatchEvent(new CustomEvent(name, { bubbles: true, detail: detail }));
  }

  // ---- stored state (D56) --------------------------------------------------

  function loadStored() {
    try {
      var raw = window.localStorage.getItem(STORE_KEY);
      if (!raw) return null;
      var obj = JSON.parse(raw);
      if (!obj || typeof obj.anchor !== "string") return null;
      return obj;
    } catch (e) {
      return null; // storage disabled: the camera simply does not persist
    }
  }

  function persist(state) {
    var map = state.map;
    if (!map) return;
    var center = map.getCenter();
    try {
      window.localStorage.setItem(STORE_KEY, JSON.stringify({
        anchor: state.anchor,
        center: [center.lng, center.lat],
        zoom: map.getZoom(),
        filters: state.filters,
      }));
    } catch (e) { /* storage disabled or full: the camera will not survive a reload */ }
  }

  function schedulePersist(state) {
    if (persistTimer) clearTimeout(persistTimer);
    persistTimer = setTimeout(function () { persistTimer = 0; persist(state); }, 200);
  }

  // ---- map data ------------------------------------------------------------

  function parseJSON(text) {
    try { return text ? JSON.parse(text) : null; } catch (e) { return null; }
  }

  function emptyCollection() { return { type: "FeatureCollection", features: [] }; }

  function palette() {
    var style = window.getComputedStyle(document.documentElement);
    var pick = function (name, fallback) {
      var value = style.getPropertyValue(name).trim();
      return value || fallback;
    };
    return {
      success: pick("--success", "#3ddc91"),
      warning: pick("--warning", "#f5b544"),
      muted: pick("--text-muted", "#a1a1a8"),
      signal: pick("--signal", "#80b8ff"),
      bg0: pick("--bg-0", "#101012"),
      bg1: pick("--bg-1", "#18181b"),
      text: pick("--text-primary", "#f5f5f7"),
    };
  }

  function activityColor(value) {
    var p = palette();
    switch (value) {
      case "fresh": return p.success;
      case "recent": return p.warning;
      default: return p.muted;
    }
  }

  // A dot's colour follows the table's last-heard class; a stale dot is muted and translucent.
  function dotColor() {
    return ["match", ["get", "activity"],
      "fresh", activityColor("fresh"),
      "recent", activityColor("recent"),
      "stale", activityColor("stale"),
      activityColor("unknown")];
  }

  function splitFeatures(collection) {
    var active = emptyCollection();
    var silent = emptyCollection();
    var features = collection && collection.features ? collection.features : [];
    features.forEach(function (feature) {
      if (!feature || !feature.geometry || feature.geometry.type !== "Point") return;
      if (feature.properties && feature.properties.activity === "stale") silent.features.push(feature);
      else active.features.push(feature);
    });
    return { active: active, silent: silent };
  }

  function byNumber(features) {
    var index = {};
    (features || []).forEach(function (feature) {
      if (!feature || !feature.geometry) return;
      var n = Number(feature.properties && feature.properties.number);
      if (n) index[n] = feature.geometry.coordinates;
    });
    return index;
  }

  function addSources(map, state, data) {
    var sources = [
      [SRC_ACTIVE, data.active, true],
      [SRC_SILENT, data.silent, true],
    ];
    sources.forEach(function (source) {
      map.addSource(source[0], {
        type: "geojson",
        data: source[1],
        cluster: source[2],
        clusterRadius: CLUSTER_RADIUS,
        clusterMaxZoom: CLUSTER_MAX_ZOOM,
      });
    });
    map.addSource(SRC_SPIDER, { type: "geojson", data: emptyCollection() });
    map.addSource(SRC_LEGS, { type: "geojson", data: emptyCollection() });
    map.addSource(SRC_LINE, { type: "geojson", data: emptyCollection() });
    if (state.hasHome) {
      map.addSource(SRC_HOME, {
        type: "geojson",
        data: {
          type: "FeatureCollection",
          features: [{
            type: "Feature",
            geometry: { type: "Point", coordinates: state.home },
            properties: {},
          }],
        },
      });
    }

    var p = palette();
    addClusterLayers(map, LYR_CLUSTERS, LYR_CLUSTER_COUNT, SRC_ACTIVE, p.signal, p);
    addClusterLayers(map, LYR_CLUSTERS_SILENT, LYR_CLUSTER_COUNT_SILENT, SRC_SILENT, p.muted, p);
    addDotLayer(map, LYR_DOTS, SRC_ACTIVE);
    addDotLayer(map, LYR_DOTS_SILENT, SRC_SILENT);

    map.addLayer({
      id: LYR_SPIDER_LEGS,
      type: "line",
      source: SRC_LEGS,
      paint: { "line-color": p.muted, "line-width": 1, "line-opacity": 0.7 },
    });
    addDotLayer(map, LYR_SPIDER, SRC_SPIDER);

    map.addLayer({
      id: LYR_LINE,
      type: "line",
      source: SRC_LINE,
      paint: {
        "line-color": p.success,
        "line-width": 1.5,
        "line-opacity": 0.8,
        "line-dasharray": [5, 4],
      },
    });

    if (state.hasHome) {
      map.addImage("advert-home", homeImage(p), { pixelRatio: 2 });
      map.addLayer({
        id: LYR_HOME,
        type: "symbol",
        source: SRC_HOME,
        layout: {
          "icon-image": "advert-home",
          "icon-size": 1,
          "icon-allow-overlap": true,
          "icon-ignore-placement": true,
        },
      });
    }

    applyVisibility(map, state);
    applyTheme(map, state);
  }

  function addClusterLayers(map, circleId, countId, source, color, p) {
    map.addLayer({
      id: circleId,
      type: "circle",
      source: source,
      filter: ["has", "point_count"],
      paint: {
        "circle-color": color,
        "circle-radius": ["step", ["get", "point_count"], 13, 10, 17, 50, 21, 200, 25],
        "circle-stroke-width": 2,
        "circle-stroke-color": p.bg0,
        "circle-opacity": 0.92,
      },
    });
    // A blank fallback style has no glyphs: the clusters still draw, just without their count.
    if (!map.getStyle().glyphs) return;
    map.addLayer({
      id: countId,
      type: "symbol",
      source: source,
      filter: ["has", "point_count"],
      layout: {
        "text-field": ["get", "point_count_abbreviated"],
        "text-font": ["Noto Sans Bold"],
        "text-size": 11,
        "text-allow-overlap": true,
        "text-ignore-placement": true,
      },
      paint: { "text-color": p.bg0 },
    });
  }

  function addDotLayer(map, id, source) {
    var p = palette();
    map.addLayer({
      id: id,
      type: "circle",
      source: source,
      filter: ["!", ["has", "point_count"]],
      paint: {
        "circle-color": dotColor(),
        "circle-radius": DOT_PX,
        "circle-stroke-width": 1.5,
        "circle-stroke-color": p.bg0,
        "circle-opacity": ["match", ["get", "activity"], "stale", 0.7, 1],
      },
    });
  }

  // The base node is a neutral anchor, not a state: a star with a thin ring, never green or pulsing
  // (D55). It is drawn once to a canvas at 2× so it stays crisp on retina screens.
  function homeImage(p) {
    var size = 30;
    var canvas = document.createElement("canvas");
    canvas.width = size * 2;
    canvas.height = size * 2;
    var ctx = canvas.getContext("2d");
    ctx.scale(2, 2);
    ctx.translate(size / 2, size / 2);
    ctx.beginPath();
    ctx.arc(0, 0, 11, 0, Math.PI * 2);
    ctx.strokeStyle = p.text;
    ctx.globalAlpha = 0.35;
    ctx.lineWidth = 1.2;
    ctx.stroke();
    ctx.globalAlpha = 1;
    ctx.beginPath();
    for (var i = 0; i < 10; i++) {
      var radius = i % 2 === 0 ? 8 : 3.4;
      var angle = -Math.PI / 2 + i * Math.PI / 5;
      var x = Math.cos(angle) * radius;
      var y = Math.sin(angle) * radius;
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    }
    ctx.closePath();
    ctx.fillStyle = p.text;
    ctx.fill();
    ctx.strokeStyle = p.bg0;
    ctx.lineWidth = 1;
    ctx.lineJoin = "round";
    ctx.stroke();
    return ctx.getImageData(0, 0, canvas.width, canvas.height);
  }

  function applyTheme(map, state) {
    var p = palette();
    if (map.getLayer(LYR_DOTS)) {
      map.setPaintProperty(LYR_DOTS, "circle-color", dotColor());
      map.setPaintProperty(LYR_DOTS, "circle-stroke-color", p.bg0);
    }
    if (map.getLayer(LYR_DOTS_SILENT)) {
      map.setPaintProperty(LYR_DOTS_SILENT, "circle-color", dotColor());
      map.setPaintProperty(LYR_DOTS_SILENT, "circle-stroke-color", p.bg0);
    }
    if (map.getLayer(LYR_SPIDER)) {
      map.setPaintProperty(LYR_SPIDER, "circle-color", dotColor());
      map.setPaintProperty(LYR_SPIDER, "circle-stroke-color", p.bg0);
    }
    if (map.getLayer(LYR_CLUSTERS)) {
      map.setPaintProperty(LYR_CLUSTERS, "circle-color", p.signal);
      map.setPaintProperty(LYR_CLUSTERS, "circle-stroke-color", p.bg0);
    }
    if (map.getLayer(LYR_CLUSTERS_SILENT)) {
      map.setPaintProperty(LYR_CLUSTERS_SILENT, "circle-color", p.muted);
      map.setPaintProperty(LYR_CLUSTERS_SILENT, "circle-stroke-color", p.bg0);
    }
    [LYR_CLUSTER_COUNT, LYR_CLUSTER_COUNT_SILENT].forEach(function (id) {
      if (map.getLayer(id)) map.setPaintProperty(id, "text-color", p.bg0);
    });
    if (map.getLayer(LYR_LINE)) map.setPaintProperty(LYR_LINE, "line-color", p.success);
    if (map.getLayer(LYR_HOME)) {
      if (map.hasImage("advert-home")) map.removeImage("advert-home");
      map.addImage("advert-home", homeImage(p), { pixelRatio: 2 });
    }
    if (map.getLayer(LYR_SPIDER_LEGS)) map.setPaintProperty(LYR_SPIDER_LEGS, "line-color", p.muted);
  }

  function applyVisibility(map, state) {
    var set = function (id, visible) {
      if (map.getLayer(id)) map.setLayoutProperty(id, "visibility", visible ? "visible" : "none");
    };
    set(LYR_CLUSTERS, state.filters.advertised);
    set(LYR_CLUSTER_COUNT, state.filters.advertised);
    set(LYR_DOTS, state.filters.advertised);
    set(LYR_CLUSTERS_SILENT, state.filters.silent);
    set(LYR_CLUSTER_COUNT_SILENT, state.filters.silent);
    set(LYR_DOTS_SILENT, state.filters.silent);
    set(LYR_HOME, state.hasHome && state.filters.base);
    if (!state.filters.advertised || !state.filters.silent) clearSpider(map, state);
  }

  // ---- opening view and camera --------------------------------------------

  function radiusBounds(lat, lon, km) {
    var cos = Math.max(Math.cos(lat * Math.PI / 180), 0.2);
    var dLat = km / 111.32;
    var dLon = km / (111.32 * cos);
    return [[lon - dLon, lat - dLat], [lon + dLon, lat + dLat]];
  }

  // The opening view covers 200 km across the shorter side of the map, i.e. a 100 km radius.
  // fitBounds lets MapLibre do the projection maths, so the radius stays honest at any latitude.
  function fitOpening(map, state, animate) {
    if (!state.hasCenter) return;
    map.fitBounds(radiusBounds(state.openingCenter[1], state.openingCenter[0], state.radiusKm), {
      padding: 0,
      maxZoom: MAX_ZOOM,
      duration: animate && !reducedMotion() ? 400 : 0,
    });
  }

  function resetView(map, state) {
    fitOpening(map, state, true);
  }

  function updateZoomButtons(map, box) {
    var plus = box.querySelector('[data-map-zoom="in"]');
    var minus = box.querySelector('[data-map-zoom="out"]');
    if (plus) plus.disabled = map.getZoom() >= MAX_ZOOM - 1e-6;
    if (minus) minus.disabled = map.getZoom() <= map.getMinZoom() + 1e-6;
  }

  // ---- cards, hover and picking -------------------------------------------

  function boxPoint(el, point) {
    return { x: el.offsetLeft + point.x, y: el.offsetTop + point.y };
  }

  function emitPin(map, state, number, point) {
    var spot = boxPoint(state.el, point);
    state.pinned = number;
    emit(state.box, "advertsmappin", { number: number, x: spot.x, y: spot.y });
  }

  function emitHover(map, state, number, point) {
    var spot = point ? boxPoint(state.el, point) : { x: 0, y: 0 };
    emit(state.box, "advertsmaphover", { number: number, x: spot.x, y: spot.y });
  }

  // ---- clusters and spiderfy ----------------------------------------------

  function dotFilter(numbers) {
    var base = ["!", ["has", "point_count"]];
    if (!numbers.length) return base;
    return ["all", base, ["!", ["in", ["get", "number"], ["literal", numbers]]]];
  }

  function clearSpider(map, state, quiet) {
    if (!map.getSource(SRC_SPIDER)) return;
    map.getSource(SRC_SPIDER).setData(emptyCollection());
    map.getSource(SRC_LEGS).setData(emptyCollection());
    state.spider = [];
    map.setFilter(LYR_DOTS, dotFilter([]));
    map.setFilter(LYR_DOTS_SILENT, dotFilter([]));
    if (!quiet) state.pinned = null;
  }

  function pointFeature(number, activity, coords) {
    return {
      type: "Feature",
      geometry: { type: "Point", coordinates: coords },
      properties: { number: Number(number), activity: activity || "" },
    };
  }

  function legFeature(from, to) {
    return {
      type: "Feature",
      geometry: { type: "LineString", coordinates: [from, to] },
      properties: {},
    };
  }

  function spiderfy(map, state, leaves) {
    if (!leaves || !leaves.length) return;
    clearSpider(map, state, true);
    var center = leaves[0].geometry.coordinates;
    var projected = map.project(center);
    var points = [], legs = [], numbers = [];
    leaves.forEach(function (leaf, i) {
      var activity = leaf.properties && leaf.properties.activity;
      if (activity === "stale" && !state.filters.silent) return;
      if (activity !== "stale" && !state.filters.advertised) return;
      var number = Number(leaf.properties && leaf.properties.number);
      if (!number || numbers.indexOf(number) >= 0) return;
      var angle = i * GOLDEN_ANGLE;
      var radius = 26 + 15 * Math.sqrt(i);
      var coords = map.unproject([projected.x + Math.cos(angle) * radius, projected.y + Math.sin(angle) * radius]);
      var place = [coords.lng, coords.lat];
      points.push(pointFeature(number, activity, place));
      legs.push(legFeature(center, place));
      numbers.push(number);
    });
    if (points.length < 2) return;
    map.getSource(SRC_SPIDER).setData({ type: "FeatureCollection", features: points });
    map.getSource(SRC_LEGS).setData({ type: "FeatureCollection", features: legs });
    state.spider = numbers;
    map.setFilter(LYR_DOTS, dotFilter(numbers));
    map.setFilter(LYR_DOTS_SILENT, dotFilter(numbers));
    state.pinned = null;
    emit(state.box, "advertsmapclosecards");
  }

  function clusterSource(layerId) {
    return layerId === LYR_CLUSTERS_SILENT ? SRC_SILENT : SRC_ACTIVE;
  }

  function activateCluster(map, state, feature) {
    var sourceId = clusterSource(feature.layer.id);
    var source = map.getSource(sourceId);
    var clusterId = feature.properties.cluster_id;
    if (!source || clusterId == null) return;
    Promise.resolve(source.getClusterExpansionZoom(clusterId)).then(function (zoom) {
      if (zoom > map.getMaxZoom()) {
        return source.getClusterLeaves(clusterId, 1000, 0).then(function (leaves) {
          spiderfy(map, state, leaves);
        });
      }
      clearSpider(map, state);
      emit(state.box, "advertsmapclosecards");
      map.easeTo({
        center: feature.geometry.coordinates,
        zoom: Math.min(zoom + 0.05, map.getMaxZoom()),
        duration: reducedMotion() ? 0 : 350,
      });
    }).catch(function () { /* a cluster that moved away mid-flight: nothing to do */ });
  }

  function featuresAt(map, point) {
    return map.queryRenderedFeatures(point).filter(function (feature) {
      return feature.layer && CLICKABLE.indexOf(feature.layer.id) >= 0;
    });
  }

  function bindMap(map, state) {
    var canvas = map.getCanvas();
    var hovered = null;

    map.on("mousemove", function (evt) {
      var hits = featuresAt(map, evt.point);
      var top = hits[0];
      canvas.style.cursor = top ? "pointer" : "";
      var number = top && top.properties && top.properties.cluster ? null : top && top.properties && Number(top.properties.number) || null;
      if (number === hovered) return;
      hovered = number;
      emitHover(map, state, number, number ? evt.point : null);
    });

    canvas.addEventListener("mouseleave", function () {
      if (hovered == null) return;
      hovered = null;
      emitHover(map, state, null, null);
    });

    map.on("click", function (evt) {
      var hits = featuresAt(map, evt.point);
      if (!hits.length) {
        clearSpider(map, state);
        state.pinned = null;
        emit(state.box, "advertsmapunpin");
        return;
      }
      var cluster = hits.filter(function (f) { return f.properties && f.properties.cluster; })[0];
      if (cluster) {
        activateCluster(map, state, cluster);
        return;
      }
      var spider = hits.filter(function (f) { return f.layer.id === LYR_SPIDER; })[0];
      if (spider) {
        emitPin(map, state, Number(spider.properties.number), evt.point);
        return;
      }
      var unique = [], leaves = [];
      hits.forEach(function (f) {
        if (!f.geometry) return;
        var n = Number(f.properties && f.properties.number);
        if (!n || unique.indexOf(n) >= 0) return;
        unique.push(n);
        leaves.push(f);
      });
      if (unique.length > 1) {
        spiderfy(map, state, leaves);
        return;
      }
      clearSpider(map, state, true);
      emitPin(map, state, unique[0], evt.point);
    });

    map.on("movestart", function (evt) {
      if (evt.originalEvent) clearSpider(map, state);
    });

    // A pinned card follows its dot through pan and zoom.
    map.on("move", function () {
      if (state.pinned == null) return;
      var coords = state.coords[state.pinned];
      if (!coords) return;
      var projected = map.project(coords);
      var spot = boxPoint(state.el, projected);
      emit(state.box, "advertsmapposition", { number: state.pinned, x: spot.x, y: spot.y });
    });

    map.on("zoom", function () { updateZoomButtons(map, state.box); });
    map.on("moveend", function () {
      var center = map.getCenter();
      lastView = { anchor: state.anchor, center: [center.lng, center.lat], zoom: map.getZoom() };
      lastFilters = state.filters;
      schedulePersist(state);
    });
  }

  // ---- line to base --------------------------------------------------------

  function clearLine(map, state) {
    if (map.getSource(SRC_LINE)) map.getSource(SRC_LINE).setData(emptyCollection());
    state.line = null;
    state.box.querySelectorAll("[data-draw-base]").forEach(function (button) {
      button.setAttribute("aria-pressed", "false");
      button.textContent = "Draw Line to Base";
    });
  }

  function toggleLine(map, state, number) {
    var active = String(state.line) === String(number);
    clearLine(map, state);
    if (active || !state.hasHome) return;
    var target = state.coords[number];
    if (!target) return;
    map.getSource(SRC_LINE).setData({
      type: "FeatureCollection",
      features: [legFeature(state.home, target)],
    });
    state.line = String(number);
    state.box.querySelectorAll('[data-draw-base]').forEach(function (button) {
      if (button.getAttribute("data-draw-base") !== String(number)) return;
      button.setAttribute("aria-pressed", "true");
      button.textContent = "Hide Line to Base";
    });
  }

  // ---- keyboard access -----------------------------------------------------

  function bindList(map, state) {
    state.box.querySelectorAll("[data-map-node]").forEach(function (button) {
      button.addEventListener("click", function () {
        var number = Number(button.getAttribute("data-map-node"));
        var coords = state.coords[number];
        if (!coords) return;
        clearSpider(map, state);
        map.easeTo({
          center: coords,
          zoom: Math.max(map.getZoom(), 10),
          duration: reducedMotion() ? 0 : 300,
        });
        map.once("moveend", function () {
          emitPin(map, state, number, map.project(coords));
        });
      });
    });
  }

  // ---- offline honesty (MON-AD-8) ----------------------------------------

  function setOffline(state, show) {
    var note = state.box.querySelector("[data-map-offline]");
    if (note) note.hidden = !show;
  }

  function loadStyle() {
    return new Promise(function (resolve) {
      var settled = false;
      var finish = function (value) {
        if (settled) return;
        settled = true;
        resolve(value);
      };
      var timer = setTimeout(function () { finish(null); }, 9000);
      if (!window.fetch) { clearTimeout(timer); finish(null); return; }
      window.fetch(POSITRON, { credentials: "omit" })
        .then(function (response) {
          if (!response.ok) throw new Error("style " + response.status);
          return response.json();
        })
        .then(function (style) {
          clearTimeout(timer);
          finish(style);
        })
        .catch(function () {
          clearTimeout(timer);
          finish(null);
        });
    });
  }

  function blankStyle() {
    return {
      version: 8,
      sources: {},
      layers: [{
        id: "background",
        type: "background",
        paint: { "background-color": palette().bg1 },
      }],
    };
  }

  // ---- init ----------------------------------------------------------------

  function readState(el) {
    var box = el.closest(".advert-map-box");
    var data = parseJSON(el.getAttribute("data-geojson")) || emptyCollection();
    var hasCenter = el.getAttribute("data-has-center") === "1";
    var hasHome = el.getAttribute("data-has-home") === "1";
    var lat = Number(el.getAttribute("data-center-lat")) || 0;
    var lon = Number(el.getAttribute("data-center-lon")) || 0;
    var homeLat = Number(el.getAttribute("data-home-lat")) || 0;
    var homeLon = Number(el.getAttribute("data-home-lon")) || 0;
    var anchor = el.getAttribute("data-map-anchor") || "world";
    var stored = loadStored();
    var restore = null;
    if (lastView && lastView.anchor === anchor) restore = lastView;
    else if (stored && stored.anchor === anchor && Array.isArray(stored.center) && typeof stored.zoom === "number") restore = stored;
    var filters = { advertised: true, silent: false, base: true };
    if (lastFilters && lastView && lastView.anchor === anchor) filters = lastFilters;
    else if (stored && stored.anchor === anchor && stored.filters) {
      filters = {
        advertised: stored.filters.advertised !== false,
        silent: stored.filters.silent === true,
        base: stored.filters.base !== false,
      };
    }
    var split = splitFeatures(data);
    return {
      el: el,
      box: box,
      anchor: anchor,
      geo: data,
      split: split,
      coords: byNumber(split.active.features.concat(split.silent.features)),
      hasCenter: hasCenter,
      hasHome: hasHome,
      center: hasCenter ? [lon, lat] : [0, 20],
      home: hasHome ? [homeLon, homeLat] : null,
      radiusKm: Number(el.getAttribute("data-radius-km")) || 100,
      filters: filters,
      restore: restore,
      spider: [],
      pinned: null,
      line: null,
      map: null,
    };
  }

  function init(el) {
    if (initialised.has(el)) return;
    initialised.add(el);
    var box = el.closest(".advert-map-box");
    if (!box || !window.maplibregl) {
      if (box) {
        var note = box.querySelector("[data-map-offline]");
        if (note) note.hidden = false;
      }
      return;
    }
    var state = readState(el);
    state.openingCenter = state.center.slice();

    loadStyle().then(function (style) {
      var online = !!style;
      if (!online) setOffline(state, true);
      var map = new window.maplibregl.Map({
        container: el,
        style: style || blankStyle(),
        center: state.restore ? state.restore.center : state.center,
        zoom: state.restore ? state.restore.zoom : 0,
        minZoom: 0,
        maxZoom: MAX_ZOOM,
        attributionControl: false,
        dragRotate: false,
        pitchWithRotate: false,
        renderWorldCopies: true,
      });
      state.map = map;
      map.touchZoomRotate.disableRotation();
      map.addControl(new window.maplibregl.ScaleControl({ maxWidth: 96, unit: "metric" }), "bottom-left");

      map.on("load", function () {
        addSources(map, state, state.split);
        // The camera is fitted once the canvas has its real size; fitting against a 0×0 transform
        // would ask MapLibre for an impossible zoom.
        if (state.restore) map.jumpTo({ center: state.restore.center, zoom: state.restore.zoom });
        else fitOpening(map, state, false);
        updateZoomButtons(map, state.box);
      });

      map.on("error", function () { setOffline(state, true); });
      map.on("sourcedata", function (evt) {
        if (evt.isSourceLoaded && evt.sourceId && evt.sourceId.indexOf("openmaptiles") === 0) setOffline(state, false);
      });

      bindMap(map, state);
      bindList(map, state);

      map.on("resize", function () { updateZoomButtons(map, state.box); });

      // Zoom, reset and chips are flat DOM beside the canvas (D55).
      var plus = box.querySelector('[data-map-zoom="in"]');
      var minus = box.querySelector('[data-map-zoom="out"]');
      var reset = box.querySelector("[data-map-reset]");
      if (plus) plus.addEventListener("click", function () {
        clearSpider(map, state);
        map.zoomTo(Math.min(map.getZoom() + 1, MAX_ZOOM), { duration: reducedMotion() ? 0 : 180 });
      });
      if (minus) minus.addEventListener("click", function () {
        clearSpider(map, state);
        map.zoomTo(Math.max(map.getZoom() - 1, map.getMinZoom()), { duration: reducedMotion() ? 0 : 180 });
      });
      if (reset) reset.addEventListener("click", function () {
        clearSpider(map, state);
        resetView(map, state);
      });

      box.querySelectorAll("[data-map-toggle]").forEach(function (chip) {
        var key = chip.getAttribute("data-map-toggle");
        chip.setAttribute("aria-pressed", state.filters[key] ? "true" : "false");
        chip.addEventListener("click", function () {
          var next = chip.getAttribute("aria-pressed") !== "true";
          chip.setAttribute("aria-pressed", next ? "true" : "false");
          state.filters[key] = next;
          lastFilters = state.filters;
          applyVisibility(map, state);
          schedulePersist(state);
        });
      });

      box.addEventListener("advertsmapunpin", function () {
        state.pinned = null;
        clearSpider(map, state, true);
      });
      box.addEventListener("advertsmapline", function (evt) {
        toggleLine(map, state, evt.detail.number);
      });

      if (window.ResizeObserver) {
        new ResizeObserver(function () { map.resize(); }).observe(el);
      }
      var themeObserver = new MutationObserver(function () { applyTheme(map, state); });
      themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    });
  }

  function scan() {
    document.querySelectorAll(".advert-map").forEach(init);
  }

  scan();
  document.addEventListener("DOMContentLoaded", scan);
  document.addEventListener("htmx:afterSwap", scan);
})();
