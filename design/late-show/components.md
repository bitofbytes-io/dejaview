# Components

Guidelines for each component in `components.css`. Class names match the Templ markup in `internal/ui`. Rendered examples are in the design system artifact and the mockups.

## Header

The app bar: DejaView wordmark on the left, the VCR display in the middle, text nav on the right, and the tape stripe (rose, amber, cyan) along the bottom.

### Use
- One row at every width (the old header stacked on phones and ate the fold). 44px tap targets.
- Wordmark: the clapperboard mark plus `wordmark` style in `ink` with `rgb-split` and a faint rose glow; "View" in `neon-rose`.
- `header-osd` holds two `osd` spans: a blinking ▶ PLAY and the tape counter (SP 0:12:34) in `ink-muted`. It's decoration, so mark it `aria-hidden`. Hidden under 720px, where the marquee carries the VCR feel instead. The counter can show the total runtime watched, or just tick.
- Nav items use `nav-link` (uppercase `label`, `ink-muted`); the current page gets `aria-current="page"` and a rose underline.
- The marquee, if kept, sits above the header (see Marquee).

### Consumer supplies
Auth state and current route.

## Marquee

The Now Playing ticker as a VCR on-screen display strip: `surface-sunken` with heavy scanlines, VT323 `osd` text in `ink` with `rgb-split`, and star separators in `neon-amber`.

### Use
- Optional. It is decoration, so it never carries information.
- Scrolls once per 60s (was 20s) and stops under `prefers-reduced-motion`.
- Replaces the pink-to-cyan gradient bar, which was the loudest element on every page.
- Show it on phones too: there it is the main VCR cue, since the header display is hidden.
- Mark it `aria-hidden="true"`.

### Consumer supplies
Nothing; the copy is fixed.

## Button

Four buttons carry every action in DejaView: `btn-primary` for the one thing a screen is for, `btn-secondary` for everything else, `btn-danger` for removing a movie, `btn-icon` for icon-only controls.

### Use
- One `btn-primary` per view: Save on the ratings card, Add on a search result, Enter on login.
- `btn-primary` is a `neon-rose` fill with `on-neon` text. Never white text on rose (2.7:1).
- Its hover is the only button glow (`glow-rose`). Secondary, danger and icon buttons stay flat.
- Every button is at least 44px tall. Add `btn-sm` (36px) only in dense desktop rows, never on phones.
- Labels are sentence case in the sans (`body-strong`). The old uppercase pixel labels are gone.
- `btn-danger` is `signal-error` orange, outlined, filling on hover. Always a word, never an icon alone.
- `btn-icon` needs an `aria-label`.

### Consumer supplies
A `<button>` or `<a>` with the class, a label, and `disabled` when logged out (draws at `disabled-opacity`).

## Input

Text fields and selects share one class, `input-field`: a `surface-sunken` well with a `line-control` border that meets 3:1.

### Use
- 48px tall, 16px text. Never smaller: iOS zooms into anything under 16px.
- Label above with `field-label` (uppercase `label` style, `ink-muted`). Placeholder is `ink-faint` and is never the only label.
- Focus is the shared `focus` ring: 2px solid `neon-cyan`, 2px offset. No glow.
- Errors: set `aria-invalid="true"` and put the message in `field-error` below, in words.
- Selects use the same class; `color-scheme: dark` keeps the native menu dark.

### Consumer supplies
The `<input>` or `<select>`, a `<label for>` and any help or error text.

## Card

The container for every block of content: `surface` fill, `line` hairline, `radius-lg`, `shadow-card`. 16px padding on phones, 24px from 640px.

### Use
- Title with `card-title`: the display face at `title` size, `ink`, optional 20px icon. No text glow.
- Split sections with `divider`.
- Don't nest cards. Inside a card, group things in `surface-sunken` wells (search results, rating rows).
- No colored left borders, no gradient fills.

### Consumer supplies
Content and an optional title.

## GroupSection

The heading row for one group of movies: the group name in the display face and the count on the right.

### Use
- Groups are separated by a `line` rule and `space-12`, not by a neon left border.
- The current group adds `is-current`, which prints a rose ▶ PLAY tag in `osd` type.
- Count in the mono `meta` style, `ink-muted`.
- The poster grid follows directly (see PosterCard).

### Consumer supplies
Group number, movie count, whether it is current.

## PosterCard

A movie in a group: the poster, who picked it, and everyone's ratings. The main unit of the dashboard.

### Use
- `poster-card` holds the TMDB poster (`poster-image`, 2:3) or, with no poster, `poster-placeholder`: a blank sleeve with the title in the display face at the top and the year in `meta` under it, clear of the picker badge.
- `picker-badge` sits top right: a solid disc in the picker's person color (`person-daniel`, `person-jennifer`, `person-caleb`, `person-aiden`) with `on-neon` initial. Solid fills read on any poster; the old outlined neon initials did not.
- Ratings sit on `poster-overlay`, a dark scrim, as `RatingBadge`s showing the person's initial.
- Scanlines run over the poster art at 1.4 × the page's `scanline-opacity`, so posters look like they're on the same TV.
- Hover lifts 3px with a `neon-rose` border and the `chroma-edge`: a red and cyan ghost either side, like a tape slipping.
- Signed in, wrap it in `draggable-item` with a `drag-handle` (top left). On touch screens the handle stays visible; with a mouse it appears on hover.
- Grid: `poster-grid`, 2 columns on phones, 3 from 640px, 5 from 900px.

### Consumer supplies
The entry link, poster URL or title and year, picker initial and person class, ratings.

## RatingBadge

A score or a person's initial on a solid fill in the rating color: `rating-low` (under 4), `rating-mid` (4 to under 7), `rating-high` (7 and up), matching `model.ScoreColorClass`, `rating-empty` for no score.

### Use
- Text is always `on-neon` in the mono `score` style, so the number or initial carries the meaning and color only reinforces it.
- `rating-badge-lg` is the average on the detail page.
- Empty is `surface-raised` with a `line-control` outline and an em dash.

### Consumer supplies
The score or initial and the color class from `model.ScoreColorClass`.

## RatingInput

One family member's score on the detail page: `rating-row` with the person's initial disc and name, and a `rating-input` (0–10, step 0.5).

### Use
- Rows sit in a 1-column list on phones, 2 columns from 640px.
- The input is 88×44 with an 18px mono value: big enough to tap and read.
- The person disc uses the person color class on the row (`person-daniel` etc.).
- A clear control, when present, is a `btn-icon` with `aria-label="Clear rating"`, not a bare red ×.

### Consumer supplies
Person name, initial and class, the current score, the input name.

## SearchResult

A TMDB match in the Add Movie card: thumbnail, title, year, and an Add button on the same row.

### Use
- Phones: 56px thumbnail, title (2 lines max) and year, a full-size `btn-primary` Add on the right (44px tall; never `btn-sm` on phones). The overview is hidden to keep results scannable.
- From 640px: 72px thumbnail and a 2-line overview in `ink-muted`.
- Rows are `surface-sunken` wells inside the card; hover shows a `line-control` border.
- Title in `body-strong` `ink`, never the neon cyan titles of the old theme.

### Consumer supplies
Poster URL, title, year, overview, the add form.

## Toast

Confirmation after an action: `surface-raised` panel, top right, with a round status icon and a short message.

### Use
- `toast-success`: `neon-mint` disc with a check. `toast-error`: `signal-error` disc with "!" and an orange border.
- The message says what happened in plain words ("Added The Goonies to Group 12").
- Replaces the solid green and red toasts with white text.
- Slides in, gone after 3s; errors should stay until dismissed.

### Consumer supplies
Message and type, via the existing `showToast` event.

## TrophyCard

One family award: icon, title, a one-line description, the winner chips and the stat that won it.

### Use
- Stacked full-width on phones, three across from 768px. Compact: no 18rem minimum height.
- The icon sits in a `surface-sunken` tile; use the line icons from the Icons group, not emoji.
- Winners are chips: a person-color disc with initial plus the name. No winner yet shows a `?` disc and "Still up for grabs".
- The stat sits under a hairline in the mono `meta` style with the number in `neon-amber`.

### Consumer supplies
Title, description, icon name, winners, value text.

## TopMovieRow

A row in the Family Top Five: rank disc, thumbnail, title with year and group, and the family score.

### Use
- Rank 1 is a `neon-amber` disc with `glow-amber` (the only glow in the Trophy Room), rank 2 `ink-muted`, rank 3 `medal-bronze`, 4–5 outlined.
- Score in mono `neon-amber`; the "Family score" caption appears from 640px.
- On phones, meta shows the year and group only; the title truncates to one line.

### Consumer supplies
Rank, entry link, poster, title, year, group, picker, average.

## FamilyTotals

Three running totals in one bordered strip: movies watched, time together, family-rated movies.

### Use
- Stays three across on phones (numbers are short); labels drop to 11px there.
- Numbers in mono at 22px (28px from 640px), `ink`. Labels uppercase `ink-muted`.
- No gradient background; hairline dividers only.

### Consumer supplies
The three values.

## AdvantageBanner

Who holds the 3-pick bonus for the next draw: a big person-color disc, a headline and a line of detail.

### Use
- `surface-raised` panel with a 1px `neon-rose` border, replacing the solid pink gradient.
- The disc takes the holder's person class; no holder shows `?` on rose.
- The ticket count on the right hides under 480px.

### Consumer supplies
Holder (or none) and the group that earned it.

## LoginCard

The sign-in screen: centered card with the wordmark, one token field and one primary button.

### Use
- Wordmark at `display-lg` with the clapperboard mark in `neon-rose`.
- Tagline in `ink-muted`: "Family movie night tracker."
- Button label "Sign in" (was "Enter Theater").

### Consumer supplies
Error state and redirect URL.
