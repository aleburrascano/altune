# Design choice: Altune iOS widget

## Picked: a-cover (a-cover/), mixed with: nothing (medium uses the full-bleed variant, medium-alt2 → medium.html)

## Why: "option A looks so much better." Artwork-led and calm. User wanted every control (shuffle/repeat added), no playlists on medium, and medium "more creatively done" → picked the full-bleed cover medium over the two-pane card and the progress-ring variant ("start with bottom").

## Design
- Faces: Plus Jakarta Sans 700/600 (display: titles), Inter 400/500/600 (body: artist, source, labels).
- Sizes: title ~16pt, artist/source ~12-13pt, times ~11pt (widget point sizes).
- Palette (roles): cover art is the color source on home widgets; canvas `#121214`, surface1 `#1C1C20`, surface2 `#232328`, border `#2A2A30`; text primary `#F4F4F6`, secondary `#A6A6AE`, tertiary `#8C8C96`; accent `#2D5BFF`, accent-text/active-toggle `#6B8FFF`, on-accent `#FFFFFF`. Play/pause is a white disc. Shuffle/repeat glyphs are dim white (inactive) / `#6B8FFF` (active). No solid blue fill on home widgets.
- Accessory surfaces (lock screen, Control Center): iOS monochrome vibrancy — white/light glyphs and text, no cover art, no `#2D5BFF` fill. Config sheet uses the system edit-sheet look with system-blue checkmarks.
- Radius: 8/12/16/24; artwork corners 12; iOS fixes the widget's own corner (~22).
- Density: calm (clutter 0.4-1.4% across all shots).
- Layout primitive: the album cover is the surface; controls ride on it (medium/small) or on the tint (large).
- Signature moment: on medium the real cover fills the whole widget with a scrim; title/artist/source top-left, a slim progress bar, and the 5-control row along the bottom.

## Screens and states
- `small.html` → systemSmall, cover-fill background + bottom scrim, title/artist + prev/play/next. Also the iOS 26 CarPlay dashboard tile.
- `small--carplay.html` → the small widget on the CarPlay dashboard beside a dimmed neighbour tile.
- `small--empty.html` → nothing played yet: "Open Altune to start a playlist" + Open pill.
- `medium.html` → PICKED full-bleed cover: title/artist/source over the art, slim progress bar, 5 controls (shuffle/prev/play-pause/next/repeat) along the bottom. (`medium-alt1.html` progress-ring variant kept for reference, not picked.)
- `medium--empty.html` → signed out: "Sign in to Altune" + Sign in pill.
- `large.html` → cover band, title/artist, progress bar, 5 controls, Up Next (2 tracks, tap to jump), 4 recent-playlist tiles.
- `large--empty.html` → signed out, scaled up.
- `lock-rectangular.html` → lock-screen accessoryRectangular: mono mini-art, title/artist, pause glyph.
- `lock-circular.html` → lock-screen accessoryCircular: vibrancy ring, play/pause.
- `lock-inline.html` → lock-screen accessoryInline: "▶ Midnight City — M83" above the clock.
- `control.html` → iOS 18 Control Center / Action Button controls: "Shuffle Library" and "Play / Pause Altune".
- `config.html` → long-press Edit sheet: Playlists = "4 most recently played" (default) or "Choose playlists…".

## Changes asked for: medium uses the full-bleed variant (done, now medium.html). Recent playlists = 4 most recently played, tracked on-device (no server change).

## Rejected
- b-remote: user liked its full control set but "A cover looks so much better" — controls were folded into a-cover instead.
- c-shelf: playlists-led; not picked.
- medium two-pane card and medium-alt1 progress-ring: the full-bleed medium was picked over both.

## Content needed
- Real album/playlist cover images (the mockups use abstract stand-in art). No blocker: real covers come from the app at runtime.
- Final wording for the signed-out sub-lines (minor).

## MVP note for /plan
The slice to build and test first is the home-screen now-playing widget — **small + medium (full-bleed)** — wired to REAL Altune playback: the app↔widget data bridge (App Group, resolved for SideStore re-signing), a Swift AudioPlaybackIntent per control calling the existing player, live now-playing snapshot, and cover art saved to the shared container. Everything else is backlog: large + Up Next, the 3 lock-screen widgets, the Control Center / Action Button control, the configurable-playlists sheet, and on-device recent-playlist tracking.
