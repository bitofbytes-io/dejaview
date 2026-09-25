# Migrating from Retro VHS Night

How to move `tailwind/styles.css` and the Templ markup onto Late Show. The component class names in `components/bundle.css` match the existing markup, so most of the work is swapping the `:root` block and the fonts link.

## Tokens

| Old variable | Old value | New token | New value |
| --- | --- | --- | --- |
| `--color-theater-black` | `#1a0a2e` | `screen` | `#0d0a12` |
| `--color-surface` | `#2d1b4e` | `surface` | `#16121d` |
| `--color-surface-raised` | `#3d2b5e` | `surface-raised` | `#201a2a` |
| (none) | | `surface-sunken` | `#08060b` |
| `--color-curtain-red` | `#ff2d95` | `neon-rose` | `#ff4f93` |
| `--color-curtain-dark` | `#cc2477` | `neon-rose-deep` | `#d93a78` |
| `--color-gold` | `#00f0ff` | `neon-cyan` | `#4de3f5` |
| `--color-gold-muted` | `#00c4d4` | `line-control` (borders) or `neon-cyan` | |
| `--color-cream` | `#e0d8f0` | `ink` | `#f3eff7` |
| `--color-cream-muted` | `#8a7e9e` | `ink-muted` | `#b9b0c7` |
| `--color-rating-mid` | `#ffe600` | `neon-amber` | `#ffc857` |
| `--color-rating-high` | `#4ade80` | `neon-mint` | `#6ff0b0` |
| `--color-error` | `#ef4444` | `signal-error` | `#ff7a45` |
| `--color-person-*` | | `person-*` | aliases of the neons |
| `--font-display` | Press Start 2P | `display` | Unbounded |
| `--font-body` | VT323 | `sans` | Archivo (VT323 stays as `osd`, for the VCR display only) |
| `--font-mono` | Space Mono | `mono` | IBM Plex Mono |

The old names were misleading (`gold` was cyan, `curtain-red` was pink), which made the palette hard to reason about. The new names say what the color is.

## Contrast fixes

- Muted text was `#8a7e9e`: 4.0:1 on `surface` and 3.3:1 on `surface-raised`, failing for small helper text (and often dimmed further with `opacity-50`/`opacity-70`). Now 8.1:1 or more.
- Primary buttons were cream on pink: 2.5:1. Now `on-neon` on `neon-rose`: 6.3:1.
- Input borders were `surface-raised` on `surface`: 1.2:1. Now `line-control`: 3.5:1 or more.
- Picker badges were colored text on an 85% scrim over a poster, so they varied poster to poster. Now solid discs.

## Markup changes

- `base.templ`: swap the Google Fonts link (see the brand book). Add `dv-page dv-grain dv-crt tape-worn` to `<body>` and use `position: fixed` for the grain and CRT layers. The old `body::before` vignette and `body::after` scanlines are replaced by `dv-crt` (same idea, tuned so text stays readable).
- Header: one row at all widths. Wrap "View" in `<em>`. Use `nav-link` instead of `btn-secondary` for nav, and set `aria-current="page"`.
- Replace `text-marquee`, `text-gold`, `text-neon-glow` and `shadow-glow`. Neon glow text-shadows become `rgb-split`, on display type and `osd` text only.
- Header: add the `header-osd` block (▶ PLAY and the tape counter) between the wordmark and the nav.
- `group-section`: drop the cyan left border. Add the `group-head` row. (An earlier draft also had a TAPE 12 label; it was dropped as redundant with the group name.)
- Rating rows: add the person class and an `initial` disc. Make the clear control a `btn-icon` with a label.
- Delete button: use `btn-danger` instead of the inline `text-red-400 border-red-400` utilities.
- Toasts: the script builds `toast-icon` spans instead of solid green or red panels.
- Icons: replace the `icon-emoji` branches in `icon.templ` with line SVGs.
- Update `site.webmanifest`: `theme_color` and `background_color` to `#0d0a12`, and fill in `name` and `short_name`.
