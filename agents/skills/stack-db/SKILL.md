---
name: stack-db
description: Forge default for databases - Drizzle ORM schema, queries and drizzle-kit migrations on PostgreSQL (PGlite in tests). Use when a story stores something new, changes a table, adds a query, or sets up the database of a new project.
---

# Database (forge default)

If the project already uses another data layer (CLAUDE.md or a project skill), follow it. Otherwise Drizzle ORM on
PostgreSQL, with PGlite when `DATABASE_URL` is unset (dev, tests, CI: no database server there).

## Setup (once)

```sh
npm install drizzle-orm pg @electric-sql/pglite && npm install -D drizzle-kit @types/pg
```

```ts
// drizzle.config.ts
import { defineConfig } from 'drizzle-kit';
export default defineConfig({ dialect: 'postgresql', schema: './src/server/db/schema.ts', out: './drizzle' });
```

```ts
// src/server/db/index.ts: one connection, migrated before first use
import { drizzle as pgDrizzle } from 'drizzle-orm/node-postgres';
import { migrate as pgMigrate } from 'drizzle-orm/node-postgres/migrator';
import { drizzle as liteDrizzle } from 'drizzle-orm/pglite';
import { migrate as liteMigrate } from 'drizzle-orm/pglite/migrator';
import * as schema from './schema';
import { PGlite } from '@electric-sql/pglite';
import { Pool } from 'pg';
// DATABASE_URL set: pgDrizzle({ client: new Pool({ connectionString: url }), schema }), then
//   await pgMigrate(db, { migrationsFolder: 'drizzle' })
// else: liteDrizzle({ client: new PGlite(process.env.PGLITE_DATA_DIR ?? './data/pglite'), schema }), then liteMigrate
// Cache the promise so migrations run once; the Docker image must contain the drizzle/ folder.
```

## Changing the schema

1. Edit `schema.ts` (`pgTable`). Conventions: `id: text().primaryKey().$defaultFn(() => crypto.randomUUID())`,
   `createdAt: timestamp({ withTimezone: true }).notNull().defaultNow()`, foreign keys with
   `.references(() => parent.id, { onDelete: 'cascade' })`, indexes in the table's third argument. PostgreSQL does not
   index a foreign key's column: add an index when queries filter or join on it, or when deleting the parent cascades.
2. `npx drizzle-kit generate --name short_name` and commit everything it writes under `drizzle/` (SQL and `meta/`).
   It needs no database. If it asks about a rename, you are headless: avoid renames (add the new column, migrate
   the data, drop the old one in a later story).
3. Never edit or delete a generated migration that is on `main`: deployed databases applied it. Never `drizzle-kit push`.
4. A new column on an existing table needs `.default(...)` or is nullable: rows already exist.

## Queries

- Before writing relations, read the `drizzle-orm` version in `package.json` and the lockfile, not an npm tag: 0.x uses
  `relations()`, v1 uses `defineRelations()`. Never mix them.
- Use the query builder or `db.query.<table>` with typed results; `sql` template only for what the builder cannot
  express (it parameterizes; never build SQL strings by concatenation).
- One statement with `.returning()` beats read-then-write; multi-step writes go in `db.transaction(async (tx) => ...)`.
- Validate input with Zod first (stack-api skill); `drizzle-zod` can derive schemas from tables.

## Check

Unit tests run against a fresh in-memory PGlite with the migrations applied (no mocks of the database). Then build,
start the app with `PGLITE_DATA_DIR=memory://` and check `/health`: a broken migration fails it, and the forge reverts
the deployment.

## Real PostgreSQL in tests

When PGlite is not enough (another process such as a Go service shares the database, or the project uses extensions or
`LISTEN`), start a throwaway server: the image has the PostgreSQL 16 binaries on `PATH`, usable as the node user.

```sh
d=$(mktemp -d); port=$(node -e "const s=require('net').createServer().listen(0,()=>{console.log(s.address().port);s.close()})")
initdb -D "$d/data" -U postgres -A trust >/dev/null
pg_ctl -D "$d/data" -o "-k $d -p $port -c listen_addresses=localhost -c fsync=off" -l "$d/log" -w start >/dev/null
export DATABASE_URL="postgres://postgres@localhost:$port/postgres"
# ... tests ...; on exit: pg_ctl -D "$d/data" -m immediate stop; rm -rf "$d"
```

One server per test run (or per acceptance script), a fresh database per test file (`CREATE DATABASE t_<random>`).
