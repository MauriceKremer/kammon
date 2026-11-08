# Server statistics

KAM's own operating numbers, in the sidebar under the node block, for the administrator (MON-SV-1). The only datasource is the status snapshot the poller already fetches from `GET /v1/status` (contract §7.1); the block never asks KAM anything itself and has nothing to show until KAM has answered.

## 1. What the block shows

| Line | Meaning |
|---|---|
| **Server** | The heading, to tell it apart from the node (KAM) above it. |
| TX | `tx.sent` on air and `tx.queued` waiting now. |
| Pipeline | `tx.rejected`, `tx.evicted`, `tx.deferred`: what the send gate refused, dropped for a full queue, or held back. |
| Gate | `tx.gate_timeouts`, plus the packets KAM's retention rings dropped (`persistence.adverts.dropped` and `persistence.group_texts.dropped` summed). |
| Event ring | How many events KAM's ring holds (`last_seq` − `first_seq` + 1) of `capacity`. |

The counters mean what KAM says they mean: they are KAM's running totals, counted on KAM's side, and they may reset when KAM restarts — the node block's epoch mark says when that happened.

## 2. Requirements

| ID | Requirement | Level | From |
|---|---|---|---|
| MON-SV-1 | The sidebar shows the server block below the node block for admins only; a viewer is sent neither the block nor its numbers, on a page or in a live refresh. | MUST | ServerStats |
| MON-SV-2 | The block shows KAM's own operating counters from the status snapshot: the TX pipeline, the gate, the persistence ring drops and the event ring, with honest wording. | MUST | ServerStats |
| MON-SV-3 | The block reads only the store's status snapshot: it adds no request to KAM and shows nothing while KAM has not answered yet. | MUST | ServerStats |

## 3. Access

The visibility rule is `view.CanSeeServerStats` (Access.md §3): administrators only. The live refresh of the overview, traffic and automations pages rides the block along out of band for an admin, the way the node block already travels; for a viewer it is left out entirely, so it never crosses the wire.
