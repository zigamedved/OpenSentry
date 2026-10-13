# Contributing

OpenSentry is a prototype. Small, tested pull requests are welcome.

## Local development

You need Go 1.22 or newer, Node.js 22, and PostgreSQL 15 or newer.

Create a database that matches the API defaults:

- user: `postgres`
- password: `postgres`
- database: `cronsentry` (historical database name; the product is OpenSentry)

```bash
cd cronsentry
go test ./...
go run ./cmd
```

Open the dashboard and create an account. Without a session, `/api/jobs` returns 401. Ping URLs stay public. Set `DEMO_SEED=true` to create `test@example.com` / `opensentry-demo` on startup.

In another shell:

```bash
cd cronsentry/web
npm ci
npm run dev
```

- API: http://localhost:8080
- Dashboard: http://localhost:5173

The Vite dev server proxies `/api` to the API, so the dashboard uses same-origin requests and does not need `CORS_ORIGINS`. The Go process reads `internal/db/schema.sql` relative to the `cronsentry` directory, so start it from there.

## Before you change behavior

Read [AGENTS.md](AGENTS.md). It is the log of wrong assumptions that already broke CI, the API, or the dashboard. Add an entry when you hit a new one.

## Checks

GitHub Actions runs `go test ./...` and `go build -o /tmp/opensentry ./cmd` from `cronsentry/`, and `npm ci && npm run build` from `cronsentry/web`. `go build ./cmd` fails locally because the output name `cmd` is the source directory. `npm run lint` is not part of CI.
