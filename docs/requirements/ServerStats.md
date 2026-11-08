# Server statistics

KAMmon's own operating numbers, in the sidebar under the node block, for the administrator (MON-SV-1). The numbers say what they are: total counters are **since KAMmon started**, and "reads" are the poller's HTTP reads of the KAM API, which is what a page never is.

## 1. What the block shows

| Line | Meaning |
|---|---|
| **Server** | The heading, to tell it apart from the node (KAM) above it. |
| Up | How long this KAMmon process has been running, from the moment `web.New` began. |
| Memory | The Go heap in use, plus the number of goroutines, read at render time. |
| KAM reads | Reads the poller made of KAM since start: `1,284` answered, and `· 3 failed` when some did not. A 404 ("needs a newer KAM") counts as failed too; a read KAMmon cancelled itself (shutdown) counts neither way. |
| Last error | The newest failed read: its age and the client's error text, clipped to 200 bytes (D33). Empty after a clean start. |

## 2. Requirements

| ID | Requirement | Level | From |
|---|---|---|---|
| MON-SV-1 | The sidebar shows the server block below the node block for admins only; a viewer is sent neither the block nor its numbers, on a page or in a live refresh. | MUST | ServerStats |
| MON-SV-2 | The block shows KAMmon's own uptime, memory in use and goroutines, with honest wording that distinguishes it from the KAM node block. | MUST | ServerStats |
| MON-SV-3 | The poller counts every read in the store (written by the poller only, read by the pages); counters never grow memory: the last error text is clipped. | MUST | ServerStats |

## 3. Access

The visibility rule is `view.CanSeeServerStats` (Access.md §3): administrators only. The live refresh of the overview, traffic and automations pages rides the block along out of band for an admin, the way the node block already travels; for a viewer it is left out entirely, so it never crosses the wire.
