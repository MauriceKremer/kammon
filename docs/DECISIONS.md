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
| A1 | Room posts and client labels | Admin only (the allow list covers channel text only) | Assumption; confirm before building the Rooms page |
| A2 | Events | Metadata only; message text is fetched on demand | Keeps text out of the ring |
| A3 | Repositories | KAM and KAMmon are separate repositories | KAM must work without KAMmon; KAM's packages are `internal/` |
