# Plan

Build in vertical slices. Each slice: test first for the logic, check the screen in demo mode, tick the box. Requirement ids are in [requirements/](requirements/); KAM ids (`API-*`) are in [kam-api/FeatureKamApi.md](kam-api/FeatureKamApi.md).

Status legend: `[x]` done in this scaffold, `[ ]` open.

## Slice 0: Scaffold

- [x] Repository, layout, `AGENTS.md`, CI, glossary, decisions
- [x] `config`: load and validate `kammon.yml`
- [x] `kam`: types and HTTP reader for status, events, channels, rooms, clients; fixtures decode strictly
- [x] `kamfake`: fake KAM with cursor, epoch and reset semantics, and a demo data generator
- [x] `store`: event ring, minute buckets, cursor logic, gap and restart marks, freshness (MON-DS-6..14, 16, 18)
- [x] `poll`: backfill and polling with backoff (MON-DS-1..5, 7)
- [x] `auth`: users, sessions, per-route roles (MON-AX-1, 3..5, 8, 11; the old 9 and 10 were withdrawn 2026-10-23, D31)
- [x] `view`: channel-text visibility (MON-AX-13, 14)
- [x] `web`: login, logout, shell, Overview (metrics, SVG chart, packet mix, recent packets), placeholders (MON-OV-1..4, 6, 7, 11; MON-AX-2, 11, 12)
- [x] `-demo` mode

## Slice 1: Overview complete

- [x] Range switch keeps its own URL; chart marks for gaps and KAM restarts (MON-OV-5)
- [x] Rooms block, automation block, duty-cycle warning (MON-OV-8..10, 12)
- [x] Stale-data bar polish and link state in the sidebar (MON-DS-13..17)
- [x] Fonts and icon sprite embedded (no CDN)

## Slice 2: Traffic

- [x] Filtered table from the event ring, paging, pause, CSV export (MON-TR-1..13)
- [x] A click on a row opens or closes its detail line (MON-TR-14)

## Slice 3: Channels and Rooms

- [x] Channels page (MON-CH-1..12)
- [x] Rooms page with the activity indicator and legend (MON-RM-1..14)

## Slice 4: Access hardening

- [x] Failed-login slowdown (MON-AX-6), Origin and htmx header checks on every change route (MON-AX-17, 18), optional TLS (MON-AX-21)
- [x] Theme cookie read by the server (design §7.3)

## Slice 5: KISS modem (KAM API phase 3 has shipped; see docs/kam-api/KAM_COMMIT)

- [ ] Snapshot, queries, form, preview, apply, log (MON-MD-1..22)

## Slice 6: Room accounts (needs KAM API phase 4)

- [ ] Client table, read-only protocol roles, remove (MON-RA-1..15); web users remain in `kammon.yml`

## Slice 7: Automations (needs KAM API phase 5; the script routes already exist)

- [x] List, editor, validate, save, enable, recover, delete, dry run (MON-AU-1..23, D29, D30). The dry run works against KAM's phase-5 route; until KAM ships it the button stays hidden behind "Needs a newer KAM." (MON-AU-20)

## Slice 8: Adverts

- [x] KAM advert inventory (`GET /v1/adverts`, API-AD-1..5) in the KAM repository, contract first
- [x] Reader, store snapshot (bounded, own freshness) and 60 s poll (MON-AD-2, 3)
- [x] Inventory table, metric strip and honest labels (MON-AD-4, 5, 10)
- [x] Map, advertised positions and OpenStreetMap links (MON-AD-6..8); hop-band placement of unlocated nodes removed (D48); marker clustering with badge zoom and spiderfy (MON-AD-17, D49); cluster z-order and smooth spiderfy (D52); base marker, inspector cards and layer chips (D53)
- [x] Flat operator-console restyle of the map: flat surfaces, no glow/shadow/blur, neutral base anchor, table-matched dot colours (D55, MON-AD-6/16/19/20)
- [x] Unpositioned-nodes panel (MON-AD-7) and localStorage viewport persistence (D56, MON-AD-15)
- [x] Copyable descriptive plan and viewer access (MON-AD-9, 11)
- [x] OpenFreeMap Positron basemap: replace the generated SVG with MapLibre GL JS + OpenFreeMap Positron tiles, attribution and an honest offline note; drop the vendored city tiers (D57, MON-AD-6/8)

## Slice 9: Audit trail

- [ ] Append-only `internal/audit` writer, `kammon.audit.jsonl` (default) and the bounded in-memory tail (MON-AT-8..13, 20)
- [ ] Record every state-changing admin action (web users and scripts today; modem and room clients when their slices land) with secrets and source kept out (MON-AT-1..7, 19)
- [ ] Record sign-ins and sign-outs too — password success and failure, guest, and logout (MON-AT-21..25)
- [ ] Admin-only `/audit` page and sidebar entry; read-only, newest first, with honest labels (MON-AT-14..18)

## Slice 4b: Manuals

Removed (D23, 2026-10-18): the manuals, their public `/manuals/` route and the sidebar Help section left the interface; the app sidebar carries only its navigation list (one plain list, D26), and the pinned copies of KAM's manuals went with them.

## Open points

- Replace the hand-written phase-2 text fixtures with recorded KAM responses when available.
- [x] Adapt the scaffold DTOs, fake KAM and fixtures to the clarified contract — event `next_after`, list envelopes, no pinned-room-role field. The reader, fake KAM and fixtures run against the pinned contract; the drift check is clean (2026-11-08).
- Decide whether demo mode ships in the release binary.
- [x] Web-user account management is separate from room accounts: see [PLAN-WebAccounts.md](PLAN-WebAccounts.md).
