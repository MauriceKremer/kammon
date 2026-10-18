# Decisions

Recorded 2026-10-11 during planning. Reasons are in [DESIGN.md](DESIGN.md) §13 and [kam-api/FeatureMonitorApi.md](kam-api/FeatureMonitorApi.md) §4.

| ID | Decision | Outcome | Why |
|---|---|---|---|
| D1 | Live data from KAM | Cursor pull over REST, polled every 1-2 s | Plain REST, no streaming, easy recovery |
| D2 | After a KAMmon restart | Memory only; backfill from KAM's ring; history start is labelled | No database or files to maintain |
| D3 | Admin versus web viewer | One listener, roles by user; admin power needs a request from the same computer | One port, no way for a remote visitor to reach admin routes |
| D4 | KAM endpoints | A separate monitoring listener (8766); scripts stay on 8765 | The existing script API stays untouched |
| D5 | Modem changes | Apply, then save to `kam.yml` | One source of truth; the reset probe reuses saved values |
| D6 | Room clients | List and remove; protocol role is read-only | No new radio-side permission model |
| D7 | What viewers see | Per-channel allow list for text | Public channels may be shared, private ones stay private |
| D8 | History | 8192-event ring in KAM; 24 h chart grows with KAMmon uptime | Small memory, no second aggregation path in KAM |
| D9 | UI technology | Go templates and htmx; charts are server-drawn SVG | One binary, no build step |
| D10 | Script tools | Dry run plus in-memory run information | Makes "build and test a script" real |
| D11 | Web roles versus room roles | Preserve existing room behavior; withdraw pinned roles | KAMmon web permissions are independent of MeshCore room authentication |
| D12 | KAM monitoring listener default | On by default, loopback only | Easiest setup; `monitor.enabled: false` removes it |
| D13 | List responses | Named envelopes and `[]` for empty lists | Explicit without generic wrapping machinery |
| D14 | Event pagination | Add `next_after`; keep `last_seq` as the ring head | Prevent clients skipping pages |
| D15 | Failed modem changes | Restore on any apply/save failure; pause TX if recovery fails | Never hide a runtime/configuration disagreement |
| D16 | Signal reporting | Always enabled | Preserve useful measurements without another setting |
| D17 | Recording | Short lock, bounded copy | No separate queue or worker; no claim of zero waiting |
| D18 | Dry runs | 500 ms default, 3000 ms maximum, one at a time, 16 replies / 4096 bytes | Bounded tests without asynchronous jobs |
| D19 | Per-packet SNR and RSSI | Measured and stored with every event and stored message, never shown on packet pages, channel message lists or message footers | KAM listens through a local repeater: signal strength describes the hop to KAM, not the sender, so the numbers carry no message |
| D21 | Room posts and client labels | Admin only; viewers see room summaries only | Client keys and room posts are private monitor data |
| D22 | Sidebar Help and manuals | Two collapsible sections, Workspace and Help; opening one collapses the other; Help lists every manual; a manual is read in the main screen, same tab; `/manuals/` stays public and cacheable. Removed 2026-10-18, see D23 | One shell, no new tab; native `<details name>`, no framework; beginners without an account still read at `/manuals/` (docs/requirements/Help.md) |
| A2 | Events | Metadata only; message text is fetched on demand | Keeps text out of the ring |
| D20 | Manuals | Markdown in `web/manuals/`, rendered once at startup, served public at `/manuals/` with `Cache-Control: public`; pages share the app shell, sidebar, pager and theme cookie. Removed 2026-10-18, see D23 | No build step or dependency; beginners can read without logging in; shared caches (Cloudflare) hold the static bytes, one theme across app and manuals |
| D23 | Manuals removed | The manuals, their public `/manuals/` route and the sidebar Help section are gone from the interface: the sidebar keeps Workspace only, `web/manuals/`, `internal/manuals` and the theme initializer for cached manual pages are deleted, and the static cache-buster lives in `internal/web` | The manual display cluttered the app; the simplest thing is a screens-only monitor, and the plain-words explanation stays in README.md and docs/ (2026-10-18) |
| D24 | Poll and age knobs in kammon.yml | `poll:` sets how often the poller reads KAM (`events`, `status`, `rooms`, `channels`), each read's `timeout`, and the pause after a 404 (`not_served`); `freshness:` sets `stale_after` and `down_after` (MON-DS-13); missing lines keep the defaults: the §5 intervals, 3 s, 10 minutes, 10 s, 2 min | Some nodes want a slower poll, and the old-data labels should match a poll that is slower or faster than the default bands (2026-10-18) |
| A3 | Repositories | KAM and KAMmon are separate repositories | KAM must work without KAMmon; KAM's packages are `internal/` |
