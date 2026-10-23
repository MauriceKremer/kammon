# KAMmon

KAMmon is a web monitor for a [KAM](../kam) node. It shows what the radio hears, how busy the channel is, what the channels and rooms are doing, the KISS modem, and the Starlark scripts, in a calm dark (or light) console.

KAM is the open system that does the work and runs fine without KAMmon. KAMmon only adds the screens and the login. It runs on the same computer as KAM and talks to it over KAM's local REST API.

**Status:** early scaffold. The demo mode, login and the Overview page work against a fake KAM. Everything else is planned; see [docs/PLAN.md](docs/PLAN.md).

## Try it

You need Go 1.25 or newer.

```sh
go run ./cmd/kammon -demo
```

Open <http://127.0.0.1:8080>. Sign in as `admin` / `admin` (local administrator) or `viewer` / `viewer` (view only). Demo mode starts a fake KAM with generated traffic, so no radio is needed.

## Run against a real KAM

1. Copy [kammon.yml](kammon.yml) to `kammon.local.yml` and change the passwords.
2. Start KAM. Its KAM API listens on loopback (port `8766` by default; see [docs/kam-api/FeatureKamApi.md](docs/kam-api/FeatureKamApi.md)).
3. Run `go run ./cmd/kammon -config kammon.local.yml`.

Passwords are plain text in the config file, like channel keys in KAM. An `admin` user has admin power only on a request from the same computer; from anywhere else the admin is a view-only user.

## Documents

| File | Content |
|---|---|
| [docs/DESIGN.md](docs/DESIGN.md) | How KAMmon looks, is built and behaves |
| [docs/requirements/](docs/requirements/) | One requirement file per feature |
| [docs/kam-api/](docs/kam-api/) | The KAM API contract this version is built against |
| [docs/PLAN.md](docs/PLAN.md) | Build order and progress |
| [docs/CONTEXT.md](docs/CONTEXT.md) | Glossary |
| [docs/DECISIONS.md](docs/DECISIONS.md) | Decisions |
| [prototype/](prototype/) | The clickable prototype (open the HTML file in a browser) |

Layout and working rules for contributors and AI assistants: [AGENTS.md](AGENTS.md).
