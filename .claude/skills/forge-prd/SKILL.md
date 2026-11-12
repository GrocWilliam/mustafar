---
name: forge-prd
description: Start a new forge project from an idea - a guided interview that settles the product (PRD), the tech stack, the architecture (frontend/backend organisation, modules, data) and the design, writes them as docs/prd.md, docs/architecture.md and docs/ui.md in the new repo, then opens its first spec. Use for "new project", "start a project", "help me write a PRD", "what architecture for", "guide me from scratch". For a feature on an existing project, use forge-spec.
---

# Forge PRD

The three documents become the new repo's reference: the `architect` agent designs every spec within
`docs/architecture.md`, the `planner` plans within `docs/prd.md` and `docs/ui.md`, and the `reviewer` rejects changes
that break `docs/architecture.md`. Agents never ask questions, so every decision that matters is settled here, with the
user. Nothing is paid until the first spec opens, so take the time.

Work from the forge root. Never print the admin password or `.env` values.

## 1. Interview

Rounds, as in `forge-spec`: each round asks, numbered, every open question whose prerequisites are settled, with your
recommended answer worded so that "yes" accepts it. Wait for the answers before the next round. Skip what the user
already said. Explain a choice in one line when the user may not know the trade-off; they asked to be guided.

**Round 1, product.** The problem and who has it. The users (roles, how many, signed in or not). The one journey that
must work first. The MVP features, and what comes later. Non-goals. Web app, API, CLI or several. Languages of the UI.
Data that is personal or must never be lost.

**Round 2, capabilities.** A capability map: a table of modules (kebab-case id, responsibility, depends on), with
dependencies in one direction, no cycles, and the build order. The first module is the thinnest slice that runs end to
end (often: the core entity, its list and its form). Each module becomes one spec later, so each must fit 20 stories.

**Round 3, tech and organisation.** Recommend from these, by default the first:

| Shape | When | Organisation |
|---|---|---|
| Full-stack monolith: SvelteKit 2 (Svelte 5), server-rendered, form actions | one web app, one team, no other client | `src/routes/` (pages, thin), `src/lib/server/<module>.ts` (domain + DB, the only code that touches the DB), `src/lib/components/` (UI), `src/lib/<module>.ts` (shared types, Zod schemas) |
| API + SPA in one repo: Hono or Fastify JSON API serving a Vite SPA | other clients will call the API (mobile, third parties) | `server/<module>/` (routes, service, repository), `web/` (SPA), `shared/` (Zod schemas both import); one process on one port |
| API only, or CLI | no human UI | `src/<module>/`, one entry point |

Then: dependency rules (routes → domain → DB, never the reverse; modules talk through each other's exported functions,
never each other's tables), auth (none, session cookie with password, or OAuth; recommend the simplest that fits the
users), the error shape, TypeScript strict. The forge's defaults, which its agents already know as skills: Tailwind CSS
v4, Drizzle ORM on PostgreSQL with PGlite when `DATABASE_URL` is unset, Zod at every trust boundary, Node's test runner
and Playwright for acceptance tests. Departing from them is allowed, but say what it costs.

Forge constraints to state, not ask: CI and agents run Debian bookworm with Node 26, Python 3, make, git and Chromium,
and no Docker in tests, so the database runs embedded (PGlite, SQLite) and `make test` installs anything else. A
service listens on `$PORT` (8080) and answers `GET /health` with 200, so it gets a `Dockerfile` and a staging deploy.
External services (mail, payment, storage) need a fake in tests: decide which.

**Round 4, design.** The tone (sober tool, playful, dense dashboard), light/dark, the brand colours or a neutral
palette, the type scale, mobile-first or desktop-first and the breakpoints, the layout grid of a typical page, the
navigation (top bar, sidebar, tabs), the shared components (button, field, card, table, modal, toast), the states of a
screen (empty, loading, error, success), and accessibility (WCAG 2.2 AA, 44px targets by default). Propose the page
list from the MVP journey and get it agreed.

## 2. Write the documents

In the scratchpad, `docs/` holding three files, each the reference an agent follows, not a discussion: decisions with
their one-line reason, no history, no open questions (decide them, and say so).

- `docs/prd.md`: Problem, Users, Goals (measurable), Non-goals, Features (MVP, then later, each tagged with its module
  id), the MVP journey step by step, Business rules (exact: limits, formulas, states and their transitions), Data
  (what is kept, personal data, retention), Success criteria.
- `docs/architecture.md`: Stack with versions, Shape and folder layout, Module map (the round 2 table and build
  order), Dependency rules, Data model (entities, fields, constraints, ownership per module), Interfaces conventions
  (routes or endpoints, JSON error shape, status codes), Auth, Configuration (env vars), Deployment (port, `/health`,
  `Dockerfile`), Testing (`make test`, where acceptance tests live, the fakes).
- `docs/ui.md`: design direction, tokens (colours light and dark, type scale, spacing, radius), breakpoints and the
  page grid, navigation, the pages and the links between them, the shared components and their variants, the screen
  states, accessibility rules, and the `data-testid` naming rule (kebab-case, `<page>-<element>`). Leave it out for
  a project with no UI.

Show the three files to the user and change them until they agree. Long files: give the path
(`/full/path/in/scratchpad`) and a summary of each, rather than pasting everything.

## 3. Create the project and its first spec

1. Ask for the repo name if not settled (lowercase, `[a-z0-9-]`).
2. Write the spec of the first module with `forge-spec` steps 2 and 4: the spec describes only that module's
   behaviour and says `Follow docs/prd.md, docs/architecture.md and docs/ui.md`; the documents carry the rest. Show it
   and get the user's OK: opening it starts paid runs.
3. Check the orchestrator runs (`curl -s localhost:8000/health`), then
   `./new-project.sh NAME /path/to/spec.md /path/to/docs`: it commits the documents before `main` is protected.
4. Hand over as `forge-spec` does, and add: the other modules are opened later with `forge-spec`, in the build
   order, each ending with `Depends on #N` of the previous one; change a document through a spec, since `main` refuses
   direct pushes.
