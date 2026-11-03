---
name: stack-ui
description: Forge default for UI and styling - Tailwind CSS v4, design tokens, mobile-first layout, accessibility. Use when building or restyling any page, component, form or layout, or when setting up styling in a new web project. Any framework.
---

# UI and style (forge default)

If the project already styles another way (its CLAUDE.md or a project skill says so), follow the project. This skill
is the default for new projects and new styling setups.

## Setup (once, in the first story that has UI)

Tailwind CSS v4, CSS-first, no `tailwind.config.js`:

```sh
npm install -D tailwindcss @tailwindcss/vite     # Vite-based frameworks (SvelteKit, Vue, React+Vite, Astro...)
# otherwise: npm install -D tailwindcss @tailwindcss/postcss, with "@tailwindcss/postcss" in postcss.config.mjs
```

```ts
// vite.config.ts
import tailwindcss from '@tailwindcss/vite';
export default defineConfig({ plugins: [tailwindcss(), /* the framework's plugin */] });
```

```css
/* the one global stylesheet, imported by the root layout */
@import "tailwindcss";

@theme {
  --color-bg: #...;   --color-surface: #...;  --color-text: #...;  --color-muted: #...;
  --color-accent: #...; --color-danger: #...;
  --font-sans: "...", system-ui, sans-serif;
}
```

Tokens in `@theme` become utilities (`bg-surface`, `text-muted`, `border-accent`). Record the palette in CLAUDE.md.

## Rules

- Style with utility classes in the markup. Only global element defaults (in `@layer base`) and the rare thing
  utilities cannot express go in the stylesheet. No inline `style=`, no per-component CSS files, no other CSS framework.
- Colours only from the theme tokens: never a raw hex or a default Tailwind colour (`red-500`) in markup.
- Repeated class lists become a component of the framework, not `@apply` soup.
- Mobile first: unprefixed classes are the phone layout, then `sm:` `md:` `lg:`. No horizontal scroll at 375px.
- Accessibility: every control has a visible label (or `aria-label`), touch targets at least 44×44 px (`min-h-11
  min-w-11`), a visible focus ring (`focus-visible:outline-2 focus-visible:outline-accent`), text contrast at least 4.5:1,
  form errors in an element with `role="alert"`, semantic elements (`button`, `nav`, `main`, `table` with `th scope`).
- Every user-visible string goes through translation (stack-i18n skill).
- Give elements that tests must find a stable `data-testid`; never rename an existing one.

## Check

Build the app, open the pages you touched with Playwright at 375px and 1280px wide, and check for horizontal scroll
(`document.documentElement.scrollWidth <= innerWidth`).
