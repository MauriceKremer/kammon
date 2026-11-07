# KAMmon Requirements: Automations (Starlark Workspace)

**Status:** Built (2026-10-23): the list, the editor, validate, save, enable, disable, recover and delete (MON-AU-1..14, 21..23). The dry run and the run object wait for KAM API phase 5; a KAM without the dry-run route leaves the button out and says "Needs a newer KAM." (MON-AU-20, D30). KAM ships periodic scripts, `rest_get` and `room_post` since 2026-11-06 (§10); KAMmon's reference panel describes them, the screens themselves are unchanged.
**Scope:** Creating, testing, saving and managing the Starlark scripts that KAM runs when it hears a channel message or on a timer. Design: [DESIGN.md](../DESIGN.md) §4.2 (master-detail). KAM side: [docs/kam-api/FeatureKamApi.md](../kam-api/FeatureKamApi.md) §7.6. Language and runtime: [docs/FeatureStarlark.md](../../../kam/docs/FeatureStarlark.md); periodic posts: [docs/FeatureStarlarkPeriodicPosts.md](../../../kam/docs/FeatureStarlarkPeriodicPosts.md).

## 1. Summary

**Starlark** is a small scripting language that looks like Python. A script reacts to a channel message and may send a reply; it may also run on a timer and fetch a web page or post into a room (§10). KAM's script routes list, create, validate, save, enable, disable, recover and delete scripts, with revision checks; they are served by the same listener as the KAM API on `127.0.0.1:8766` since KAM merged its listeners (2026-10-23). KAMmon becomes the screen for it.

KAMmon adds two things the API lacked: a **dry run**, which runs a script against a made-up message and shows what it would have said without sending anything, and **run information** (how often a script ran, how long the last run took, how many replies it queued).

A script can only do what KAM's `reply(channel, text)` allows. It cannot reach the radio, the files or the network. KAMmon must not suggest otherwise.

## 2. Goals and non-goals

**Goals**

- GL-1. See every script with its state and last run.
- GL-2. Write and edit a script in the browser, validate it, test it with a dry run, and save it.
- GL-3. Enable, disable, recover (after a timeout) and delete scripts safely.
- GL-4. Teach a beginner what a script receives and what it may do.

**Non-goals**

- Syntax highlighting, completion or a debugger in version 1.
- Running a script against real traffic from KAMmon.
- Version history beyond KAM's current revision number.
- Importing or exporting a script file (copy and paste is enough).

## 3. What the page shows

| Area | Content |
|---|---|
| **Script list** (left) | Name, state (READY, disabled, FROZEN, BROKEN), revision, a state dot, and a new-script button (admin). |
| **Editor** | Title with revision; a monospace text area with Validate under it. |
| **Settings row** | Script name, enabled checkbox, timeout in milliseconds. |
| **Console** | Result of the last Validate or Save, as plain text; save tells the new revision ("Saved revision 4"). |
| **Last run** | "Last run 14:59:48 · 2 ms · 1 reply" from the `run` object. |
| **Fixture form** | A small form for the dry run: channel, sender, text, hops, SNR, RSSI, clock drift, test timeout; the Dry run button at its bottom, the test's replies below the button. Sensible defaults. |
| **Reference** | A collapsed panel: the `on_group_text(message)` signature, the fields of `message`, `reply(channel, text)`, `room_post(room_id, text)`, `rest_get(url)`, and how a timer script declares itself (§10). |
| **Actions row** | At the bottom of the page, after the test message and the reference: Save, with the "Unsaved changes" note, and Delete. |
| **Recover** | For a FROZEN script: the last error and a Recover button. |

### 3.1 What a script receives

`message` is a read-only dictionary:

| Key | Meaning |
|---|---|
| `channel` | Normalised name such as `#emmen` |
| `sender` | Sender name |
| `body` | Message text |
| `timestamp_unix` | Sender's timestamp |
| `hop_count`, `hop_stations` | How far it travelled and the one-byte path hashes |
| `has_rx_meta`, `snr_quarter_db`, `rssi_dbm` | Signal data; SNR is in quarter dB (divide by 4) |
| `time_drift_seconds` | Difference between sender and KAM clocks |

A timer script's `on_timer(message)` gets one field, `timestamp_unix` — the KAM time of the fire.

## 3.2 What a script can do (KAM, 2026-11-06)

Besides `reply(channel, text)`, a script can:

| Reach | Call | Notes |
|---|---|---|
| A web page | `rest_get(url)` | Returns the body and the HTTP status. Shares the script's timeout, caps the body at 1 MiB, allows only `http`/`https`. |
| A KAM room | `room_post(room_id, text)` | Posts as the room's own identity into one of KAM's hosted rooms; clients get it through the normal backlog and push. |
| A timer | `timer_interval_minutes = N` plus `def on_timer(message)` | `N` a whole number of minutes ≥ 1, a plain assignment at the top of the source. The script object's `timer_interval_minutes` (read-only, from the source) shows it; a timer fire reuses the script's timeout and can freeze on timeout. |

The standard Starlark `json` and `time` modules are predeclared in every script.

Starter template offered for a new script:

```python
def on_group_text(message):
    if message["body"] == "!ping":
        reply(message["channel"], "pong")
```

## 4. Requirements

### 4.1 Reading

| ID | Requirement | Level |
|---|---|---|
| MON-AU-1 | The script list is read from `GET /scripts` (the KAM API listener, 8766) every 10 s and when the page opens. | MUST |
| MON-AU-2 | The list shows `runtime_status` in words. A FROZEN script shows the last error and a Recover button; a BROKEN script shows the last error. | MUST |
| MON-AU-3 | When `run` is present the page shows last run time, duration and reply count; otherwise it shows nothing in its place. | MUST |
| MON-AU-4 | Viewers see names, state, revision and run information. They never see source, and no source is fetched for them. | MUST |
| MON-AU-5 | When KAM's script API is down, the list shows the last known state with the stale-data bar. | MUST |

### 4.2 Editing (admin)

| ID | Requirement | Level |
|---|---|---|
| MON-AU-6 | Opening a script loads it with `GET /scripts/{id}` and remembers its `revision`. | MUST |
| MON-AU-7 | Validate sends `POST /scripts/validate` with the current text and timeout and shows KAM's message verbatim (it contains line and column). | MUST |
| MON-AU-8 | Save sends the name, text, timeout, enabled state and `expected_revision`; a create also carries the id, the name as typed (D32). Creating sends `expected_revision` 0 or none. | MUST |
| MON-AU-9 | A `409` shows "Changed elsewhere. Reload to see the latest version." and keeps the user's text in the editor so nothing is lost. A create has no stale revision: its `409` (the id or the name is already in use) shows KAM's own words. | MUST |
| MON-AU-10 | Save does not enable a script by itself. Enabled is an explicit checkbox and uses the enable and disable endpoints with the current revision. | MUST |
| MON-AU-11 | Recover sends `POST /scripts/{id}/recover` with the revision and optional new timeout. | MUST |
| MON-AU-12 | Delete asks for confirmation and sends `DELETE /scripts/{id}?revision=N`. | MUST |
| MON-AU-13 | The timeout field takes a positive whole number of milliseconds. KAM sets no upper limit; a value above 5000 shows a note that a long script blocks other scripts, because scripts run one at a time. | SHOULD |
| MON-AU-14 | Leaving the page with unsaved changes asks for confirmation. | SHOULD |

### 4.3 Dry run

| ID | Requirement | Level |
|---|---|---|
| MON-AU-15 | Dry run sends `POST /scripts/dry-run` with unsaved editor text and a fixture. Test timeout defaults to 500 ms, with a separate 1-3000 ms range; this does not change the live script's configured timeout. Explain 409 (another test running) and captured-reply limits (16 replies, 4096 UTF-8 bytes). | MUST |
| MON-AU-16 | The result lists each reply as "would send to #channel: text", the duration, or the error. | MUST |
| MON-AU-17 | The console says "Nothing was sent." on every dry run. | MUST |
| MON-AU-18 | The fixture form keeps its values while the page is open and has a reset button. | SHOULD |
| MON-AU-19 | The reference panel states that a dry run uses the same engine and the same timeout rules but cannot reproduce load. | SHOULD |
| MON-AU-20 | While KAM has no dry-run route (a `404`, or a `405` from an older KAM whose `/scripts/{id}` routes cover the path), the button is absent and a line says "Needs a newer KAM". | MUST |

### 4.4 Honesty

| ID | Requirement | Level |
|---|---|---|
| MON-AU-21 | The page never implies that a script can reach anything beyond what it says: `reply`, `room_post`, `rest_get` and its own timer (§3.2). Nothing else — no radio settings, no files, no network writes. | MUST |
| MON-AU-22 | A replying script uses airtime. The page shows a line: "Replies are real radio transmissions and count against the duty cycle." | MUST |
| MON-AU-23 | Source text is escaped when shown and is never run in the browser. | MUST |

## 5. What KAMmon needs from KAM

| Need | Endpoint | KAM requirement |
|---|---|---|
| List, get, create, update, delete | `GET/POST /scripts`, `GET/PUT/DELETE /scripts/{id}` | API-AU-1 (exists) |
| Validate | `POST /scripts/validate` | API-AU-1 (exists) |
| Enable, disable, recover | `POST /scripts/{id}/enable|disable|recover` | API-AU-1 (exists) |
| Run information | `run` object on each script | API-AU-2 |
| Dry run | `POST /scripts/dry-run` | API-AU-3, API-AU-4 |
| Queue depth and dropped events | `GET /v1/status` (`automation`) | API-AU-5 |
| Script run events | `script` events | API-EV-6 |
| Periodic scripts | `timer_interval_minutes` field, read-only, from the script's source | KAM's FeatureStarlarkPeriodicPosts (no new endpoint) |

KAM's periodic posts (2026-11-06) need no new route: the script list already carries the read-only `timer_interval_minutes` field, so the screens stay current with the list alone. What a page tells a user about them is §3.2 and the reference panel. If a later screen surfaces per-script timer intervals (for example in the list) it reads the same field.

KAM grew a second way to run a script. A line `timer_interval_minutes = N` in the source (a plain integer ≥ 1, top-level) makes KAM call `on_timer(message)` about every N minutes; `message` carries only `timestamp_unix`. A script may handle messages, timers or both, and timer scripts can send and fetch like any other. Timer fires are best-effort: one is dropped when the queue is full, and there is no catch-up on KAM restart.

KAMmon uses one base URL, `kam_monitor_url` (8766); the script routes share it (D28).

## 6. Visibility

| Element | Viewer | Admin |
|---|---|---|
| Script names, state, revision, run information | Yes | Yes |
| Source, dry run, save, enable, delete, recover | No | Yes |

## 7. Tests

- Fake KAM: validate ok and error, save ok and `409`, enable and disable with a stale revision, recover, delete, dry run ok and error and `404`.
- A `409` keeps the user's text.
- A viewer cannot fetch a script's source.
- The dry-run console always says nothing was sent.
- Starter template and fixture defaults match `message` keys in §3.1.

## 8. Corrections to the prototype

The prototype's sample scripts use `msg.text` and `reply(msg, ...)`. Real scripts use `on_group_text(message)`, `message["body"]` and `reply(channel, text)`. The prototype's Validate and Dry run only print simulated text.

## 9. Open points

- Whether KAMmon should show an event list of recent `script` events per script.
- Whether the editor needs a line-number gutter in version 1.
