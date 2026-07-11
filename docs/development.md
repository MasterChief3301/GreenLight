# Development

[← Docs index](README.md)

## Prerequisites

- **Go 1.22+**
- A **C toolchain** (gcc/clang) — `mattn/go-sqlite3` uses cgo, so builds need
  `CGO_ENABLED=1` (the default when a compiler is present).

## Build, test, run

```bash
go build ./...        # compile everything
go test ./...         # unit tests
go vet ./...          # static checks

# run locally
export GREENLIGHT_ADMIN_PASSWORD='dev'
export GREENLIGHT_SESSION_SECRET="$(openssl rand -hex 32)"
go run ./cmd/greenlight
```

## What the tests cover

The suite focuses on the logic that would be painful to get wrong:

| Area | Test | What it proves |
|---|---|---|
| Exactly-once resolution | `internal/store` `TestResolveConcurrent` | 20 concurrent resolvers → exactly one succeeds. |
| Restart-safe expiry | `TestListOverdue` | Overdue requests are found by SQL, not timers. |
| Rule precedence | `TestRulePrecedence` | exact → source → category → global ordering. |
| Request validation | `internal/app` `TestCreateRequestValidation` | Bad input is rejected before storage. |
| Defaults resolution | `TestCreateRequestDefaults*` | Rule vs. config vs. explicit precedence. |
| Auth primitives | `internal/server` `security_test.go` | Session signing, expiry, CSRF, login lockout. |

## Project layout

```
cmd/greenlight      main, wiring, graceful shutdown
internal/config     env-var configuration
internal/models     domain types + status/action transitions
internal/store      SQLite persistence (requests, rules, api keys)
internal/app        business logic (create/decide/expire, ntfy, callbacks)
internal/ntfy       ntfy publish client
internal/resume     resume-URL callback delivery with retries
internal/scheduler  background timeout + reminder engine
internal/server     HTTP API + web UI (auth, CSRF, templates)
web/                embedded templates + static assets (HTMX, CSS, icon)
```

See [Architecture](architecture.md) for how these fit together.

## Frontend

The UI is server-rendered Go `html/template` + [HTMX](https://htmx.org) (vendored
at `web/static/htmx.min.js`) — **no build step, no npm**. Templates and static
assets are embedded into the binary with `go:embed`, so the compiled binary is
fully self-contained.

To change styles, edit `web/static/app.css`; to change markup, edit the templates
in `web/templates/`. Rebuild the binary to pick up embedded-asset changes.

## Conventions

- Keep business logic in `internal/app`; handlers stay thin.
- All request state transitions go through `store.Resolve` so the exactly-once
  guarantee holds.
- Match the surrounding code's style; run `gofmt` / `go vet` before committing.
