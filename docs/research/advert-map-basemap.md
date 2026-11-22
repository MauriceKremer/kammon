# Adverts map: basemap resolution, projection and serving

> **Superseded 2026-11-22:** the offline basemap this research produced was replaced by OpenFreeMap's hosted Positron tiles (D57). Kept as the record of the offline work.

Status: implemented 2026-02-14 (the §6 plan; lakes and SVG text labels left out — this repo's SVGs are textless by design, the figures ride in the notes). Date: 2026-02-14. Research for improving the Adverts page country map (`internal/web/basemap_data.go`, `internal/web/chart.go advertMapSVG`). Today the map is Natural Earth 1:110m land and borders flattened into one SVG path pair (~50 KB), drawn server-side in equirectangular projection, inlined into the page HTML on every render and every 60-second htmx swap. Constraints that frame every option: offline (GL-2, MON-AD-8 — no tiles, no third-party requests), server-drawn SVG from numbers only (DESIGN §3.2, A3), no new Go dependency, no build step, runs on a Pi.

## 1. Resolution: 1:50m is the right step up, 1:10m is not worth it

Natural Earth publishes three scales. Measured (cublya.github.io/world-atlas/guides/choosing-a-resolution/, Natural Earth 5.1.2):

| Measure | 110m (current) | 50m | 10m |
|---|---|---|---|
| Land GeoJSON | 55 KB | 545 KB | 3.37 MB |
| Land gzipped | 21 KB | 169 KB | 840 KB |
| Points | ~8 k | ~80 k | ~478 k |
| UN member states drawn | 165 / 193 | 193 / 193 | 193 / 193 |

- 110m is "a postcard map": coastlines are angular at anything past a full-width world overview, and 28 member states have no polygon at all. At country/regional zoom — KAMmon's main use — it reads as rudimentary.
- 50m is the cartographers' recommended level for full-screen interactive maps and "zoomed about 3×" views; it draws every UN member. That matches a country-scale KAMmon map at 300–960 px.
- 10m is for >3× zoom or print. At ≤960 px its extra points land on top of each other, and it has zero-area / reversed degenerate rings that break some renderers. Skip it.
- Do not simplify 10m yourself to get small files: area-threshold simplification collapses microstates that the hand-generalised 50m file keeps.

KAMmon's generator (`scripts/gen-basemap.py`) already rounds coordinates to 0.01° and dedupes repeats — about 1 km at the equator, invisible at the map's pixel scale (geojason.com measured 0.01° simplification keeps 35 % of 10m points with no visible change at survey scales). Feeding the same script 50m sources keeps the output near-lossless and lands the path text at roughly 150–350 KB. That is too big to inline per refresh but fine embedded (see §3 for serving).

Optional third layer at 50m: `ne_50m_lakes` (public domain, same source) for orientation. Rivers and populated-place labels add page weight and clutter; the design's calm principle does not want country/city text labels, and each would sit inside the numbers-only SVG, so skip labels (`adverts.md` §3.1; the strict numbers-only test).

## 2. Projection: switch equirectangular to Web Mercator

This answers the open point in requirements/Adverts.md §8. Sources: mapshaper docs on web maps; standard Mercator properties.

- Equirectangular with a tangent-plane fit is fine for spans of a few hundred km, but at continent scale it visibly stretches east–west, and the hop-distance bands (drawn as pixel circles around home) are only honest at the centre latitude.
- Mercator is conformal: local shapes and local km circles stay true everywhere the map can reach, so the hop bands stay circular and proportional at any fit. One formula per point: `y = R·ln(tan(π/4 + φ/2))`; ten lines in the client code, no new dependency. Band radii scale with the local scale factor at home latitude (`1/cos φ`).
- Every peer product the operator compares with (openstreetmap.org itself, MeshCore's internet map, MeshMonitor, MeshCore Open) draws tiles in Web Mercator, so the KAMmon map stops looking "off" against the OSM links next to it.
- Limits: Mercator has no poles beyond ±85° (irrelevant for mesh nodes) and inflates high latitudes — acceptable for a radio-range map; the 15 km/hop band assumption is itself approximate.

## 3. Serving: move the basemap out of the inline SVG into a cached static asset

The structural cost today is not the basemap's size, it is where it lives: inside every `advertMapSVG` render, so the full ~50 KB path text ships on every page view and every hourly htmx partial swap. Higher detail at 50m would multiply that.

Pattern (MDN `<use>` docs; CSS-Tricks "SVG use with External Reference"; scottjehl.com "Have It All"):

- Emit `<use href="/static/basemap.svg#basemap"/>` instead of the two paths. Same-origin external references are baseline-widely-supported and "leverage browser cache well": the file downloads once and survives every refresh.
- KAMmon already serves vendored static assets from the same origin with its own handler (`web/static`), so no new dependency and no third-party request — MON-AD-8 still holds.
- CSS classes from the page stylesheet do not reach inside an external `use` (separate DOM), but inherited CSS custom properties do. Put the fills directly in the file as presentation attributes with variable fallbacks (`fill: var(--bg-3, #26262a)` in a dark-first file plus a light override file, or one file whose variables inherit from `:root` — the Jehl pattern). Theme switching keeps working.

Result: the adverts live region drops from ~50 KB per swap to a few hundred bytes, which pays for the 50m upgrade several times over.

Rejected serving alternatives: inline-at-50m (too heavy per refresh); runtime TopoJSON decoding in Go (a decoder plus per-country paths buys nothing for one continuous outline); offline tile/PMtiles embedding (megabytes for ≤1000 dots, and it drags rendering into the browser, both violations).

## 4. Dot behaviour: what the peers do and what is worth copying

MeshCore's internet map, MeshMonitor and MeshCore Open all cluster, colour and popup their nodes on Leaflet/MapLibre tile maps. The interaction layer (zoomable, clickable, popups) is out of KAMmon's scope by design — server-drawn, read-only, no drawing in the browser. Three of their ideas fit anyway:

- **Freshness colouring.** MeshCore Map colours nodes green → yellow → red → black by how recently they were heard. KAMmon already computes a per-node activity class for the table; the map dots can carry the same class so a silent node's dot reads differently from a fresh one — one CSS class per dot, numbers-only markup.
- **Deterministic anti-overlap.** MeshCore Open spreads guessed markers with a fixed angle hashed from the public key so identical positions never stack; KAMmon already does this with the golden angle for probable dots. With up to 1000 nodes (MON-AD-3), two located nodes can share a pixel: apply the same deterministic spread to coincident dots instead of drawing on top of each other.
- **Scale bar and labelled bands.** A km scale bar and "15 km" text on the hop bands (and degree labels on the graticule) are derived numbers, not radio text, so they stay inside the numbers-only rule and cost a few lines. High value for a range-planning picture: the operator can read "my mesh reaches ~45 km" straight off the map.

Not copied: clustering into numbered blobs (KAMmon dots are already numbered like the table; a cluster would break that mapping), link/trace overlays (`hop_stations` ambiguity is a documented open point, unchanged).

## 5. Default view: country level, robust to bad advertised coordinates

Operator requirement: the default area of the map is the country, not the nodes — because some adverts carry bad GPS coordinates and must not define the zoom.

Two failure modes today (`advertMap` fits the viewBox to every located node plus home and bands):

- **One bogus coordinate stretches the map.** A node advertising a position thousands of km away pulls the fit to world scale; every real node shrinks to a dot cluster.
- **"No fix" positions sit on Null Island.** Devices without a GPS fix advertise 0°N 0°E with `has_location` set. They drag the fit towards the Gulf of Guinea and draw as dots there, mislabelled as places. MeshCore Open's position history already drops Null Island fixes for the same reason (`meshcoreopen.org/docs/map/`).

Design: the view comes from the operator's configured location, and adverts only draw *inside* it.

1. **Country frame from `location` in kammon.yml.** `scripts/gen-basemap.py` gains a second output: Natural Earth 110m `admin_0_countries` as country id + polygon rings (~8 k points, ~80–100 KB of generated Go, never drawn — used only for lookups). At render time: point-in-polygon with the home coordinate, then the default view is the **bbox of the polygon ring that contains home**, padded ~8 %. Taking the containing ring rather than the whole country's bbox is what makes it correct: metropolitan France, conterminous USA, mainland Norway and Portugal — the overseas/archipelago rings that share the country's bbox never fit, because home is not in them.
2. **Microstates.** The 110m countries file omits 28 UN members (Malta, Monaco, Singapore, Liechtenstein…). Add Natural Earth's 110m `admin_0_tiny_countries` points (~2 KB): when home falls in one, the default view is a fixed window around its point (~±0.5°).
3. **Fallbacks, honest by order.** No `location` configured, or home falls in no country polygon (coastal, 110m generalisation): fall back to today's node-based fit, minus the bad-coordinate filter below. The page can say the country could not be identified.
4. **Bad coordinates never shape the view.**
   - Adverts within ±0.01° of 0°N 0°E are treated as no fix: never plotted, never affect any fit, stay in the table with their advertised coordinate. A note counts them, e.g. "2 nodes advertise 0°N 0°E (no GPS fix) and are not plotted."
   - Any advert outside the default area — bogus or genuinely elsewhere — is simply not drawn. A note counts them. The view never widens for them.
   - Adverts inside the area draw as today: advertised coordinates, honest as always (`position is advertised, not measured`).
5. **Bands unchanged.** Probable-hop bands stay anchored at home; where a band exceeds the area it clips at the frame edge, as any off-map content does.
6. **With Mercator (§2)** the fit happens in projected space: transform the ring bbox latitudes through the Mercator y before computing the span.

Cost: one-time ~90–100 KB of generated data, one bbox check + ray-cast per render (early exit on the country bbox). The 50m countries file would buy no visible pip benefit at 10× the size — skip it.

## 6. Plan, smallest complete change

1. `scripts/gen-basemap.py`: switch to the 50m land and `admin_0_boundary_lines_land` sources (optionally lakes); keep 0.01° rounding. One-time ~150–350 KB into the binary.
2. `scripts/gen-basemap.py` second output: 110m `admin_0_countries` rings and `admin_0_tiny_countries` points for the default-view lookup (§5); generated, small, drawn never.
3. Emit the basemap to `web/static/basemap.svg` at startup or embed it as an asset; reference it with `<use>`; colours as variable-fallback presentation attributes.
4. Switch `advertMap`/`advertMapSVG` to Mercator; scale band radii by the home-latitude factor.
5. Country-level default view and bad-coordinate handling per §5: ring-bbox default, Null Island filter, not-drawn counts in the notes, fallback chain.
6. Add scale bar, graticule labels, band km labels, freshness classes on dots, deterministic spread for coincident dots.
7. Update Adverts.md (MON-AD-6 wording: country frame, no-fix and out-of-area counting; close the §8 projection point) and DESIGN §3.2 where "110m" or inline-path is named. Tests: numbers-only test unchanged; the fit ignores a 0°N 0°E advert and a far-away advert when `location` is configured; fallback without `location`; ring-containing-home choice; note counts; `/static/basemap.svg` cache headers.

## 7. Sources

- cublya.github.io/world-atlas/guides/choosing-a-resolution/ — 110m/50m/10m sizes, coverage, why not to self-simplify 10m
- geojason.com/geojson-file-size-optimization — coordinate-precision vs visibility table
- martynafford/natural-earth-geojson — CC0 GeoJSON mirrors of all three scales and 50m lakes
- mapshaper.org/docs/guides/geojson-for-web-maps.html — WGS84/projection practice for web maps
- developer.mozilla.org/en-US/docs/Web/SVG/Reference/Element/use — same-origin external `use`, cache behaviour
- css-tricks.com/svg-use-with-external-reference-take-2/ — external sprite caching, CSS boundary limits
- scottjehl.com/posts/svgtopia/ — styleable external SVG via CSS custom properties with fallbacks
- blog.meshcore.io/2026/04/04/meshcore-map — freshness colours, clustering, QR per node
- meshcoreopen.org/docs/map/ — deterministic marker spread for guessed positions
- meshmonitor.org — peer monitor's interactive map scope (context only)
