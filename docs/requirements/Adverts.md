# KAMmon Requirements: Adverts Page

**Status:** Implemented 2026-11-22 (page, plan, map, copyable text); needs the KAM advert inventory (`GET /v1/adverts`, API-AD-1..5). Map reworked 2026-02-14: a 1:50m Web Mercator basemap served as one asset, a country-level default frame, and no-fix handling (D42, D43; [research/advert-map-basemap.md](../research/advert-map-basemap.md)).
**Scope:** A read-only inventory of the nodes KAM has heard advertising, plus a descriptive plan: an inventory table, a map of advertised positions, and a plain-text plan the operator can copy. Design: [DESIGN.md](../DESIGN.md) §4. Data: [DataStore.md](DataStore.md), KAM contract §7.7.

## 1. Summary

A MeshCore advert is a node announcing who it is: a nickname, role flags, sometimes a position, relayed over the mesh. KAM stores the newest advert per public key for 30 days by default (`adverts.keep_days` / `adverts.max_kept`). KAMmon reads that inventory with `GET /v1/adverts` and shows:

- **what is out there** — every stored node, newest heard first, with its advertised name, roles, position, hops and clock drift;
- **where it is** — an offline map of advertised positions, with nodes that advertised no position placed at a probable distance by hop count;
- **the plan** — the same facts as plain text, copyable in one click.

The page makes **no recommendations**. It records what the adverts say, and each claim carries its own limit. Nothing is verified, and a nickname is not an identity.

Plain words: **advert** means a node announcing itself. **Hop count** is how many repeaters the advert's packet passed; 0 means KAM heard the node directly. **Advertised position** is a coordinate the node chose to include, not a measured location.

## 2. Goals and non-goals

**Goals**

- GL-1. Show every stored advert, honestly labelled, without opening KAM's database.
- GL-2. Draw the advertised positions on a local map that works without internet.
- GL-3. Place nodes without a position by their hop distance when the operator gave `location` in `kammon.yml`.
- GL-4. Offer the whole picture as copyable plain text.

**Non-goals**

- Advice, scoring, ranking or "you should" lines.
- Advert frequency or interval history: KAM keeps the newest advert per node, not a reception log.
- Editing adverts or triggering one from this page.
- Online map tiles or any third-party request from the page; OpenStreetMap is only a link.
- Treating a nickname as identity or a position as proof.

## 3. What the page shows

| Area | Content |
|---|---|
| **Metrics** | Nodes stored, how many advertised a position, repeaters (with room servers as the caption), and nodes silent for over 24 hours. |
| **Map** | The map. Solid dots are advertised positions; hollow dots are probable placements by hop band. Hovering or clicking a dot opens the advert's card beside it; a legend, the configured own position and the band assumption sit under it. |
| **Nodes table** | `#`, name (or "Unnamed advert"), role flags, short public key with the full key on hover, hops (`direct`, `1 hop`, `N hops`), last heard with the activity indicator, first heard (or "unknown"), clock drift, and the position as an OpenStreetMap link. |
| **Plan text** | A copyable plain-text description: header, counts, one line per node, notes. |
| **Footnotes** | Names, roles and positions are what each node advertised; they are not verified identity or proof. Hop count describes the packet that delivered the newest advert. |

### 3.1 Map

- The basemap is Natural Earth 1:50m land, country borders and lakes (public domain, `scripts/gen-basemap.py`), projected once at generate time to Web Mercator and shipped as one SVG asset: `internal/web/basemap_svg.go` serves it at `/static/basemap.svg` with an asset-version ETag, and the map draws it with a single same-origin `<use>` reference (D42). Paths are simplified within a 0.012-unit corridor (0.005 for lakes), so the detail survives down to the 16× frame. The page makes no third-party request.
- The frame defaults to about 260 km around `location` from `kammon.yml`, never wider than the holding country's own outline: the generator embeds Natural Earth 1:110m admin-0 polygons and tiny-country points, and the page clamps the 200 km window to the ring or window that holds the configured location — metropolitan France, not the whole sovereignty; a country the 110m polygons leave out (Malta, Monaco, Singapore…) is the ±0.5° window around its tiny-country point. Without `location`, or when no outline matches, the frame fits the stored nodes and says so (D43).
- A node that advertised `has_location` is plotted at its coordinate; a coordinate within ±0.01° of 0°N 0°E is a missing GPS fix: never plotted, never shaping the frame, counted in the notes. The table keeps the advertised row (D43).
- Nodes outside the frame are counted, never drawn; the frame never widens for them.
- A node without a position is drawn on a hop band around `location` from `kammon.yml`: hop 0 in the 0–15 km band, hop 1 in 15–30 km, and so on to hop 4 and beyond; a band past the frame is clipped, and the frame never widens for it. The dots spread by a fixed angle so they do not stack, and two nodes at one spot step aside deterministically. Bands assume at most 15 km per hop.
- Without `location`, unlocated nodes stay in the table only and the page says how to place them.
- The `+`/`−` control multiplies or divides the frame ×2, from a ¼ frame to 16×; the zoom rides `?mapzoom=` in the live region's URL, so the 60-second refresh keeps it, and the frame's centre stays put (D44).
- Cities of Natural Earth's 10m populated places (the full list) draw as small dots with their names, filtered to the frame, most major first, a minimum label distance apart, at most 24 per frame (D45).
- Dots carry the table's last-heard colour (fresh, recent, older, stale). A scale bar, exact at the home latitude (or the frame centre without `location`), and a degree grid are drawn; their figures are said in words below the map.
- Radio text never enters the SVG (the chart rule): the only names on it are vendored Natural Earth city names, generated data alongside the roads and coastlines. Hover and click cards are escaped HTML beside the SVG (`web/static/map.js`, D46).

## 4. Requirements

| ID | Requirement | Level |
|---|---|---|
| MON-AD-1 | `/adverts` is a page for every role and reads the store only; opening it makes no call to KAM. `GET /ui/adverts` serves the live region; a plain link redirects to the page. | MUST |
| MON-AD-2 | The poller reads `GET /v1/adverts` every `poll.adverts` (default 60 s). A failure keeps the previous snapshot and marks only this page's data stale. | MUST |
| MON-AD-3 | The store keeps at most `MaxAdvertNodes` (1000) entries; when KAM or the cap cuts the list short, the page says so. The page never keeps unbounded data (MON-DS-18). | MUST |
| MON-AD-4 | The table shows `#`, name or "Unnamed advert", role flags, short key with the full key available, hops, last heard with the activity indicator, first heard or "unknown", clock drift, and position or "Not advertised". Rows are newest heard first. | MUST |
| MON-AD-5 | The metric strip shows nodes stored, how many advertised a position, repeaters (caption: room servers) and nodes silent over 24 hours. | MUST |
| MON-AD-6 | The map draws the embedded Natural Earth basemap by one same-origin `<use>` reference, frames the country at the configured `location` (the containing ring; a tiny-country point when the polygons have none; the stored nodes when `location` is missing or unmatched), and draws solid dots at advertised positions and hollow dots at probable band positions, each dot announcing its row number to assistive tech without a printed label. Adverts within ±0.01° of 0°N 0°E are not plotted and nodes outside the frame are not drawn; both are counted in the notes. The SVG carries no text from the radio. | MUST |
| MON-AD-7 | Hop bands are drawn only when `location` is configured, at 15 km per hop up to hop 4 and beyond; a band past the frame is clipped and the frame never widens for it. The page states the assumption. Without `location`, the page says nodes without a position are not plotted. | MUST |
| MON-AD-15 | The map offers `+`/`−` zoom controls stepping ×2, from a ¼ frame to 16×; the zoom rides `?mapzoom=` in the live region's URL so the poll keeps it, a bound disables its button, and words name the zoomed width. The default frame is about 260 km around `location`, never wider than the country's ring. | MUST |
| MON-AD-8 | A located node links to `https://www.openstreetmap.org/` in a new tab. The page itself loads no tiles and makes no third-party request. | MUST |
| MON-AD-9 | "Copy plan" copies the plain-text plan: header and generation time, the counts, one line per node, and the notes. The text is descriptive only; it contains no recommendation. | MUST |
| MON-AD-10 | Labels are honest: nickname is "as advertised", not identity; position is advertised, not measured; hop count belongs to the stored advert; first heard is "unknown" when KAM has no row value; cut lists are labelled. | MUST |
| MON-AD-11 | Viewers and admins both see the page: advert names are public over the air, as on the Traffic page (§6). | MUST |
| MON-AD-12 | Names and any other radio text are escaped by the template; `template.HTML` stays confined to the server-drawn SVG in `chart.go`, whose text is numbers or vendored Natural Earth city names — never radio text (a test enforces this). The dots carry no printed labels. | MUST |
| MON-AD-13 | The live region refreshes about once a minute and survives the swap, including the copy button. | SHOULD |
| MON-AD-16 | Hovering or focusing a dot opens the advert's card beside the map — mini (name, roles, hops, last heard on hover or focus) and full when pinned by click or Enter (position with OpenStreetMap link, public key, first heard, clock). The cards are server-escaped HTML beside the number-only SVG; `web/static/map.js` delegates the events, so the live refresh keeps them; any other click or Escape un-pins. | MUST |
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
- The map frames about 260 km around `location` (clamped to the containing ring), falls back to the node fit without `location`, treats a 0°N 0°E advert as no fix, counts nodes outside the frame, and `/static/basemap.svg` answers with the asset-version ETag and a 304 on revalidation.
- Zoom steps: the words, the next-step URLs, the disabled bounds, and the zoom carried in the live region's URL.
- Dots carry no printed labels, announce their row number, and the per-advert card exists beside the map with the advert's details.
- Unlocated nodes become hollow (probable) dots with bands when `location` is set; without it they stay in the table and the note explains why.
- Radio text in a nickname is escaped.
- The partial answers htmx and a plain request redirects to `/adverts`.
- Empty state, missing snapshot and store freshness.
- Store cap and truncation reporting.
- Strict fixture decode and the HTTP reader for `GET /v1/adverts`.

## 8. Open points

- Whether to add a per-node advert frequency view. It needs a reception-history table in KAM, which is deliberately not in the current contract.
- Whether to draw relay edges from `hop_stations`. One-byte node hashes collide, so edges would be "probable" and often ambiguous.
- Whether the map should use Web Mercator instead of equirectangular — closed 2026-02-14: the map now projects Web Mercator, and the frame defaults to the operator's country (D42, D43).
