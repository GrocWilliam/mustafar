# Project memory

<!-- The forge's agents read this file on every run. The first story fills it in; every story keeps it true. -->

What this project is, in two sentences: _not written yet: the first story fills it in._

This file is the project's memory for the forge's agents. Keep it short, true and current: update it in the same commit
as any change that makes it wrong, and add what the next agent would otherwise rediscover the hard way.

## Stack

_Language, framework, database, test framework, and the versions that matter._

## Layout

_Where each kind of code lives, one line per folder or key file._

## Conventions

_Patterns every change follows: validation, errors, naming, styles, migrations, translations._

## Commands

```sh
make test     # what CI runs first
make build    # if the app has a build step
# how to run one story's acceptance test, how to run the app
```

## Skills

The forge's defaults are the `stack-ui`, `stack-api`, `stack-db`, `stack-tests`, `stack-i18n` and `stack-security` skills. Where this
project differs, say so here; project-specific how-tos go in `.claude/skills/`.

## Pitfalls

<!-- One line per trap that cost an attempt: what happened, what to do instead. -->
