# KAMmon Requirements: Adverts Page

**Status:** Implemented 2026-11-22 (page, plan, map, copyable text); needs the KAM advert inventory (`GET /v1/adverts`, API-AD-1..5).
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
| **Plan** | The map. Solid dots are advertised positions; hollow dots are probable placements by hop band. A legend, the configured own position and the band assumption sit under it. |
| **Nodes table** | `#`, name (or "Unnamed advert"), role flags, short public key with the full key on hover, hops (`direct`, `1 hop`, `N hops`), last heard with the activity indicator, first heard (or "unknown"), clock drift, and the position as an OpenStreetMap link. |
| **Plan text** | A copyable plain-text description: header, counts, one line per node, notes. |
| **Footnotes** | Names, roles and positions are what each node advertised; they are not verified identity or proof. Hop count describes the packet that delivered the newest advert. |

### 3.1 Map

- The basemap is a Natural Earth 1:110m land outline and country borders, embedded in the binary (public domain, `scripts/gen-basemap.py`). Drawing is server-side inline SVG in an equirectangular projection, fitted to the nodes and padded.
- A node that advertised `has_location` is plotted at its coordinate.
- A node without a position is drawn on a hop band around `location` from `kammon.yml`: hop 0 in the 0–15 km band, hop 1 in 15–30 km, and so on to hop 4 and beyond. The dots spread by a fixed angle so they do not stack. Bands assume at most 15 km per hop.
- Without `location`, unlocated nodes stay in the table only and the page says how to place them.
- SVG text is built from numbers only (the chart rule); names never enter the SVG.

## 4. Requirements

| ID | Requirement | Level |
|---|---|---|
| MON-AD-1 | `/adverts` is a page for every role and reads the store only; opening it makes no call to KAM. `GET /ui/adverts` serves the live region; a plain link redirects to the page. | MUST |
| MON-AD-2 | The poller reads `GET /v1/adverts` every `poll.adverts` (default 60 s). A failure keeps the previous snapshot and marks only this page's data stale. | MUST |
| MON-AD-3 | The store keeps at most `MaxAdvertNodes` (1000) entries; when KAM or the cap cuts the list short, the page says so. The page never keeps unbounded data (MON-DS-18). | MUST |
| MON-AD-4 | The table shows `#`, name or "Unnamed advert", role flags, short key with the full key available, hops, last heard with the activity indicator, first heard or "unknown", clock drift, and position or "Not advertised". Rows are newest heard first. | MUST |
| MON-AD-5 | The metric strip shows nodes stored, how many advertised a position, repeaters (caption: room servers) and nodes silent over 24 hours. | MUST |
| MON-AD-6 | The map draws the embedded outline, solid dots at advertised positions and hollow dots at probable band positions, numbered like the table. The SVG carries no text from the radio. | MUST |
| MON-AD-7 | Hop bands are drawn only when `location` is configured, at 15 km per hop up to hop 4 and beyond. The page states the assumption. Without `location`, the page says nodes without a position are not plotted. | MUST |
| MON-AD-8 | A located node links to `https://www.openstreetmap.org/` in a new tab. The page itself loads no tiles and makes no third-party request. | MUST |
| MON-AD-9 | "Copy plan" copies the plain-text plan: header and generation time, the counts, one line per node, and the notes. The text is descriptive only; it contains no recommendation. | MUST |
| MON-AD-10 | Labels are honest: nickname is "as advertised", not identity; position is advertised, not measured; hop count belongs to the stored advert; first heard is "unknown" when KAM has no row value; cut lists are labelled. | MUST |
| MON-AD-11 | Viewers and admins both see the page: advert names are public over the air, as on the Traffic page (§6). | MUST |
| MON-AD-12 | Names and any other radio text are escaped by the template; `template.HTML` stays confined to the numbers-only SVG (a test enforces this). | MUST |
| MON-AD-13 | The live region refreshes about once a minute and survives the swap, including the copy button. | SHOULD |
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
- Unlocated nodes become hollow (probable) dots with bands when `location` is set; without it they stay in the table and the note explains why.
- Radio text in a nickname is escaped.
- The partial answers htmx and a plain request redirects to `/adverts`.
- Empty state, missing snapshot and store freshness.
- Store cap and truncation reporting.
- Strict fixture decode and the HTTP reader for `GET /v1/adverts`.

## 8. Open points

- Whether to add a per-node advert frequency view. It needs a reception-history table in KAM, which is deliberately not in the current contract.
- Whether to draw relay edges from `hop_stations`. One-byte node hashes collide, so edges would be "probable" and often ambiguous.
- Whether the map should use Web Mercator instead of equirectangular for very large regions; at country scale the difference is small.
