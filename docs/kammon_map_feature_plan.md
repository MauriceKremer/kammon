# KAMmon Map — OpenFreeMap Positron Plan

**Status:** implemented 2026-11-22 — the generated Natural Earth SVG basemap on `/adverts` is replaced by OpenFreeMap's hosted **Positron** tiles, drawn with MapLibre GL JS. This is a deliberate change of direction; the flat operator-console overlay (D55) and the page's data rules stay.

## Why this change

The map currently draws a server-generated Natural Earth basemap (D42) so it can run with no tiles and no third-party request. That bought offline operation, but it reads as a schematic: hand-drawn coastlines and borders, with city names the operator has to reconcile against the familiar map in their head.

We are deliberately switching the basemap to **OpenFreeMap Positron**. Three reasons, in order:

1. **It is a selling point.** A real, familiar, high-quality basemap makes the product look finished and trustworthy next to the peers — MeshCore's internet map, MeshMonitor and MeshCore Open all run on tile maps.
2. **Positron fits the console.** Positron is the light, desaturated, no-decoration style: the basemap recedes so the node dots are the subject. It matches [DESIGN.md](DESIGN.md) §5's calm principle better than any hand-tuned SVG.
3. **It is free and open.** OpenFreeMap serves the tiles with no API key, no account and no usage cap at this scale, on the same OpenStreetMap data we already link to.

This is a *deliberate trade*, not an accident: the map stops working offline for its background, but the page's data never does.

## What this reverses (be honest)

The offline/no-tiles design was a real constraint, not a mistake, and we are reversing it on purpose. The affected decisions and requirements:

| Was | Becomes |
|---|---|
| Offline, no tiles, no third-party request (GL-2, `MON-AD-8`) | The basemap is the one online exception: tiles load from OpenFreeMap's CDN; everything else stays same-origin. |
| Server-drawn SVG, browser only moves a window into it (A3, D42, D47) | MapLibre GL JS draws the basemap in the browser; the store still feeds the dots. |
| No Leaflet/MapLibre/Supercluster, no build step (D49) | MapLibre GL JS is vendored like htmx (one prebuilt file, no npm build step); the no-build-step rule still holds. |
| Vendored Natural Earth + Dutch city labels (D45, D51, D54) | Positron labels places itself; the bespoke label engine is removed. |

The map needs the internet for its background. When tiles cannot load, the page says so honestly, and the dots — which come from the store, not the internet — still draw over a plain background. Radio data is never affected by a tile outage.

## What stays

- **Data from the store.** The poller and `store.Adverts()` are unchanged; the map still reads only the store, never KAM (rule 3).
- **Only valid advertised positions are plotted.** No-fix (0°N 0°E) and unlocated nodes stay in the table and are counted, never guessed onto the map (D48, `MON-AD-6/7`).
- **Flat operator-console overlay** (D55): no cards, gradients, glows or shadows; radius 6 px; blue for headings/actions only, green/amber/red for states. The inspector cards, zoom/reset controls and Advertised/Silent/Base chips keep their flat tokens.
- **Honest labels** (`MON-AD-9/10`): "advertised position", "hops = path length", "stale", never "verified"/"online".
- **Radio text is never `template.HTML`** (rule 5). In the new map, node names ride as GeoJSON properties and are written to the DOM as *text*, never as inner HTML; the `template.HTML` exception stays confined to `chart.go`'s numbers-only SVG.
- **Opening view and Reset** (`MON-AD-15`, D50): the map still opens on the 100 km view around `location` (Emmen in the demo) and `Reset` returns there.

## Target

- **Library:** MapLibre GL JS, vendored in `web/static/` and served same-origin — no CDN for the code, no npm build step (the same pattern as htmx).
- **Style:** OpenFreeMap Positron, loaded from `https://tiles.openfreemap.org/styles/positron`.
- **Data layer:** the located adverts become a GeoJSON source with a circle layer carrying the table's last-heard colours; clustering uses MapLibre's built-in cluster source (numbered badges), with spiderfy kept for dots that still overlap at the deepest zoom.
- **Attribution:** `© OpenFreeMap © OpenStreetMap contributors` — required by the tiles' terms, shown on the map.
- **Offline:** a tile-load failure shows an honest "basemap unavailable" note; the dots still draw.

## Rework slices

Work in this order; test first for anything server-side, check in `-demo` in a browser.

1. **Vendor MapLibre GL JS.** Add the prebuilt `maplibre-gl.js` and `maplibre-gl.css` to `web/static/`, serve them same-origin, and relax the CSP for the map only: allow OpenFreeMap's tile (and glyph/sprite) hosts in `connect-src`/`img-src`, plus `worker-src blob:` for MapLibre's workers.
2. **Swap the basemap.** Replace the `<use href="/static/basemap.svg">` surface with a MapLibre container pointed at the Positron style, with attribution. Remove the generated basemap asset and `scripts/gen-basemap.py` once nothing else needs them.
3. **Re-plumb the dots.** Emit the located adverts as GeoJSON (id = row number, coordinates, last-heard class); a circle layer draws them. No-fix and unlocated adverts stay counted, never in the GeoJSON.
4. **Clustering and spiderfy.** Use MapLibre's cluster source for numbered badges (constant screen size), zoom-to-bounds on activation, and spiderfy at the deepest zoom so overlapping dots stay clickable. Drop the bespoke SVG union-find and the city-label engine.
5. **Flat overlay follows the map.** Keep the inspector cards, zoom/reset and chips as flat DOM (unchanged tokens from D55), now positioned against MapLibre's camera instead of the SVG `viewBox`. `Copy Public Key` and `Draw Line to Base` keep working.
6. **Honest offline and attribution.** Tile failure → "basemap unavailable" note while the dots still draw; attribution always present.
7. **Docs and tests.** Amend `requirements/Adverts.md` (`MON-AD-6`, `MON-AD-8`, `MON-AD-15`), record `D57` in `DECISIONS.md`, and update the Adverts tests to pin the OpenFreeMap surface and attribution instead of `/static/basemap.svg`.

## Files

| Path | What changes here |
|---|---|
| `web/static/maplibre-gl.js`, `web/static/maplibre-gl.css` | New vendored MapLibre GL JS (no build step) |
| `web/static/adverts-map.js` | MapLibre init, GeoJSON layer, clustering/spiderfy, camera → cards/chips; the bespoke SVG camera/cluster code is removed |
| `web/static/kammon.css` | Map container and flat overlay tokens (kept from D55), attribution and offline-note styles |
| `web/templates/pages/adverts.html` | Map surface + attribution + offline-note markup; GeoJSON emitted server-side with names still escaped |
| `internal/web/adverts.go` | Serve the located adverts as GeoJSON and keep the store read; drop the basemap SVG render |
| `internal/web/server.go` | CSP: allow OpenFreeMap's tile hosts and MapLibre workers for the map only |
| `scripts/gen-basemap.py`, `internal/web/basemap_*.go` | Removed with the SVG basemap (city-label engine included) |
| `docs/requirements/Adverts.md` | `MON-AD-6`, `MON-AD-8`, `MON-AD-15` and the map section now describe the OpenFreeMap Positron basemap |
| `docs/DECISIONS.md` | `D57` records this change; D41/D42/D49 are superseded where they conflict |
| `docs/PLAN.md` | Slice 8 line updated |

## Guardrails

1. **The map is the one online exception.** The library stays vendored and same-origin; the only third-party request is the basemap tiles and their glyphs (D57).
2. **No build step.** MapLibre ships a prebuilt file; nothing is compiled or bundled.
3. **Flat operator-console standard** (D55): no cards, gradients, glows or shadows; radius 6 px; blue for headings/actions only.
4. **Only valid advertised positions are plotted**; nothing is placed by hop count (`MON-AD-6/7`).
5. **Radio text is never HTML.** Node names are written as text, never `template.HTML` (rule 5); the only `template.HTML` stays `chart.go`.
6. **Honest labels** (`MON-AD-10`): "advertised", "last heard", "stale", never "verified"/"online"; a tile outage is said in words.
7. **Bounded memory.** The GeoJSON is the same bounded `store.Adverts()` snapshot, never a second unbounded copy (DataStore §3).

## Open questions

- **Theme.** Positron is light; the default KAMmon theme is dark charcoal. Confirm the light basemap is the wanted look (it is the deliberate choice), and whether the dark theme should keep Positron or offer OpenFreeMap's dark style as a switch. Dots and cards must stay legible over the light basemap.
- **Attribution placement and hosts.** MapLibre also fetches glyphs and sprites; confirm the exact OpenFreeMap hosts to allow in the CSP.
- **Self-hosting.** OpenFreeMap can be self-hosted; note whether that is a later option without changing the plan now.

## How to implement

1. Add/amend the requirement ids and record D57 first.
2. Implement test-first for server-side changes (GeoJSON, CSP, attribution); check every slice in `go run ./cmd/kammon -demo` in a browser (both themes).
3. Keep `go test -race ./...`, `go vet ./...` and `gofmt -l .` clean.
4. Tick [PLAN.md](PLAN.md) and commit citing the requirement id in the message.
