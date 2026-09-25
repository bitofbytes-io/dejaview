DejaView is a family movie night tracker: a watch list split into groups, who picked each movie, and everyone's ratings. The look is **Late Show**: a rented tape playing on a CRT at midnight. Scanlines, a soft vignette, film grain, red-and-cyan misregistration on the big type, and the VCR's own on-screen display (▶ PLAY, the tape counter) framing a clean, readable app. The tape is the atmosphere; the text you read is never on the tape's terms.

## Principles

1. **It's playing on a TV.** Scanlines, vignette and grain cover the whole screen, set by one tape-wear class on the page root: `tape-clean`, `tape-worn` (the default) or `tape-rewound`.
2. **The VCR talks in OSD.** VT323 is back, but only as the VCR's on-screen display: ▶ PLAY, the tape counter, TAPE 12, kickers, big counter numbers. 20px and up, a few words at a time. Never sentences, buttons or form labels.
3. **The big type is misregistered.** Display headings, the wordmark and OSD text carry `rgb-split`, the red and cyan ghost of a worn tape. Body text stays sharp.
4. **One neon sign.** `neon-rose` is the brand light: the primary action, the current place. The header wears the tape stripe (rose, amber, cyan). The other neons carry meaning: people, ratings, focus, rank one.
5. **Readable under the tape.** Body text is Archivo at 16px or more. Scanlines darken one row in three; `ink` and `ink-muted` stay above 5.8:1 even on those rows at `tape-worn`.

## Voice

- Warm, short, a little playful. It's a family app, not a cinema chain.
- Sentence case everywhere: "Save ratings", "Add movie", "Trophy room". Uppercase only in the `label` style (kickers, nav, field labels), kept to three words.
- Say what happened: "Added The Goonies to Group 12", "Couldn't save ratings. Try again."
- Cut theme-park copy. "Enter Theater" becomes "Sign in". "High fives, popcorn, and the movies we loved most" becomes "The movies we loved most". The VCR does the winking (▶ PLAY, SP 0:12:34, TAPE 12, "Be kind, rewind" in the marquee), so the sentences don't have to.
- No emoji in UI copy or as icons.

## Color

The theme is `late-show`, dark only.

- Page ground is `screen`. Containers are `surface`, lifted items `surface-raised`, wells inside a card `surface-sunken`.
- Text is `ink`; secondary text `ink-muted`; placeholders and disabled labels only `ink-faint`.
- Hairlines are `line` (decorative). Anything you click or type into has a `line-control` border.
- Text or icons on any neon fill are `on-neon`, never white. White on rose is 2.7:1; `on-neon` is 6.3:1.
- People: `person-daniel` (cyan), `person-jennifer` (rose), `person-caleb` (amber), `person-aiden` (violet). Always as a solid disc with the initial in `on-neon`, so color is never the only cue.
- Ratings: `rating-low` under 4, `rating-mid` 4 to under 7, `rating-high` 7 and up (the thresholds in `model.ScoreColorClass`). The number is always printed.
- Errors and destructive actions use `signal-error` (orange) with a word or icon, so they never read as the rose brand color.
- No gradients except the poster scrim and the scanline and vignette layers. No purple-to-pink fills.

## Type

Four families, all on Google Fonts:

```html
<link href="https://fonts.googleapis.com/css2?family=Archivo:ital,wght@0,400..800;1,400&family=IBM+Plex+Mono:wght@400;500;600&family=Unbounded:wght@500..800&family=VT323&display=swap" rel="stylesheet">
```

- **VT323** (`osd`): the VCR's on-screen display. `osd` 22px for ▶ PLAY, the tape counter, TAPE labels, kickers and the marquee; `osd-lg` 30px for the average and top-five scores; `osd-xl` 44px for the family totals. Always uppercase. Never below 20px, never a sentence.
- **Unbounded** (`display`): wide and heavy, the VHS-sleeve voice. Use it for `display-xl`, `display-lg`, `title` and `wordmark` only, never for paragraphs or buttons.
- **Archivo** (`sans`): everything you read. `body` 16/24 is the default; `body-lg` for the synopsis; `body-sm` for metadata; `label` for uppercase kickers.
- **IBM Plex Mono** (`mono`): numbers. Scores (`score`, `score-lg`), years, runtimes, counts (`meta`), always tabular.
- Inputs are never below 16px, so iOS doesn't zoom.
- On phones under 480px, `display-xl` drops to 32/36 and `display-lg` to 24/30.

Press Start 2P is gone and VT323 no longer sets body text. Those two were the main readability problem: VT323 at 16px has a tiny x-height and pixel joins that blur on phones. At 22px and up, in short uppercase bursts, VT323 reads fine and says "VCR" instantly.

## Texture: the tape

| Layer | Class | tape-clean | tape-worn | tape-rewound |
| --- | --- | --- | --- | --- |
| Scanlines (3px pitch, dark) | `dv-crt` | 0.10 | `scanline-opacity` 0.18 | 0.26 |
| CRT vignette | `dv-crt` | off | `vignette-opacity` 0.28 | 0.40 |
| Film grain | `dv-grain` | 0.05 | `grain-opacity` 0.08 | 0.11 |
| RGB split on display and OSD type | `rgb-split`, `osd` | off | `rgb-split` | `rgb-split-strong` |
| Tracking noise band | `tracking-band` | off | on | on |

- Put `dv-page dv-grain dv-crt` on `<body>` and the layers on fixed pseudo-elements, so they cover the viewport, not the document.
- Posters get their own scanlines (1.4 × the page's) and, on hover, the `chroma-edge`: a red and cyan ghost either side, like a tape slipping.
- The tracking band is a strip of static drifting down behind the Trophy Room hero. It stops under `prefers-reduced-motion`, as does the OSD blink.
- The header's bottom edge is the tape stripe: 2px each of `neon-rose`, `neon-amber`, `neon-cyan`, with a soft rose glow.

## Space, shape, depth

- 4px grid: `space-1` … `space-16`. Page gutter `space-4` on phones, `space-8` from 1024px. Card padding `space-4`, then `space-6` from 640px. `space-12` between groups.
- Corners: `radius-sm` for badges and thumbnails, `radius-md` for buttons, inputs and posters, `radius-lg` for cards, `radius-pill` for person discs and rank discs.
- Depth: `shadow-card` at rest, `shadow-lift` on hover and toasts.
- Every tap target is at least 44px.

## States

- Focus: 2px solid `focus` (`neon-cyan`) outline with a 2px offset, on every interactive element. No glow on focus.
- Hover: a surface step up (`surface` to `surface-raised`) or a `line-control` border. Poster cards lift 3px and show the `chroma-edge`.
- Disabled (logged out): `disabled-opacity`, plus a one-line note in `body-sm` saying why.
- Motion: 150–200ms ease. The marquee scrolls once per 60s; the ▶ PLAY cue blinks; the tracking band drifts. All three stop under `prefers-reduced-motion`.

## Iconography

- Use the line icons in the Icons group (from `icon.templ`): 24px grid, 1px stroke in `currentColor` in the app (the stored SVGs are drawn in `ink` so they show as images). Set them at 1em beside text, 24px in trophy tiles.
- Replace the 23 emoji icons (👑 🎰 🍿 📼 and friends) with line icons in the same style. Emoji are the biggest source of the "AI cheesy" feel and render differently on every phone.
- Icon-only buttons need an `aria-label`.

## Logo

- In the app, the mark is the clapperboard line icon in `neon-rose` next to the `wordmark`: "Deja" in `ink`, "View" in `neon-rose`, with a faint rose glow.
- The app icon (Logos group) is the current orange eye-and-film mark. It's off-palette; recolor it to `neon-rose` on `screen` when the redesign ships.

## Layout

- Dashboard: header, Add movie card, then groups newest first. Poster grid: 2 columns on phones, 3 from 640px, 5 from 900px.
- Movie detail: poster and group card stacked above details on phones; 1/3 + 2/3 from 1024px.
- Trophy room: hero, bonus banner, trophy cards (stacked on phones, 3 across from 768px), top five, totals.
