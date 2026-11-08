# Server statistics

The machine that runs KAM, in the sidebar under the node block, for the administrator (MON-SV-1). The only datasource is the opt-in `system` section of `GET /v1/status?system=true`, which the poller already fetches; the block never asks KAM anything itself and has nothing to show until KAM has answered.

## 1. What the block shows

| Line | Meaning |
|---|---|
| **Server** | The heading, to tell it apart from the node (KAM) above it. |
| Memory | `used_percent` and `available_bytes` — the RAM ready for new work, not a usage claim. |
| CPU | The core count and the 1/5/15-minute load averages, rounded for display to what `top` shows. No usage percentage: load is portable and answers "is the box overloaded" without sampling. |
| Disk | `used_percent` and `available_bytes` of the one filesystem the database lives on — the only disk KAM can actually exhaust. |
| Database | The database file's size, plus the WAL file's, when there is one. |

Honesty rules from the contract: sizes are bytes and are shown in the usual binary units; a section the OS cannot measure (memory, disk, CPU loads on some systems) is left out entirely, never shown as a zero. `collected_at` in the snapshot says how fresh a reading is; KAM refreshes it at most once per `monitor.system_interval_sec` (default 5 s) and collects nothing while nobody asks.

## 2. Requirements

| ID | Requirement | Level | From |
|---|---|---|---|
| MON-SV-1 | The sidebar shows the server block below the node block for admins only; a viewer is sent neither the block nor its numbers, on a page or in a live refresh. | MUST | ServerStats |
| MON-SV-2 | The block shows the status snapshot's `system` section — memory, CPU load, disk and database sizes — with honest wording, leaving unmeasured sections out. | MUST | ServerStats |
| MON-SV-3 | The block reads only the store's status snapshot: the poller asks with `?system=true`, the block adds no request of its own, and it shows nothing while KAM has not answered; a KAM without the section says "Needs a newer KAM." | MUST | ServerStats |

## 3. Access

The visibility rule is `view.CanSeeServerStats` (Access.md §3): administrators only. The live refresh of the overview, traffic and automations pages rides the block along out of band for an admin, the way the node block already travels; for a viewer it is left out entirely, so it never crosses the wire.
