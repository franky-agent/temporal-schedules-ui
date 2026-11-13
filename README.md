# Temporal Schedules UI

A minimal web UI for [Temporal Schedules](https://docs.temporal.io/develop/go/workflows/schedules)
built with **Go 1.27** + **htmx**. No frontend build step, no JavaScript framework —
just server-rendered HTML swapped in place by htmx.

![schedules list](docs/screenshot.png)

## Features

- **List** all schedules (auto-refreshing table, polls every 10 s via htmx)
- **Create** a schedule from a simple form — cron expression *or* interval
- **Detail** view: action counts, upcoming runs, recent runs, policies
- **Pause / Unpause / Trigger / Delete** — each row is updated in place via htmx
- A demo `SendEmailWorkflow` + worker are included so triggered actions actually run

## Quick start

You need a Temporal server reachable on `localhost:7233` (the default dev server).

```bash
# 1. Start Temporal dev server (in another terminal)
temporal server start-dev

# 2. Run the UI + worker
go run ./cmd/server

# 3. Open the UI
open http://localhost:7099
```

### Configuration (env vars)

| Var | Default | Description |
|-----|---------|-------------|
| `TEMPORAL_HOST_PORT` | `localhost:7233` | Temporal server host:port |
| `SCHEDULES_UI_ADDR` | `:7099` | HTTP listen address |

## Project layout

```
cmd/server/main.go          # entrypoint: connect Temporal, start worker + HTTP server
internal/temporal/          # Temporal client + ScheduleService helpers + demo workflow
  client.go                 # Dial + health check
  schedules.go              # List/Describe/Create/Pause/Unpause/Trigger/Delete
  workflow.go               # SendEmailWorkflow + activity (demo action)
internal/web/               # HTTP layer
  server.go                 # routes, template parse, helpers
  handlers.go               # request handlers (htmx-aware)
  templates/*.html          # Go templates (base, index, row, rows, detail, create)
  static/style.css          # hand-written dark theme CSS
```

## How the htmx flow works

- `GET /` renders the full page with the schedule table.
- `GET /schedules` returns just the `<tbody>` — used for the 10 s auto-refresh and
  the manual "Refresh" button (`hx-get="/schedules" hx-target="#schedule-rows" hx-swap="outerHTML"`).
- Row actions (`pause`/`unpause`/`trigger`/`delete`) target `#row-<id>` and swap the
  single row out, so the whole table isn't re-rendered on every click.
- `GET /schedules/{id}` returns the detail panel inline (htmx) or a full page otherwise.
- `GET /schedules/new` lazily loads the create form into `#create-panel`.

## Why this design

- **Single binary, no node_modules.** Templates and CSS are `go:embed`-ded.
- **htmx over JSON API.** The server returns HTML fragments; no client-side
  state, no fetch/JSON parsing, no virtual DOM.
- **Go 1.27 `http.ServeMux` routing.** Uses the new `METHOD /path/{id}` patterns
  and `r.PathValue("id")` — no third-party router needed.
- **Thin wrapper over the SDK.** `ScheduleService` maps 1:1 onto
  `client.ScheduleClient` so the UI can't drift from the SDK semantics.
