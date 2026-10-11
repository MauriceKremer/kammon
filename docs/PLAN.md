# Plan

Build in vertical slices. Each slice: test first for the logic, check the screen in demo mode, tick the box. Requirement ids are in [requirements/](requirements/); KAM ids (`API-*`) are in [kam-api/FeatureMonitorApi.md](kam-api/FeatureMonitorApi.md).

Status legend: `[x]` done in this scaffold, `[ ]` open.

## Slice 0: Scaffold

- [x] Repository, layout, `AGENTS.md`, CI, glossary, decisions
- [x] `config`: load and validate `kammon.yml`
- [x] `kam`: types and HTTP reader for status, events, channels, rooms, clients; fixtures decode strictly
- [x] `kamfake`: fake KAM with cursor, epoch and reset semantics, and a demo data generator
- [x] `store`: event ring, minute buckets, cursor logic, gap and restart marks, freshness (MON-DS-6..14, 16, 18)
- [x] `poll`: backfill and polling with backoff (MON-DS-1..5, 7)
- [x] `auth`: users, sessions, local-request check (MON-AX-1, 3..5, 8..11)
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

## Slice 3: Channels and Rooms (needs KAM API phase 2)

- [ ] Channels page (MON-CH-1..12)
- [ ] Rooms page with the activity indicator and legend (MON-RM-1..14)

## Slice 4: Access hardening

- [ ] Failed-login slowdown (MON-AX-6), Origin and htmx header checks on every change route (MON-AX-17, 18), optional TLS (MON-AX-21)
- [ ] Theme cookie read by the server (design §7.3)

## Slice 5: KISS modem (needs KAM API phase 3)

- [ ] Snapshot, queries, form, preview, apply, log (MON-MD-1..22)

## Slice 6: Room accounts (needs KAM API phase 4)

- [ ] Client table, read-only protocol roles, remove (MON-RA-1..15); web users remain in `kammon.yml`

## Slice 7: Automations (needs KAM API phase 5; the script routes already exist)

- [ ] List, editor, validate, save, enable, recover, delete, dry run (MON-AU-1..23)

## Open points

- Confirm assumption A1 in [DECISIONS.md](DECISIONS.md) before slice 3.
- Replace the hand-written `testdata/kam/*.json` with recorded KAM responses when KAM ships the monitor API.
- Before integration, adapt scaffold DTOs, fake KAM and fixtures to the clarified contract: event `next_after`, list envelopes and no pinned-room-role field. Passing scaffold tests do not yet prove compatibility with this revision.
- Decide whether demo mode ships in the release binary.
