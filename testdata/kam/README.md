# Contract fixtures

Each file is the contract in machine form. `internal/kam` decodes them with unknown fields
rejected, so a new field in the contract fails the tests until the types know it.

| File | Source |
|---|---|
| `status.json` | Recorded from the live KAM monitor API (2026-10-12). Regenerate when the contract lands committed in the KAM repository. |
| `events.json` | Recorded from the live KAM monitor API (first four events of the real ring). |
| `events-traits.json` | Hand-written for the fields the live ring had not produced yet: a `duplicate` RX, a TX with `ok`, and a `room` event. Replace with a real capture when the node produces them. |
| `channels.json`, `rooms.json`, `room_clients.json` | Hand-written from the contract: KAM phase 2 has not shipped these endpoints (404 today). |

Record a fixture:

```sh
curl -s http://127.0.0.1:8766/v1/status -o testdata/kam/status.json
curl -s 'http://127.0.0.1:8766/v1/events?after=0&limit=4' -o testdata/kam/events.json
```
