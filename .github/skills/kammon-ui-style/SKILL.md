---
name: kammon-ui-style
description: 'Use when creating or changing KAMmon screens: templates, CSS, charts, components and wording. Defines the visual tokens, typography, components, honest-label rules and accessibility checks for the KAMmon web UI.'
---

# KAMmon UI Style

Full design: [docs/DESIGN.md](../../../docs/DESIGN.md). This file is the short version for UI work.

## Look

A calm, dense operator console. Dark by default, light on request. No cards, gradients, glows or shadows (the toast is the only shadow). Hierarchy comes from type size and spacing; 1 px rules separate regions.

## Tokens (`web/static/kammon.css`)

Same names as the KAM manuals. Dark / light:

| Token | Dark | Light |
|---|---|---|
| `--bg-0`, `--bg-1`, `--bg-2`, `--bg-3` | `#101012`, `#18181b`, `#222225`, `#2c2c30` | `#ffffff`, `#f8fafc`, `#f1f5f9`, `#e2e8f0` |
| `--text-primary` / `-secondary` / `-muted` | `#f5f5f7` / `#c5c5ca` / `#a1a1a8` | `#0f172a` / `#334155` / `#526174` |
| `--signal` (headings, links, primary action, focus) | `#80b8ff` | `#2563eb` |
| `--success`, `--warning`, `--danger` | `#3ddc91`, `#f5b544`, `#ff7188` | `#147a43`, `#a45b00`, `#c42c48` |

Blue is for headings and actions, never for status. Green, amber and red mean a state and always come with a word or a shape.

## Type

- Headings: Syncopate, solid `--signal`. h1 28 px (24 px on phones), h2 16 px, h3 14 px. Keep them short.
- Body: Hanken Grotesk 15 px. Small 13 px. Micro 12 and 11 px. Data in Roboto Mono.
- Letter spacing stays zero. Fonts are self-hosted; the system sans-serif is the fallback.

## Layout

Left sidebar (224 px), top bar, page heading. Page patterns: metric strip plus split, master-detail, filter plus table, form plus snapshot. Breakpoints 1600, 1100, 767 and 360 px. Tables scroll inside their own box; the page never scrolls sideways.

## Wording

- "Remembered clients", not "online". "Last heard", not "last seen online".
- "Estimated airtime". "Observed packet airtime", not "occupancy".
- "Echo matches", not "verified".
- **Stale Node** means silent for over 24 hours; the session has not expired. **Unknown** means no time is recorded.

## Rules

- Server decides access; templates never decide visibility (`internal/view`).
- Escape all radio text. No `template.HTML` except numbers-only SVG in `internal/web/chart.go`.
- Meaning never depends on colour alone.
- Visible focus (3 px `--signal` outline), labelled controls, `aria-current` on the active page, `aria-live` on logs and toasts, real `<table>` markup.
- Charts are server-drawn SVG with a text alternative; colours come from CSS variables so a theme switch needs no redraw.
- `prefers-reduced-motion` turns off the 140 ms hover transition.
