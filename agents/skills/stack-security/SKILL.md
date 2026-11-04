---
name: stack-security
description: Forge security checklist for web apps - input validation, injection (SQL, shell, path, HTML), authentication and authorization, secrets, sessions and cookies, dependencies. Use when writing or reviewing code that reads outside input, touches auth or permissions, builds SQL, shell commands, file paths or HTML, handles secrets, or adds a dependency.
---

# Security checklist (forge default)

If the project's CLAUDE.md or a project skill sets stricter rules, follow them. Each item is a rejection when it is
broken by the change under review; a pre-existing issue the change does not touch is a finding, not a rejection.

## Input

- Every input from outside (body, query, params, headers, cookies, env, files, third-party responses) is parsed by a
  schema before use (`stack-api`). Size limits on strings, arrays and uploads.
- No trust in client-side checks: the server validates again.

## Injection

- SQL: Drizzle query builders or `sql` template tags with interpolated values. Never `sql.raw()` or string
  concatenation with input; identifiers (columns, sort fields) from input go through an allow-list.
- Shell: no `exec`/`execSync` with a string built from input. Use `execFile`/`spawn` with an argument array.
- Paths: a path built from input is resolved and checked to stay inside its base directory (`path.resolve` then
  `startsWith(base + path.sep)`); reject `..` and absolute paths.
- HTML: rendered through the framework's escaping. `{@html}`, `dangerouslySetInnerHTML`, `innerHTML` and `v-html` only
  on content the app produced itself, or sanitized.
- Redirects: a redirect target from input is a relative path, or checked against an allow-list.

## Authentication and authorization

- Every new endpoint or page that reads or changes user data checks who is calling, on the server, before doing it.
- Authorization is per object: user A cannot read or change user B's record by changing an id in the URL.
- Passwords hashed with a slow hash (`argon2`, `bcrypt`, `scrypt`), never stored or logged in clear; compare secrets
  and tokens with a constant-time comparison.
- Sessions: random tokens of 128 bits or more (`crypto.randomBytes` / `crypto.randomUUID`), cookies `HttpOnly`,
  `SameSite=Lax` or stricter, `Secure` outside local development. State-changing requests are not GETs.

## Secrets and data

- No secret, token or password in the code, tests, fixtures, logs or error messages: read them from env vars.
- Error responses to clients carry no stack traces, SQL or internal paths.
- Personal data is not logged.

## Dependencies

- A new dependency is a known, maintained package with the exact name intended (no typo-squats), and it is needed:
  the platform or an installed dependency does not already cover it.
