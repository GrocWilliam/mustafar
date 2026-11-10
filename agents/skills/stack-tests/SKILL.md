---
name: stack-tests
description: Forge default for tests - unit tests next to the code (Vitest for TypeScript), real databases instead of mocks, and the recipe for a story's acceptance tests (run.sh, free port, Playwright). Use when writing tests, writing acceptance tests, or debugging a failing test or CI run.
---

# Tests (forge default)

## Unit tests (dev)

- The project's runner; for TypeScript, Vitest with `*.test.ts` next to the code (`npm install -D vitest`, script
  `"test": "vitest run"`, and `make test` runs `npm ci && npm test`).
- Test behaviour through the public function: each Zod schema (valid, each rejection, boundaries), domain functions,
  mappers, and handlers with a fake request.
- Database code runs against an in-memory PGlite with the real migrations: never mock SQL or the ORM.
- One concern per test, named after the behaviour; no snapshot tests of whole pages.

## Acceptance tests (tester only; frozen for everyone else)

Folder `tests/acceptance/us-N/`: `run.sh` plus test files (Node's `node:test` + `node:assert/strict` for JS
projects). Copy the newest existing `run.sh` when there is one. It must:

1. `cd` to the repository root; build only when `FORGE_PREBUILT` is unset.
2. Pick a free port in 10000-32767 (`node -e` snippet with `net.createServer`), never 0 or above 32767.
3. Start the server with an in-memory database (`PGLITE_DATA_DIR=memory://`, `DATABASE_URL` unset) in the background,
   logging to a temp file; wait for `GET /health` = 200; print the log on failure; kill the server on exit.
4. Run the tests and exit non-zero on failure.

Each criterion gets its expected case, plus the failure and boundary cases the story states or clearly implies:
invalid or missing input when the criterion names a validation (each rule, with the status and error it gives), the
limits it names (max length, 0 and 1 items, the last page), and not-found or not-allowed when it names an owner or an
id. Never test behavior the story does not imply: the dev cannot see your reasons, only your tests, and must not
chase rules nobody asked for. Name these tests after the criterion too (`rejects an empty title`).

Set up data through the public API, not the database. Browser tests:
`createRequire(process.env.PLAYWRIGHT_DIR + '/')('playwright-core')` and
`chromium.launch({ executablePath: process.env.CHROMIUM_PATH })`; never install browsers. Select by `data-testid`
and roles, never by CSS classes (Tailwind classes change). At least one test per acceptance criterion, named after it; check
every language the app ships when the story shows text.

## Debugging

Run one story: `make build && FORGE_PREBUILT=1 bash tests/acceptance/us-N/run.sh`. A failing frozen test after your
change: read what it really checks and fix the code, never the test. When the cause is not obvious, follow the
`stack-debug` skill.
