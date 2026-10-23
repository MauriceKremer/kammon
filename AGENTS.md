# AGENTS.md

KAMmon is a Go web monitor for one [KAM](../kam) node. KAM is the open system of record and works without KAMmon. KAMmon is a client of KAM's local REST API: it supplies the screens, the login and an in-memory history. [README.md](README.md) covers build and run.

## Philosophy

KAMmon is for enthusiasts, like KAM: pragmatism and easy setup come before strict design and hardening.

- Choose the simplest thing that works. One `kammon.yml`, one binary, no database, no front-end build step, no new dependency unless it clearly pays off.
- Passwords you write by hand are plain text in `kammon.yml`, like channel keys in KAM. Do not add secrets files, environment-variable plumbing or file-permission checks unless asked.
- Keep cheap correctness safeguards: input validation, bounded memory, escaping, not logging secrets.
- Write docs for beginners, in plain words.

## Rules

1. **KAM is open.** KAMmon never needs KAM to remember anything for it, never asks KAM to know about KAMmon, and keeps working (marked out of date) when KAM is slow or gone.
2. **Never import KAM code.** KAM's packages are `internal/`. The contract is [docs/kam-api/FeatureKamApi.md](docs/kam-api/FeatureKamApi.md); KAMmon defines its own types in `internal/kam`.
3. **Pages read the store, never KAM.** One poller writes `internal/store`; handlers only read it. Admin actions are the only calls from a request to KAM.
4. **The server decides access.** Every route is registered with a required role. Visibility rules live in `internal/view` only; templates never decide who sees what.
5. **Radio text is untrusted.** Names, senders, message text and labels are escaped by `html/template`. Never use `template.HTML` except for numbers-only SVG in `internal/web/chart.go` (a test enforces this).
6. **Honest labels.** Say what a number means: "remembered", not "online"; "echo", not "verified"; "stale", not "expired" ([docs/DESIGN.md](docs/DESIGN.md) §8).
7. **Memory is bounded.** Every buffer has a hard limit ([docs/requirements/DataStore.md](docs/requirements/DataStore.md) §3).

## The KAM repository

KAM sits next to KAMmon at `../kam`. The knowledge is one-way: KAM's code, docs and AGENTS.md never mention KAMmon. Everything about the link lives on this side, in [docs/kam-api/](docs/kam-api/README.md) and this section.

| Path | What |
|---|---|
| `../kam/docs/FeatureKamApi.md` | Source of truth for the contract; its §10 phases say what is spec'd when. Check its Status header — it may lag KAM's code. |
| `../kam/internal/api/` | KAM's implementation of the API; check `../kam/main.go` for whether it is wired in. |
| `../kam/AGENTS.md` | KAM's method for MeshCore research and feature docs; useful when reading KAM, silent about KAMmon. |
| `docs/kam-api/` | KAMmon's pinned copy, `KAM_COMMIT` and the drift check; see its README. |

When KAM lands a committed change to the contract, refresh `docs/kam-api/` and `testdata/kam/` in the same slice that uses it.

## Commands

```sh
go run ./cmd/kammon -demo          # fake KAM with generated data; login admin/admin or viewer/viewer
go run ./cmd/kammon -config kammon.yml
go test -race ./...
go vet ./... && gofmt -l .
diff ../kam/docs/FeatureKamApi.md docs/kam-api/FeatureKamApi.md   # contract drift check
```

## Layout

| Path | Purpose |
|---|---|
| `cmd/kammon` | Flags, wiring, signals |
| `internal/config` | Load and validate `kammon.yml` |
| `internal/kam` | KAM API types and the HTTP reader |
| `internal/kamfake` | Fake KAM and the demo data generator (also used in tests) |
| `internal/store` | In-memory history: event ring, minute buckets, snapshots, marks |
| `internal/poll` | The poller: the only reader of KAM |
| `internal/auth` | Users, sessions, local-request check |
| `internal/view` | Visibility rules (what a role may see) |
| `internal/web` | Routes, handlers, templates, chart |
| `web/` | Embedded templates and static files (htmx is vendored, no CDN) |
| `testdata/kam` | Example KAM responses; the contract in machine form |
| `prototype/` | The clickable HTML prototype, kept as visual reference |
| `docs/` | Design, requirements, plan, glossary, decisions, pinned KAM contract |

## Working method

- Requirements have stable ids (`MON-OV-3`, `API-EV-2`). Cite them in tests and commits.
- Test first for logic (the store cursor, visibility, auth). Check screens in a browser in demo mode.
- Work in vertical slices in the order of [docs/PLAN.md](docs/PLAN.md); tick the plan when a slice is done.
- When a requirement is finished, commit and push it, citing its ids (`MON-...`, `D...`) in the message; do not leave finished work uncommitted.
- Change the contract in the KAM repository first, then refresh `docs/kam-api/` and `testdata/kam/`.
- UI work follows the [kammon-ui-style skill](.github/skills/kammon-ui-style/SKILL.md).
- Use the terms in [docs/CONTEXT.md](docs/CONTEXT.md). Record decisions in [docs/DECISIONS.md](docs/DECISIONS.md).
