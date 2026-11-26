# KAMmon Requirements: Audit Trail

**Status:** Built (2026-11-26): `internal/audit` (append-only JSON Lines with a bounded in-memory tail), the record of admin changes and sign-ins, and the admin-only read-only `/audit` page (MON-AT-1..25, D60..D62). Demo mode keeps the trail in memory.
**Scope:** A permanent, read-only record of every state-changing administrator action and of every sign-in and sign-out, written to disk, shown on an administrator-only page. It answers "who changed what, and when" and "who came and went". KAM data stays in KAM; this is KAMmon's own record of its own activity. Related rules: [Access.md](Access.md) (who may act and sign in), [WebAccounts.md](WebAccounts.md) and [Automations.md](Automations.md) (the actions themselves).

## 1. Summary

KAMmon's administrator can change things from the browser: add, edit and delete web users, and create, save, enable, disable, recover and delete scripts. Today those changes happen and are then gone from KAMmon's memory when it restarts, because the store is memory-only ([DataStore.md](DataStore.md) §2, D2).

An **audit trail** keeps a permanent record of those changes, and of every sign-in and sign-out — a password login that succeeds or fails, a guest login, and a logout. Each event appends one **audit record** to an append-only file on disk. An administrator can open an **Audit trail** page to read the records, newest first, but nothing on that page — and no route of the server — can change or remove a record. The trail is a log of what was done, not a control surface.

A change record stores the **date and time**, the **user** who acted, the **action**, the **target**, the **changed values** as before/after pairs, and the **outcome** (success or failure); a sign-in record stores the same minus the target and changed values. The trail never stores passwords, password hashes or script source.

Plain words: the trail is like a shop's till roll — every change and every sign-in is written down in order, anyone who may use the controls can read it, and no one can tear a line out from the web page.

## 2. Goals and non-goals

**Goals**

- GL-1. Every state-changing administrator action leaves one permanent record on disk.
- GL-2. An administrator can read the whole trail from a dedicated page; no other role can.
- GL-3. The trail can be read but never modified or removed from the web.
- GL-4. The record says exactly what changed, without re-leaking secrets.
- GL-5. A failed action is recorded too, marked as failed.
- GL-6. Every sign-in and sign-out — including failed password attempts and guest logins — leaves one record.

**Non-goals**

- Recording read-only operations (opening a script, a validate, a dry run, viewing a page). These change nothing.
- Recording the poller's reads of KAM. The poller never changes anything ([DataStore.md](DataStore.md) MON-DS-4).
- Recording KAM-side changes made by anything other than KAMmon's own admin actions.
- A second database or server. The trail is one append-only file.
- Tamper-proofing against someone who already controls the computer. KAMmon adds no OS-level file permissions (consistent with the project's philosophy); the guarantee is that no web route or handler can modify or remove a record.

## 3. What is recorded

### 3.1 The audit record

One record is one line of JSON (JSON Lines), appended in order. Fields:

| Field | Meaning |
|---|---|
| `seq` | A whole number that grows by one per record. It names the order and survives a restart. |
| `at` | Date and time the action was recorded, RFC 3339, from KAMmon's clock. |
| `actor` | The username that acted: the signed-in administrator for a change, the user who signed in or out, or the name typed at a failed sign-in (empty when none was typed). |
| `role` | The role of the acting session — `admin`, `viewer` or `guest`. Empty for a failed sign-in, where no session exists. |
| `action` | A stable verb string naming the action, from the tables in §3.3 and §3.4. |
| `target` | What was acted on: `{ "type", "id", "name" }`. `id` is present for scripts. Empty for sign-in and sign-out records. |
| `changes` | The fields that changed, each as `{ "from", "to" }`. `from` is absent for a new value, `to` is absent for a removed value. Secrets are never values here (§3.5). Empty for sign-in and sign-out records. |
| `outcome` | `"ok"` or `"failed"`. A failed action is still recorded. |
| `detail` | One plain sentence summing up the result, for example "Saved revision 5." or "Enabled. Revision 5 is current." |
| `remote` | The immediate connection's host that sent the request, when it is known. It is the peer the server saw, which may be a reverse proxy, not necessarily the administrator's own address. |

Example:

```json
{"seq":12,"at":"2026-11-26T14:03:05+01:00","actor":"admin","role":"admin","action":"script.disable","target":{"type":"script","id":"ping","name":"!ping"},"changes":{"enabled":{"from":true,"to":false}},"outcome":"ok","detail":"Disabled. Revision 5 is current.","remote":"192.168.1.20"}
```

### 3.2 What is recorded

The trail records two kinds of activity:

- **Administrator changes** — actions that change state (§3.3).
- **Sign-in activity** — sessions starting and ending (§3.4).

Read-only operations are not recorded:

| Recorded | Not recorded (changes nothing) |
|---|---|
| Create, update or delete a web user | Opening the web-users page |
| Create, update, enable, disable, recover or delete a script | Opening the editor, validate, dry run |
| Sign in (success, failed, guest) and sign out | Viewing a page |

A **validate** and a **dry run** run KAM code but change nothing on KAM or in KAMmon, so they leave no audit record (MON-AT-1). The dry run always says "Nothing was sent." ([Automations.md](Automations.md) MON-AU-17), and the audit trail follows the same honesty: nothing changed, nothing recorded.

### 3.3 The actions and their changed fields

| Action | Source (built today) | `target` | `changes` recorded |
|---|---|---|---|
| `user.create` | Web users, add | `{type:"user", name}` | `name` (to), `role` (to), `password` → `{"to":"<set>"}` |
| `user.update` | Web users, edit | `{type:"user", name}` (new name) | `name`, `role` and/or `password` → `{"to":"<changed>"}`, only the fields that changed |
| `user.delete` | Web users, delete | `{type:"user", name}` | none (the action says it) |
| `script.create` | Automations, Save on a new script | `{type:"script", id, name}` | `name` (to), `timeout` (to), `source` → `{"to":"<set>"}` |
| `script.update` | Automations, Save on an existing script | `{type:"script", id, name}` | `name`, `timeout` and/or `source`, only the fields that changed |
| `script.enable` | Automations, enable | `{type:"script", id, name}` | `enabled` → `{"from":false,"to":true}` |
| `script.disable` | Automations, disable | `{type:"script", id, name}` | `enabled` → `{"from":true,"to":false}` |
| `script.recover` | Automations, Recover | `{type:"script", id, name}` | `timeout` (from/to) when a new timeout was given |
| `script.delete` | Automations, Delete | `{type:"script", id, name}` | none (the action says it) |

Actions that are still on the plan reuse the same envelope when their slices land:

| Action | Slice | Notes |
|---|---|---|
| `modem.apply` / `modem.save` | 5 (KISS modem) | The changed radio values as before/after pairs. |
| `room.client.remove` | 6 (Room accounts) | `target` names the room and the removed client's public key. |

### 3.4 Sign-in activity

| Action | Source | `actor` | `outcome` | `detail` |
|---|---|---|---|---|
| `login.success` | `POST /login` succeeds | the user's name | `ok` | "Signed in." |
| `login.failed` | `POST /login` is refused | the name that was typed (empty when none) | `failed` | "Wrong name or password." |
| `login.guest` | `POST /login/guest` succeeds | `Guest` | `ok` | "Signed in as Guest." |
| `logout` | `POST /logout` | the session's user | `ok` | "Signed out." |

A sign-in record has no `target` and no `changes` (MON-AT-25). The attempted password is never recorded (MON-AT-5).

### 3.5 What is never stored

- **Passwords and password hashes.** A password set or change is recorded as `<set>` or `<changed>`, never the value; the password typed at a failed sign-in is never recorded at all (MON-AT-5, MON-AT-22, MON-AX-16).
- **Script source.** A source change is recorded as `<set>` or `changed` with a SHA-256 fingerprint of the source, so the trail ties the change to a specific source without storing the body (MON-AT-6). The editor's own text stays out of the record, exactly as the store keeps script source out of memory ([Automations.md](Automations.md) D29).
- **Radio text.** The trail stores only administrator-typed values; names and values that come from the radio never enter it, and nothing is ever written to the page with `template.HTML` (rule 5).

## 4. What the page shows

| Area | Content |
|---|---|
| **Notice** | One line above the table: "The audit trail is a permanent record. Nothing on this page can change or remove it." |
| **Table** | Newest first: time, user, action, target (for changes), the changed fields (from → to), and the outcome. A failed action or a failed sign-in is marked; a sign-in row shows its `detail` and no target or changes. |
| **Redacted values** | A password change reads `password: <changed>`; a source change reads `source: changed (fingerprint …)`. No secret text ever appears. |
| **Empty state** | "No activity recorded yet." when the trail is empty. |
| **Paging** | The newest records with a "Show more" control; the page says how many records it is showing and how many exist in total. |

The page is a real URL (`/audit`) that renders fully on the server, with a live partial (`/ui/audit`) for htmx, following the same master-detail pattern as the other pages ([DESIGN.md](../DESIGN.md) §4.2). Every value is escaped by `html/template`.

## 5. Requirements

### 5.1 Recording

| ID | Requirement | Level |
|---|---|---|
| MON-AT-1 | Every state-changing administrator action in §3.3 appends exactly one audit record. Validate and dry run change nothing and append none. | MUST |
| MON-AT-2 | A failed action is recorded too, with `outcome: "failed"` and the failure in `detail`. | MUST |
| MON-AT-3 | Each record carries the fields in §3.1: `seq`, `at`, `actor`, `role`, `action`, `target`, `changes`, `outcome`, `detail`, and `remote` when known. | MUST |
| MON-AT-4 | `changes` lists only the fields that changed, each as `from`/`to` scalar values; `from` is omitted for a new value and `to` for a removed one. | MUST |
| MON-AT-5 | Passwords and password hashes are never recorded. A password set or change is recorded as `<set>` or `<changed>`. | MUST |
| MON-AT-6 | Script source is never written into a record. A source change is recorded as `<set>` or `changed` with a SHA-256 fingerprint of the source. | MUST |
| MON-AT-7 | The `actor` is the signed-in administrator's username, and a record is written only after the route guard has accepted the administrator role ([Access.md](Access.md) MON-AX-11). | MUST |

### 5.2 Storage and persistence

| ID | Requirement | Level |
|---|---|---|
| MON-AT-8 | Records are appended to one file as JSON Lines, one record per line, opened append-only (`O_APPEND`). | MUST |
| MON-AT-9 | The trail has a single writer under its own lock; each record is one write followed by `fsync`, so a record is whole before the next action proceeds. | MUST |
| MON-AT-10 | `seq` is monotonic and continues across restarts: on start the log reads the last line's `seq` and carries on. | MUST |
| MON-AT-11 | On start the log loads a bounded tail into memory (the most recent 1000 records by default) and counts the total; a torn trailing line from an interrupted write is ignored and reported, not treated as an error. | MUST |
| MON-AT-12 | The default path is `kammon.audit.jsonl` next to `kammon.yml`; an `audit_file:` key in `kammon.yml` relocates it. In demo mode the trail stays in memory for the life of the process and writes no file. | MUST |
| MON-AT-13 | The trail exposes no way to modify or delete a record: there is no edit, delete or truncate method, and the only file handle is append-only. | MUST |

### 5.3 Viewing

| ID | Requirement | Level |
|---|---|---|
| MON-AT-14 | `/audit` and `/ui/audit` are administrator-only; any other role is answered `403` (D34). | MUST |
| MON-AT-15 | The page lists records newest first — time, actor, action, target, changes (from → to), outcome — and every value is escaped by `html/template`. | MUST |
| MON-AT-16 | The page shows the newest records with a bounded "Show more" control and states how many records are shown and how many exist in total. | MUST |
| MON-AT-17 | The page states plainly that the trail is a permanent record and cannot be changed or removed from this page. | MUST |
| MON-AT-18 | The sidebar shows an "Audit trail" entry with a lock icon, visible only to administrators (D34, MON-AX-12). | MUST |

### 5.4 Failure and boundedness

| ID | Requirement | Level |
|---|---|---|
| MON-AT-19 | If the action succeeds but writing the record fails (for example the disk is full), the action stays done, KAMmon logs the failure loudly, and the response shows a visible warning: the change happened, but the trail could not be written. | MUST |
| MON-AT-20 | The in-memory tail is bounded (MON-AT-11). The file itself grows with use; administrator actions and sign-ins are rare, so growth is small, and this is stated in the docs rather than hidden. | MUST |

### 5.5 Login activity

| ID | Requirement | Level |
|---|---|---|
| MON-AT-21 | A successful password sign-in appends one record — `action: login.success`, `actor` the user's name, `role` the user's role, `outcome: ok`. | MUST |
| MON-AT-22 | A failed sign-in appends one record — `action: login.failed`, `actor` the name that was typed (empty when none), `role` empty, `outcome: failed`, and `remote` when known. The attempted password is never recorded (MON-AT-5). | MUST |
| MON-AT-23 | A guest sign-in (while `guest_access` is on) appends one record — `action: login.guest`, `actor: Guest`, `role: viewer`, `outcome: ok`. | MUST |
| MON-AT-24 | A sign-out appends one record — `action: logout`, `actor` the session's user, `outcome: ok`. | MUST |
| MON-AT-25 | A sign-in or sign-out record has no `target` and no `changes`. | MUST |

A sign-in record is appended only after the check has decided the answer (accepted or refused), so the trail always tells the truth about the attempt. A session that ends by idle timeout is not recorded, because there is no request to hang a record on.

## 6. Storage and configuration

One data file, not part of `kammon.yml` (which stays configuration and is rewritten in place by the web-users feature). The trail is written beside it:

```yaml
# Where the audit trail is written. Append-only; default is kammon.audit.jsonl
# next to this file. The web page can only read it.
# audit_file: kammon.audit.jsonl
```

The layout gains one package:

| Path | Purpose |
|---|---|
| `internal/audit` | The audit trail: the record type, the append-only file writer, the bounded in-memory tail, and the reader the page uses. |

The page reads the trail's bounded in-memory tail, never the file directly, so a page view never opens or scans the file (it is already loaded on start). Rule 3 ("pages read the store, never KAM") is untouched: the audit trail is not KAM data, and it has no poller. The trail's writers are the administrator action handlers and the sign-in and sign-out handlers, each appending after its event has finished.

## 7. Visibility

| Element | Viewer | Admin |
|---|---|---|
| Audit trail page and records | No: the routes answer `403` (D34) | Yes |
| Modify or remove a record | No | No — nothing on the page or any route can |

An administrator can read the whole trail but cannot change or remove any part of it from the web. Removing or editing the file itself remains an operator-with-shell-access act, the same as editing `kammon.yml` by hand; KAMmon adds no file-permission checks unless asked (project rule).

## 8. Tests

- A table test that each state-changing action in §3.3 appends one record with the right `action`, `target`, `changes` and `outcome`, and that validate and dry run append none.
- A test that no record contains a password, a password hash or a script body, and that a source change carries only a fingerprint.
- A test that the trail type has no delete or truncate method and the page routes are `GET` only (compile-time and route-table checks).
- A test that a viewer gets `403` on `/audit` and `/ui/audit`, and that the sidebar entry is absent for a viewer.
- A test that a failed action is recorded with `outcome: "failed"`.
- A test that a write failure after a successful action yields the warning and a log line, without undoing the action.
- A test that restarting reloads the tail and continues the sequence number.
- A race test that concurrent administrator actions append distinct, ordered records.
- A test that a successful sign-in, a failed sign-in, a guest sign-in and a sign-out each append one record with the right `action`, `actor` and `outcome`.
- A test that a failed sign-in records the attempted name and never the attempted password.

## 9. Open points

- **Retention and rotation.** The file grows for the life of the node. Trimming or rotating the oldest records would remove them, which the operator asked never to happen from the web; because administrator actions are rare, the simplest version keeps appending. Rotation, if ever wanted, is an operator-side filesystem decision, not a web feature.
- **The remote address.** `remote` records the immediate peer, which may be a reverse proxy. Whether to trust and record `X-Forwarded-For` is left open (it is a transport concern, and KAMmon adds no transport rules of its own, D31).
- **A separate debug log** for read-only operations (validate, dry run) is out of scope for the audit trail; it would be a different, non-permanent log if it is ever wanted.
- **Idle session expiry.** A session dropped by the idle timeout has no request, so it is not recorded; only an explicit sign-out is.
- **The planned slices.** When the KISS modem (Slice 5) and room accounts (Slice 6) land, their admin actions append records through the same `internal/audit` writer, using the `modem.*` and `room.client.remove` actions in §3.3.
