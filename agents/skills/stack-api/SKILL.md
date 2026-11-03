---
name: stack-api
description: Forge default for HTTP APIs and input validation - Zod schemas at every trust boundary, JSON error shape, status codes. Use when adding or changing an endpoint, a form handler, or any code that reads request bodies, query strings, params, env vars or external data. Any framework.
---

# API and validation (forge default)

If the project already validates another way (CLAUDE.md or a project skill), follow it. Otherwise, Zod 4
(`npm install zod`, `import * as z from 'zod'`).

## Validation

- Every input from outside is parsed by a Zod schema before use: JSON bodies, form data, query strings, route params,
  environment variables, files and third-party responses. Never trust a TypeScript type for those.
- One schema per input, next to the domain code (`src/server/<domain>.ts` or the project's equivalent), exported with
  its type: `export const NewThing = z.object({...}); export type NewThing = z.infer<typeof NewThing>;`
- Make the rules exact: `.trim()`, `.min()/.max()` on strings, `.int()` and ranges on numbers, `z.enum([...])`,
  `.strict()` objects when unknown keys are a mistake, `z.coerce.number()` only for form/query strings.
- Cross-field rules with `.refine(..., { error: '...', path: ['field'] })`.
- Custom messages with `{ error: '...' }` (Zod 4; not `message`). Messages shown to users are translation keys.
- Use `safeParse` in handlers; never let a ZodError escape as a 500.
- When the database uses Drizzle, derive row schemas with `drizzle-zod` (`createInsertSchema(table)`) instead of
  repeating the columns, then refine them.

## Endpoints

```ts
const parsed = NewThing.safeParse(await readJson(request)); // readJson: malformed JSON -> 400
if (!parsed.success) return json({ error: 'invalid input', issues: z.flattenError(parsed.error).fieldErrors }, { status: 400 });
```

- Error shape: `{ "error": "<short reason>" }`, plus `issues` (field → messages) for validation errors.
- Status codes: 200 resource, 201 created (with the resource), 204 deleted (no body), 400 invalid input, 404 unknown
  id, 409 conflict (duplicate), never 500 for bad input.
- Responses go through one mapper per resource: camelCase fields, never raw database rows, never secrets.
- Keep handlers thin: parse → call the domain function → map → respond. Domain functions take typed, already-valid input.
- Pages and forms call the same domain functions or the API; they never duplicate validation.

## Check

Unit-test each schema: a valid input, every rejection, and the boundaries (max length, min/max numbers). Then the
acceptance tests of the stories that use the endpoint.
