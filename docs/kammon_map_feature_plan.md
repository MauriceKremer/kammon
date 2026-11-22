# KAMmon Map — Rework Plan

This is the plan for **reworking the map on the Adverts page** (`/adverts`) to meet the KAMmon operator-console standard. The map already works; the rework is about *how it looks and reads*, not about adding a second rendering engine.

**Status:** open — rework of the shipped Adverts map (Slice 8 in [PLAN.md](PLAN.md)).

## Why this rework exists

The shipped map adopted a "glassmorphism" look that breaks the design system. The standard is [DESIGN.md](DESIGN.md) §5 and the short version in [`.github/skills/kammon-ui-style/SKILL.md`](../.github/skills/kammon-ui-style/SKILL.md): **no cards, gradients, glows or shadows (the toast is the only shadow)**, radius 6 px, 1 px rules, and **blue for headings/actions only — green/amber/red mean a state**. The map currently diverges:

| Where | Now (non-standard) | Standard |
|---|---|---|
| `.map-card` (inspector) | `backdrop-filter: blur(14px)`, translucent fill, `box-shadow`, radius 14 px | flat `--bg-2` panel, 1 px `--border-default`, radius 6 px, no blur/shadow |
| `.map-zoom button` (`+ − Reset`) | `backdrop-filter: blur(8px)`, translucent fill, `box-shadow`, radius 10 px | flat `--bg-2` control, radius 6 px, no blur/shadow |
| `.map-chip` (layer toggles) | pill radius 999 px, translucent fill, `box-shadow` | radius 6 px, flat `--bg-2`, no shadow |
| `.map-home-ring` / `map-home-pulse` | animated radar pulse = a glow | no glow; a static ring or none |
| `.map-home-star` (base node) | green `--success` | green means a *state*; the base node is an anchor, so it needs a non-status treatment |

Two things the earlier draft of this plan asked for are also out of scope for the same reason: a hosted **OpenFreeMap Positron** basemap (violates offline/no-tiles, `MON-AD-8`, and "charts are server-drawn SVG"), and **glassmorphism** popups (violates the no-cards/no-shadows rule).

## What stays (baseline — do not regress)

The rework is restyling, not re-architecture. These already meet the standard and must keep working:

- **Offline server-drawn basemap** — Natural Earth 1:50m land/borders/lakes, Web Mercator, one generated SVG asset (`internal/web/basemap_svg.go`) served at `/static/basemap.svg`, drawn with a same-origin `<use>` (D42, `MON-AD-6`). No tiles, no third-party requests.
- **Country-level frame** — about 260 km around `location` in `kammon.yml`, clamped to the containing ring; node-fit fallback (D43, D44, `MON-AD-6`).
- **Strict GPS filtering** — only valid advertised positions become dots; no-fix (0°N 0°E) and unlocated adverts are counted, not drawn, never placed by hop count (D48, `MON-AD-6/7`).
- **Interactive camera** — 100 km opening view centred on `location` (Emmen only in the demo), drag/momentum, pinch, wheel, `+ −` ×2 steps, smooth `Reset`; camera lives in JS and survives the 60 s refresh (D47, D50, `MON-AD-15`).
- **Clustering & spiderfy** — numbered badges, badge zoom-to-bounds, golden-angle spiderfy with legs, constant screen size (D49, D52, `MON-AD-17`).
- **City labels** — constant small muted type with a halo, rank-based reveal, local + Dutch tiers (D45, D51, D54, `MON-AD-18`).
- **Inspector content** — name, key prefix, role, hops from base, colour-coded last heard, base frequency, plus `Copy Public Key` and `Draw Line to Base` (the *content* stays; only the glass surface changes).
- **Honest labels & copyable plan** — "advertised position", "hops = path length of the newest advert" (`MON-AD-9/10`).

## Rework slices

Each slice is test-first for anything server-side, browser-checked in `-demo` for styling, and cited against a requirement id and a decision. Work in this order.

### 1. Flat inspector cards

Restyle `.map-card` and its `card-*` children in `web/static/kammon.css` to the standard: solid `--bg-2` (or `--bg-1` with a 1 px `--border-default`), radius 6 px, the existing type scale (13 px head, 12 px body, mono for the key), no `backdrop-filter`, no `box-shadow`, no translucent fill. Keep the card's position-follow behaviour in `web/static/map.js` untouched.

### 2. Flat map controls and layer chips

- `.map-zoom button`: flat `--bg-2`, 1 px `--border-default`, radius 6 px, no shadow/blur; keep the disabled state and `Reset`.
- `.map-chip`: radius 6 px (drop the pill), flat surface, pressed state via `--signal` tint and border as today but without the shadow. Keep counts and `aria-pressed`.

### 3. Base node without glow or state colour

Remove the `map-home-pulse` animation (and the `@keyframes`) and the green `--success` fill on `.map-home-star`/`.map-home-ring`. Render the base node as a distinct, non-status anchor — a neutral marker (`--text-primary`/`--text-secondary`) or an outlined glyph that does not read as "healthy/error" — with the existing `Base Node` chip carrying the identity. Honour `prefers-reduced-motion`. Record the chosen glyph/colour in `DECISIONS.md`.

### 4. Basemap stays offline

Keep the generated SVG basemap. If the goal is a lighter "Positron-like" background, achieve it inside `scripts/gen-basemap.py` and the CSS custom properties on `.advert-map` (`--map-land`, `--map-border`, `--map-lake`), **not** by switching to OpenFreeMap or any hosted tile service.

### 5. Review dot status colour semantics

Dots encode last-heard state by colour (`fresh` = success, `recent` = signal, `older`/`unknown` = muted, `stale` = dotted). Confirm this reads as the activity indicator (the card and table already give the word) and that "recent" is not using blue as a *status* in a way that collides with "blue = headings/actions". If a state needs a distinct token, add it; meaning must never depend on colour alone.

### 6. Unpositioned-nodes panel (optional)

Nodes without a valid position already appear in the Nodes table and are counted in the map notes. A collapsible panel grouping exactly those rows (search/sort by name, key, hops, last heard) would save scrolling; it must reuse `store.Adverts()` and never place or estimate them onto the map (`MON-AD-7`). The count is dynamic, not "3".

### 7. Viewport persistence / URL sync (optional)

The camera deliberately carries no URL state today (D47). Persisting pan/zoom/chip state to `localStorage` or `?lat=&lon=&zoom=` is a possible convenience, but must keep the server-drawn-frame model and the 60 s refresh replay, and must never put radio text or keys into the URL.

### 8. Relay-path overlay (open, blocked on KAM)

A "mesh backhaul" or route overlay is not derivable from the advert record: `hop_stations` has no per-hop coordinates and one-byte hashes collide (Adverts.md §8). Keep as a research item; it likely needs a KAM-side contract change first. `Draw Line to Base` already covers single-node anchoring.

## Files

| Path | What changes here |
|---|---|
| `web/static/kammon.css` | `.map-card`, `.map-zoom button`, `.map-chip`, `.map-home-star`, `.map-home-ring`, `@keyframes map-home-pulse`, `.advert-map` basemap tokens |
| `web/static/adverts-map.js` | base-marker/anchor drawing if the glyph changes; no camera/cluster changes expected |
| `web/static/map.js` | unchanged (card behaviour), unless a card needs a new field |
| `web/templates/pages/adverts.html` | card markup only if structure changes; otherwise unchanged |
| `internal/web/adverts.go` | only if the server markup for the anchor/cards changes |
| `scripts/gen-basemap.py`, `internal/web/basemap_*.go` | only for slice 4 |
| `docs/requirements/Adverts.md` | amend `MON-AD-16`, `MON-AD-19`, `MON-AD-20` to the flat standard |
| `docs/DECISIONS.md` | `D55` records the flat restyle (supersedes D53's glass wording) |

## Guardrails

1. **Offline, no tiles, no third-party requests** (`MON-AD-8`). OpenStreetMap stays a link out only.
2. **Server draws the whole frame; the browser only moves a window into it** (D47, A3). No Leaflet/Supercluster/build step (D49).
3. **No cards, gradients, glows or shadows; radius 6 px; blue for headings/actions only.**
4. **Only valid advertised positions are plotted**; nothing is placed by hop count (`MON-AD-6/7`).
5. **Radio text stays out of the SVG** (`MON-AD-12`); colours come from CSS variables so theme switching needs no redraw.
6. **Honest labels** (`MON-AD-10`): "advertised", "last heard", "stale", never "verified"/"online".

## How to implement

1. Add/amend requirement ids in `requirements/Adverts.md` and a decision in `DECISIONS.md`.
2. Implement test-first for server-side changes; check every slice in `go run ./cmd/kammon -demo` in a browser (both themes).
3. Keep `go test -race ./...`, `go vet ./...` and `gofmt -l .` clean.
4. Tick [PLAN.md](PLAN.md) and commit citing the requirement id in the message.
