# Glossary

Use these words in code, tests, docs and screens.

| Term | Meaning |
|---|---|
| **KAM** | The Go service that connects a MeshCore KISS radio to SQLite and Starlark. The open system of record. |
| **KAMmon** | This project: the web monitor and login layer in front of KAM. |
| **Monitor API** | KAM's local REST API for KAMmon (`127.0.0.1:8766`). Contract: [kam-api/FeatureMonitorApi.md](kam-api/FeatureMonitorApi.md). |
| **Script API** | KAM's existing Local Management API for Starlark scripts (`127.0.0.1:8765`). |
| **Event** | One record in KAM's event ring: a packet heard (`rx`), a packet sent (`tx`), room activity (`room`), a script run (`script`) or a modem event (`modem`). Never carries payload text. |
| **Event ring** | KAM's fixed-size, in-memory list of recent events. |
| **Epoch** | A random id KAM creates at start. A new epoch means KAM restarted. |
| **Cursor** | The number of the last event KAMmon has seen (`after`). Kept by KAMmon only. |
| **Reset** | A reply flag meaning the cursor was too old or the epoch changed: earlier data may have a gap. |
| **Store** | KAMmon's in-memory history. Pages read the store, never KAM. |
| **Poller** | The one goroutine that reads from KAM and writes the store. |
| **Bucket** | One minute of events, summed per packet class. |
| **Packet class** | Group text, adverts, room traffic, other. |
| **Airtime** | How long a packet occupies the radio channel. In events it is an estimate. |
| **Observed packet airtime** | Estimated airtime of everything KAM heard or sent. Not total channel occupancy. |
| **Repeat** | A packet KAM already saw (`duplicate`). It still used airtime. |
| **Channel** | A shared-key group conversation such as `#public`. Not a room. |
| **Room** | A message board hosted by KAM with logins and history. |
| **Remembered client** | A client KAM has a room session for. Not an online user. |
| **Last heard** | The last valid request KAM received from a client. |
| **Stale Node** | A remembered client not heard for over 24 hours. Its session has not expired. |
| **Unknown** | No last-heard time is recorded. Not the same as Stale Node. |
| **Room protocol role** | Existing password-granted radio role in KAM. Shown read-only; independent of KAMmon web-user permissions. The pinned-role proposal is withdrawn. |
| **Echo** | The modem returns the values the host stored. It does not prove the radio chip accepted them. |
| **Viewer** | A logged-in user who can only look. |
| **Admin** | A user with full control, effective only on a request from the same computer. |
| **Allow list** | `viewer_text_channels`: channels whose message text viewers may read. |
