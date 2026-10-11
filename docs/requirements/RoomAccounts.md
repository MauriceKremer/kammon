# KAMmon Requirements: Room Accounts

**Status:** Requirements only. Nothing is built.
**Scope:** Inspecting and removing remembered MeshCore room clients. Protocol roles are read-only. KAMmon web users and web permissions belong to [Access.md](Access.md), not this page. Design: [DESIGN.md](../DESIGN.md). KAM side: [docs/kam-api/FeatureMonitorApi.md](../kam-api/FeatureMonitorApi.md) §7.3 and §7.4.

## 1. Summary

In KAM today a client's role comes from the **password** it logs in with: the admin password gives *admin*, the guest password gives *read-write*, and a read-only fallback gives *guest*. There is no way to change one client without changing the password for everyone.

The earlier pinned-role proposal is withdrawn. This page shows existing radio-side roles and can **remove** a client, which forgets its session. It does not change passwords or login behavior. KAMmon's web administrator is allowed to call the full-access local REST API; that is unrelated to a radio's room role.

KAMmon's room roles are separate from KAMmon's own login accounts ([Access.md](Access.md)).

Plain words:

| In KAMmon | In KAM | Can post | Notes |
|---|---|---|---|
| Admin | `ADMIN` | Yes | Protected when the room is full |
| Contributor | `READ_WRITE` | Yes | |
| Viewer | `GUEST` | No | Receives posts |

## 2. Goals and non-goals

**Goals**

- GL-1. List the remembered clients of a room with role, last activity and label.
- GL-2. Clearly separate radio protocol roles from KAMmon web-user permissions; room roles cannot be edited here.
- GL-3. Remove a client.
- GL-4. Say clearly what removing a session does and does not do.

**Non-goals**

- Adding a client by public key before it ever logs in (decision D6).
- A block list. A removed client can log in again with a valid password.
- Changing room passwords or room settings. Those stay in `kam.yml`.
- Rooms' creation or deletion.

## 3. What the page shows

| Area | Content |
|---|---|
| **Room selector** | A select with the rooms. |
| **Notice** | "Radio roles follow existing room login rules. Removing a client forgets its session, not its access credentials. It may log in again. Web-user permissions are managed separately in KAMmon." |
| **Role legend** | The table above, in short. |
| **Client table** | Label or key, public key (short, copy control), read-only protocol role, last activity with indicator, remove button. |
| **Filter** | Search by label or key; sort by last activity. |
| **Capacity** | "9 of 20 remembered" (`max_clients`). |
| **Help** | Room passwords and radio admission rules live in `kam.yml`; KAMmon web users live in `kammon.yml`. Neither is edited by this client list. |

## 4. Requirements

### 4.1 Reading

| ID | Requirement | Level |
|---|---|---|
| MON-RA-1 | The page is for admins on a local request only. It is not in the navigation for viewers and its routes answer `403`. | MUST |
| MON-RA-2 | The client table reads the named `clients` envelope from `GET /v1/rooms/{id}/clients` when opened and every 10 seconds. Do not refresh while a removal dialog is open. | MUST |
| MON-RA-3 | A row shows the label when KAM sent one and the short key otherwise; the full key is always reachable. The label is marked "from advert" and is not treated as identity. | MUST |
| MON-RA-4 | Show the existing protocol role read-only, labelled separately from the signed-in web user's role. No role select or pin marker. | MUST |
| MON-RA-5 | The last-activity cell uses the activity indicator and legend from [Rooms.md](Rooms.md). | MUST |
| MON-RA-6 | "Remembered" capacity shows `clients_remembered` against `max_clients`. | SHOULD |

### 4.2 Changing

| ID | Requirement | Level |
|---|---|---|
| MON-RA-7 | Withdrawn: no role mutation or pinned-role control (D11). | WITHDRAWN |
| MON-RA-8 | A removal changes the row only after KAM confirms; on an error leave the client visible and show the message. | MUST |
| MON-RA-9 | Withdrawn: no demotion control (D11). | WITHDRAWN |
| MON-RA-10 | Remove opens a dialog: "Forget this client's session? It may log in again under the existing room rules. This does not block it." Confirming sends `DELETE`. | MUST |
| MON-RA-11 | A `404` after a change (the client was just evicted) reloads the table and says so. | MUST |
| MON-RA-12 | Each successful change adds a line to a small "Recent changes" list on the page (kept in KAMmon memory, lost at restart). | SHOULD |
| MON-RA-13 | The label field is display only. KAMmon has no list of names of its own. | MUST |

### 4.3 Honesty

| ID | Requirement | Level |
|---|---|---|
| MON-RA-14 | The page never says "ban", "block" or "kick". It says "remove". | MUST |
| MON-RA-15 | While KAM lacks client removal, do not offer removal. A `404` for a removed/evicted client refreshes the list; do not confuse an absent client with an unsupported feature. | MUST |

## 5. What KAMmon needs from KAM

| Need | Endpoint | KAM requirement |
|---|---|---|
| Clients with protocol role, last activity, label | `GET /v1/rooms/{id}/clients` | API-RM-2, API-RM-8, API-RM-9 |
| Remove a client | `DELETE /v1/rooms/{id}/clients/{key}` | API-RM-5 |
| Changes applied on the room worker and saved first | Behaviour | API-RM-7 |
| Change events | `room` events `client_removed` | API-EV-5 |

KAM keeps existing room login semantics and validates removals (`API-F-12`). KAMmon checks web access before calling it; KAM needs no web-user or token model.

## 6. Visibility

Admin only. A viewer gets none of this page.

## 7. Tests

- A fake KAM: delete succeeds, client was already evicted (`404`), and persistence failure leaves the session intact.
- The row updates after confirmation only.
- The removal dialog and read-only role display; no request to a role setter is made.
- A viewer request to every route of this page returns `403`.
- Existing room behavior is unchanged after integration, including read-only fallback and blank resumes.

## 8. Open points

- Whether the "Recent changes" list should be written to a log file, since KAMmon keeps nothing on disk.
- No pinned-role extension is planned. Web-user role configuration is part of Access, not room-client management.
