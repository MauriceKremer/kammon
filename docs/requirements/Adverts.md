# KAMmon Requirements: Adverts Page

**Status:** Implemented 2026-11-22 (page, plan, map, copyable text); needs the KAM advert inventory (`GET /v1/adverts`, API-AD-1..5). Map reworked 2026-02-14: a 1:50m Web Mercator basemap served as one asset, a country-level default frame, and no-fix handling (D42, D43; [research/advert-map-basemap.md](../research/advert-map-basemap.md)). Map camera reworked 2026-11-22: the map opens on a 100 km view around the located nodes' centre and pans and zooms client-side (D47, MON-AD-15). Map filtered 2026-11-22: only nodes with a valid advertised position are plotted; hop-band placement of unlocated nodes is gone (D48). Marker clustering added 2026-11-22: nearby dots become numbered badges that zoom to their bounds, and overlapping dots spiderfy at the deepest zoom (D49, MON-AD-17). Opening view and Reset 2026-11-22: the map opens and resets on the configured node (Emmen in the demo) at the 100 km radius, falling back to the located nodes only when `location` is missing (D50). City labels 2026-11-22: subtle constant-size type with a high-opacity halo, and the lower Natural Earth ranks hidden at the 100 km view, revealed as the camera closes in (D51). Cluster z-order and animated spiderfy 2026-11-22 (D52). Base marker, glass inspector cards and layer chips 2026-11-22 (D53). Local town tier 2026-11-22: Emmen, Coevorden and nearby towns are vendored alongside Natural Earth, and a generated national Dutch tier adds every populated place ≥30,000 from GeoNames (D54). Visual rework 2026-11-22: the glassmorphism look — glass inspector cards, the radar pulse and glass-tinted controls — was replaced with the flat operator-console standard (no glow, no shadow, no blur; D55, [kammon_map_feature_plan.md](kammon_map_feature_plan.md)). Unpositioned panel and reload persistence 2026-11-22: nodes with no usable position group into a collapsible panel (MON-AD-7), and the camera plus chip state persist in `localStorage` across reloads (D56, MON-AD-15). Basemap 2026-11-22: the generated Natural Earth SVG was replaced by OpenFreeMap Positron tiles drawn with MapLibre GL JS, with attribution and an honest offline note (D57, [kammon_map_feature_plan.md](kammon_map_feature_plan.md)).
**Scope:** A read-only inventory of the nodes KAM has heard advertising, plus a descriptive plan: an inventory table, a map of advertised positions, and a plain-text plan the operator can copy. Design: [DESIGN.md](../DESIGN.md) §4. Data: [DataStore.md](DataStore.md), KAM contract §7.7.

## 1. Summary

A MeshCore advert is a node announcing who it is: a nickname, role flags, sometimes a position, relayed over the mesh. KAM stores the newest advert per public key for 30 days by default (`adverts.keep_days` / `adverts.max_kept`). KAMmon reads that inventory with `GET /v1/adverts` and shows:

- **what is out there** — every stored node, newest heard first, with its advertised name, roles, position, hops and clock drift;
- **where it is** — a map of the positions nodes advertised on the hosted OpenFreeMap Positron basemap, filtered to valid GPS fixes;
- **the plan** — the same facts as plain text, copyable in one click.

The page makes **no recommendations**. It records what the adverts say, and each claim carries its own limit. Nothing is verified, and a nickname is not an identity.

Plain words: **advert** means a node announcing itself. **Hop count** is how many repeaters the advert's packet passed; 0 means KAM heard the node directly. **Advertised position** is a coordinate the node chose to include, not a measured location.

## 2. Goals and non-goals

**Goals**

- GL-1. Show every stored advert, honestly labelled, without opening KAM's database.
- GL-2. Draw the advertised positions on a clean, familiar basemap — OpenFreeMap Positron.
- GL-3. Keep the map geographic and exact: plot only nodes that advertised a valid position; leave nodes without one in the table, counted but not drawn.
- GL-4. Offer the whole picture as copyable plain text.

**Non-goals**

- Advice, scoring, ranking or "you should" lines.
- Advert frequency or interval history: KAM keeps the newest advert per node, not a reception log.
- Editing adverts or triggering one from this page.
- An offline basemap: the map's background is OpenFreeMap Positron, and when the tiles cannot load the page says so (the dots still draw from the store).
- Treating a nickname as identity or a position as proof.

## 3. What the page shows

| Area | Content |
|---|---|
| **Metrics** | Nodes stored, how many advertised a position, repeaters (with room servers as the caption), and nodes silent for over 24 hours. |
| **Map** | The map. Solid dots are nodes with a valid advertised position; nodes without one are not plotted. Hovering or clicking a dot opens the advert's card beside it; a legend, the configured own position and the count of nodes left off sit under it. |
| **Nodes table** | `#`, name (or "Unnamed advert"), role flags, short public key with the full key on hover, hops (`direct`, `1 hop`, `N hops`), last heard with the activity indicator, first heard (or "unknown"), clock drift, and the position as an OpenStreetMap link. |
| **Plan text** | A copyable plain-text description: header, counts, one line per node, notes. |
| **Footnotes** | Names, roles and positions are what each node advertised; they are not verified identity or proof. Hop count describes the packet that delivered the newest advert. |

### 3.1 Map

- The basemap is OpenFreeMap's hosted **Positron** style (`https://tiles.openfreemap.org/styles/positron`), drawn in the browser with MapLibre GL JS vendored in `web/static/` (no CDN for the code, no build step). It is the page's one third-party request, and the required attribution `© OpenFreeMap © OpenStreetMap contributors` is shown on the map (D57).
- The map opens on a view of 100 km radius centred on the configured node (`location` in kammon.yml; Emmen, 52.77920 N 6.90690 E in the demo). Without `location` it centres on the mean latitude and longitude of the nodes that advertised a position, and without either it opens on a whole-world view (D50).
- A node that advertised `has_location` is plotted at its coordinate; a coordinate within ±0.01° of 0°N 0°E is a missing GPS fix: never plotted, never shaping the view, counted in the notes. The table keeps the advertised row (D43).
- Only nodes with a valid advertised position are plotted. A node without one stays in the table and is counted in the notes; nothing is guessed onto the map, so every dot is a coordinate the node itself chose (D48).
- The map is fully interactive (`web/static/adverts-map.js`, D47): drag or touch pans, two fingers pinch, the scroll wheel zooms where the pointer is, `+`/`−` step the zoom ×2, and `Reset` glides back to the opening view — the configured node's 100 km view. The camera is MapLibre's, kept in memory and replayed after the 60-second refresh, and the cards beside the map follow each move. The camera and the chip state also persist in `localStorage` and are restored on reload when the frame matches; the URL carries no camera state (D56).
- Places are labelled by the Positron style itself, so the vendored Natural Earth local/Dutch city tiers and their label engine are removed (D45, D51, D54 superseded).
- Dots carry the table's last-heard colour (fresh, recent, older, stale). A scale bar, exact at the home latitude, is shown; its figures are said in words below the map.
- Dots nearer than about 42 screen pixels group into a numbered cluster badge (MapLibre's cluster source). Activating a badge glides the view into the group's bounds, and at the deepest zoom dots that still overlap spiderfy apart with a short animation and a leg back to the common spot, so every node stays an ordinary clickable dot. Markers keep one screen size (D49, D52, MON-AD-17).
- The configured base node (`location` in kammon.yml; Emmen in the demo) draws as a bold neutral anchor — a distinct, non-status marker with no state colour and no radar pulse. It keeps one screen size, sits above the cluster layer, and can be hidden by its own layer chip (MON-AD-19, D55).
- Hovering a dot opens a flat inspector card with the node name, public-key prefix, role, hops from base, colour-coded last heard and the base radio frequency; clicking pins it and reveals the position, first heard, clock, `Copy Public Key` and `Draw Line to Base`. One base line is drawn at a time (D53, MON-AD-16).
- The map controls are flat buttons — 1 px border, 6 px radius, no shadow or blur. The legend is a set of accessible flat toggle chips — Advertised (N), Silent (N) and Base Node — with counts; Silent starts off, so stale nodes are hidden until the operator asks for them (D53, MON-AD-20).
- Radio text never enters `template.HTML` (the chart rule, rule 5): node names ride as GeoJSON properties and are written to the DOM as text, never as inner HTML. The only `template.HTML` stays the numbers-only SVG in `chart.go`. Hover and click cards are escaped HTML beside the map (`web/static/map.js`, D46).

## 4. Requirements

| ID | Requirement | Level |
|---|---|---|
| MON-AD-1 | `/adverts` is a page for every role and reads the store only; opening it makes no call to KAM. `GET /ui/adverts` serves the live region; a plain link redirects to the page. | MUST |
| MON-AD-2 | The poller reads `GET /v1/adverts` every `poll.adverts` (default 60 s). A failure keeps the previous snapshot and marks only this page's data stale. | MUST |
| MON-AD-3 | The store keeps at most `MaxAdvertNodes` (1000) entries; when KAM or the cap cuts the list short, the page says so. The page never keeps unbounded data (MON-DS-18). | MUST |
| MON-AD-4 | The table shows `#`, name or "Unnamed advert", role flags, short key with the full key available, hops, last heard with the activity indicator, first heard or "unknown", clock drift, and position or "Not advertised". Rows are newest heard first. | MUST |
| MON-AD-5 | The metric strip shows nodes stored, how many advertised a position, repeaters (caption: room servers) and nodes silent over 24 hours. | MUST |
| MON-AD-6 | The map draws the OpenFreeMap Positron basemap (MapLibre GL JS vendored in `web/static/`, attribution shown), and draws solid dots only at valid advertised positions, each dot announcing its row number to assistive tech without a printed label. Adverts within ±0.01° of 0°N 0°E and adverts without a position are not plotted; each is counted in the notes. The map markup carries no text from the radio. | MUST |
| MON-AD-7 | Nodes without a valid advertised position are never placed by hop count or any other estimate. They stay in the table, and the page states that they are not plotted. | MUST |
| MON-AD-15 | The map is fully interactive: it opens on a view of 100 km radius centred on the configured node (`location`; the mean of the nodes that advertised a position when `location` is missing; a whole-world view without either); drag or touch pans, two fingers pinch, the scroll wheel zooms where the pointer is, `+`/`−` buttons step the zoom ×2, and `Reset` glides smoothly back to that opening view; a bound disables its button, and the camera survives the 60-second refresh; the camera and the Advertised/Silent/Base chip state are also restored on reload from `localStorage` when the frame matches, while the URL carries no camera state (D56). | MUST |
| MON-AD-8 | The basemap is OpenFreeMap Positron, shown with the attribution `© OpenFreeMap © OpenStreetMap contributors`. A located node still links to `https://www.openstreetmap.org/` in a new tab. When the tiles cannot load, the page says the basemap is unavailable and the store-fed dots still draw. | MUST |
| MON-AD-9 | "Copy plan" copies the plain-text plan: header and generation time, the counts, one line per node, and the notes. The text is descriptive only; it contains no recommendation. | MUST |
| MON-AD-10 | Labels are honest: nickname is "as advertised", not identity; position is advertised, not measured; hop count belongs to the stored advert; first heard is "unknown" when KAM has no row value; cut lists are labelled. | MUST |
| MON-AD-11 | Viewers and admins both see the page: advert names are public over the air, as on the Traffic page (§6). | MUST |
| MON-AD-12 | Names and any other radio text are escaped by the template; `template.HTML` stays confined to the server-drawn SVG in `chart.go`, whose text is numbers or vendored static names — never radio text (a test enforces this). Node names in the map are written to the DOM as text, never as HTML. The dots carry no printed labels. | MUST |
| MON-AD-13 | The live region refreshes about once a minute and survives the swap, including the copy button. | SHOULD |
| MON-AD-16 | Hovering or focusing a dot opens a flat inspector card beside the map — name, public-key prefix, role (Repeater, Room Server, Client), hops from base, colour-coded last heard and the base radio frequency — with the position, first heard and clock; clicking pins it and reveals `Copy Public Key` and `Draw Line to Base`. The cards are server-escaped HTML beside the map; `web/static/map.js` delegates the events, so the live refresh keeps them; any other click or Escape un-pins. | MUST |
| MON-AD-17 | Dots closer than a screen threshold group into number badges; badges and their hit areas sit behind the node circles and their members are hidden, so a badge never covers a standard dot. Activating a badge smoothly zooms the camera into the group's bounds, and at the deepest zoom dots that still overlap glide out with a smooth spiral animation and legs back to their common spot so each stays clickable. Grouping follows the camera, and markers keep a constant screen size. The badges carry counts only, never radio text. | MUST |
| MON-AD-19 | The configured base node draws as a bold neutral anchor — a distinct, non-status marker with no state colour and no pulse — at a constant screen size, above the cluster layer. A `Base Node` layer chip can hide it. | SHOULD |
| MON-AD-20 | Map controls (`+`, `−`, `Reset`) use flat buttons — 1 px border, 6 px radius, no shadow or blur — that follow the dark/light tokens. The map legend is a set of accessible flat toggle chips showing counts for Advertised, Silent and Base Node; Silent starts off and each chip toggles its layer. | SHOULD |
| MON-AD-18 | Place labels are provided by the Positron basemap and stay visually subordinate to the node dots (the dots are the subject). The vendored Natural Earth local/Dutch city tiers and their label engine are removed (D57). | SHOULD |
| MON-AD-14 | Empty states say "Waiting for KAM…", "Needs a newer KAM…" after a 404, or "Adverts are unavailable right now." when the snapshot is down. | MUST |

## 5. What KAMmon needs from KAM

| Need | Source | KAM requirement |
|---|---|---|
| Stored adverts, one per public key, newest heard first | `GET /v1/adverts?limit=` | API-AD-1 |
| Public key, node hash, nickname, role flags | The same record | API-AD-1 |
| Advertised position, or `has_location: false` | `has_location`, `latitude`, `longitude` | API-AD-1 |
| Hop count and relay path hashes of the stored advert | `hop_count`, `hop_stations` | API-AD-1 |
| First and last reception, advert clock and drift | `first_received_at`, `last_received_at`, `advert_timestamp`, `time_drift_seconds` | API-AD-1 |
| No secret, no raw payload, no ingestion cost | The route reads the existing table only | API-AD-2, API-AD-3 |

## 6. Visibility

| Element | Viewer | Admin |
|---|---|---|
| Every advert row, name, key, hops, times, drift | Yes | Yes |
| Position and OpenStreetMap link | Yes | Yes |
| Map and plan text | Yes | Yes |

Advert names are public over the air and already visible to viewers on the Traffic page (Traffic.md §6). The page is read-only, so it adds no write path.

## 7. Tests

- The page shows the inventory, map, legend, OpenStreetMap link, copy button and plan text for a viewer.
- The map draws the OpenFreeMap Positron basemap with its attribution, opens on the 100 km view around `location`, falls back to the located nodes without `location`, treats a 0°N 0°E advert as no fix, and counts nodes without a valid position. Radio text in a nickname is escaped and never enters the map markup.
- The opening view: the 100 km view is centred on the configured node (Emmen in the demo), `Reset` glides back to it, and the camera carries no URL state but persists in `localStorage` (D56) — the pan and zoom live in `web/static/adverts-map.js` and are checked in the browser in demo mode (drag with momentum, pinch, wheel, buttons, reset, card following, the camera replayed after a refresh, and the camera plus chip state restored after a reload).
- Dots carry no printed labels, announce their row number, and the per-advert card exists beside the map with the advert's details.
- Place labels come from the Positron style itself; the vendored Natural Earth local/Dutch city tiers and their `data-rank` markup are removed.
- Clustering: nearby dots become a numbered badge, a badge activation zooms into its members' bounds, badges/hit areas sit behind the dots, and at maximum zoom overlapping dots glide out into a spider with legs; markers stay one screen size. Checked in the browser in demo mode (the Go tests pin the legend and note; the grouping itself is camera state).
- Base marker, inspector fields/actions and layer chips: checked in the browser in demo mode (base anchor present with no state colour or pulse, card shows role/hops/last heard/frequency, Copy and Draw Line work, chips hide their layers, and no card or control casts a shadow or uses blur).
- Only located nodes become dots; nodes without a valid advertised position stay in the table, are counted in the notes and never appear as a guessed placement, with or without `location` configured.
- The map plots only valid advertised positions; a no-fix or absent position is skipped and counted.
- Radio text in a nickname is escaped.
- The partial answers htmx and a plain request redirects to `/adverts`.
- Empty state, missing snapshot and store freshness.
- Store cap and truncation reporting.
- Strict fixture decode and the HTTP reader for `GET /v1/adverts`.

## 8. Open points

- Whether to add a per-node advert frequency view. It needs a reception-history table in KAM, which is deliberately not in the current contract.
- Whether to draw relay edges from `hop_stations`. One-byte node hashes collide, so edges would be "probable" and often ambiguous.
- Whether the map should use Web Mercator instead of equirectangular — closed 2026-02-14: the map now projects Web Mercator, and the frame defaults to the operator's country (D42, D43).
