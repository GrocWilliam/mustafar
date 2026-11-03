---
name: stack-i18n
description: Forge default for translation (i18n) - message catalogs per language, locale detection, plurals, number and date formatting. Use whenever a change adds or changes user-visible text, error messages, emails, or dates and numbers shown to users, and when setting up a new project's languages.
---

# Translation (forge default)

If the project already has an i18n setup (CLAUDE.md or a project skill), use it as it is. Otherwise set up the
minimal one below; the spec says which languages (default: English only, still through catalogs, so adding a
language later is only a new file).

## Setup (once)

- One flat JSON catalog per language: `src/i18n/en.json`, `src/i18n/fr.json`..., keys namespaced by screen:
  `"home.title"`, `"campaign.fear"`, `"error.nameRequired"`. English is the reference.
- A ~20-line helper, no library: `t(locale, key, params?)` looks up the locale, falls back to English, then to the
  key; replaces `{name}` placeholders; plurals with `Intl.PluralRules` and keys `item.count.one` / `item.count.other`.
- Locale: a `lang` cookie set by a language switcher, else the first supported `Accept-Language` language, else
  English. Resolve it once per request on the server and pass it down; set `<html lang>` to it.
- A unit test that every catalog has exactly the English keys and no empty value.

## Rules

- No user-visible string in code or markup: buttons, labels, placeholders, `aria-label`, `title`, `alt`, page
  titles, validation and API error messages shown to users, emails.
- Never build sentences by concatenation (`t('a') + name + t('b')`): one key with placeholders, so each language
  can order the words.
- Numbers, currencies and dates with `Intl.NumberFormat` / `Intl.DateTimeFormat` in the current locale; never
  hand-formatted.
- Add every new key to **all** catalogs in the same commit; translate properly (natural wording, correct accents and
  typography, e.g. French `« »` and a space before `: ; ! ?` where the project does so), never English placeholders.
- Data the user typed (names, notes) is never translated. Content with an official translation (rules, game
  terms) uses the official term.
- Remove keys nobody uses any more.

## Check

The catalog test passes; acceptance tests that show text check each shipped language (`lang` cookie per language).
