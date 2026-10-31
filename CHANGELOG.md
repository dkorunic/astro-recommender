# Changelog

All notable changes to astro-recommender, newest first. Each entry covers the
commits since the previous tag. Version numbers skip where no tag was cut
(v0.6–v0.7, v0.10–v0.12, v0.15–v0.17). See [MODELS.md](MODELS.md) for the
formulas behind the model changes.

## [v0.24.3] — 2026-10-30

### Performance
- Framed scoring is 27% faster (`BenchmarkPipelineCamera` 120 → 88 ms) and
  moonlit scoring 4% faster. Field-rotation checks return early when the trail
  is within the zenith's star width. Each `-filter` choice's bands are now
  worked out once per run instead of once per target. The filtered tallies
  share one magnitude conversion per minute.
- New `BenchmarkPipelineCamera` (framed, alt-az, all eight filters).
  `default.pgo` is now regenerated from all three benchmarks.
- Terminal view: the altitude chart is redrawn only when the curve, the size
  or the theme changes. It had been 40% of a frame's allocations.
- Terminal view: every night in a session shares one SQM, 7Timer and
  grid-elevation lookup (`fetch.Memo`). Stepping through nights no longer
  re-asks DarkSkySites, whose quota counts each request.

### Fixed
- The test suite now passes under `GOOS=js` in Node (`task test-wasm`).
  About 25 tests in six packages had assumed the native build, and the catalog
  tests panicked.

### Dependencies
- bubbletea v2.1.1, ntcharts v2.9.0 and indirect modules.

## [v0.24.2] — 2026-10-29

### Added
- DSS2 pictures fall back to NASA SkyView (DSS2 Red) when CDS hips2fits is
  down, in both the terminal view and the web page. Both services are probed
  at start. A failed service is marked down for the session, so a dead one
  costs one wait, not one per target. When both are down, they are probed again
  at most once a minute.

### Fixed
- A `-focal` within 1% of the scope's own counts as the scope's own optics. A
  crop at the scope's own focal length keeps its label and SUBS.
- `-type cluster` with `-filter` now shows the filtered Moon limit for
  emission clusters.
- Terminal view: the place lookup runs once on the program's context. Before,
  a night cancelled by stepping could leave the next night without a place
  name.
- Nights ahead share today's COBS comet observations instead of each fetching
  them again.
- Moon up/down spans are sampled every 5 minutes and bisected at the
  crossings, so they are exact to the minute.
- The terminal view's empty list names the limits in force, as the text
  report does.

## [v0.24.1] — 2026-10-28

### Fixed
- Terminal view: the site is geocoded once. Holding ←/→ used to query
  Nominatim once per night, which exceeded its one-request-a-second policy.
- Terminal view: ←/→ pressed while `t` (tonight) is still computing now step
  from tonight instead of from the night shown.
- Terminal view: `context canceled` warnings from superseded computes are no
  longer shown or echoed on exit.
- Terminal view: with `-date`, ←/→ step from the date while it computes. The
  help no longer lists ↓ twice.

## [v0.24.0] — 2026-10-27

### Added
- **Interactive terminal view** (Bubble Tea, Lip Gloss, ntcharts). It opens
  when both stdin and stdout are terminals. It has the same layout as the web
  page: Tonight, Weather and Plan on the left, target cards, and the selected
  target's DSS2 picture (Kitty graphics or half-blocks) beside its altitude
  curve and sub-exposure estimate. Keys: ←/→ for the previous or next night,
  `t` for tonight, `s` to sort, `/` to search, `o` to open Aladin, `r` for red
  night vision, `w` for warnings. `-no-tui`, `-json`, `-csv`, a pipe or
  `TERM=dumb` keep the text tables.
- New `internal/view` package. It builds one document that both the web page
  and the terminal view render.
- **Web page redesigned** after Telescopius' deep-sky browser. It has an
  Observatory panel, a Tonight panel (the Moon, a dusk-to-dawn bar and
  forecast hours) and the options as tabs. Each target is a card with its DSS2
  picture, the table's values, a SCORE badge and an altitude curve. The text
  report moved into a collapsible "Full report".
- **Built-in scope filters**: on the Seestars, DWARF 3/mini and Draco, the
  dual-band filter is `-filter`'s default, with each band at the maker's width
  per line. The header and JSON (`sky.filter.bands`) show the bands.
- **Core framings**: an object too large for any frame is framed at its usual
  core when the core fits (11 objects, after NightAstro's framings). The
  target keeps its whole size and brightness.
- `scripts/demo.sh` renders `demo.jpg` from a headless terminal-view frame
  (`ASTRO_RECOMMENDER_SNAPSHOT`), with the picture pasted at full resolution.

### Fixed
- Field rotation: a target framed at its core trails at the core's edge.
- `-scope` with a different `-focal` counts as other optics (SUBS needs
  `-aperture`). A different `-pixel`, `-scale` or larger `-fov` counts as
  another camera (needs `-qe` and `-read-noise`). The Frame line says which.
  With another camera, the scope chip's dark current, response and label no
  longer apply.
- Built-in filter widths apply only when `-filter`'s line filters are the
  scope's own. Overlapping bands of unequal width now meet within their
  overlap.
- COBS: an unnumbered comet fragment is keyed as MPC packs it. An answer
  without a page count, or with more than 10 pages, is an error rather than a
  partial month.
- `-tz Local` no longer throws a RangeError in the web page.
- The Moon limit on the page and in the terminal view includes the header's
  filter and eclipse floors. The Moon's hours span dusk to dawn, not just the
  `-from`/`-to` window.
- `LoosestMoonK` considers only the types `-type` keeps. A target at the
  zenith counts as trailed instead of returning NaN.
- On a polar night, a `-to` at or before the window's start means the next
  day.
- `galaxyv.py` also replaces a galaxy V more than 1.5 mag brighter than its B
  (68 entries). `imm2yaml.py` reads a self-closed empty spreadsheet row as
  empty; it used to take the next row's cells and drop that object.
- Messier: fixed mis-encoded apostrophes (Bode's, Ptolemy's, de Mairan's).
- Web: clear the Go runtime's leftover timers after each run. They threw "Go
  program has already exited" after every networked run.

## [v0.23.0] — 2026-10-26

### Added
- **Lunar eclipses**: the Earth's shadow is computed per minute (a port of
  Astronomy Engine's geometry, within 2 minutes of its contacts for seven
  eclipses in 2025–2028). Moonlight and the Moon-distance limit are dimmed by
  the share of light left, so a totality scores as a dark night. The Moon line
  and JSON `moon.eclipse` list the penumbral, umbral and total stretches.
- **Bright-star warning (STAR column)**: the brightest star to V 4.0 (517 from
  the Yale Bright Star Catalogue) or planet (Mercury to Saturn, from JPL
  Keplerian elements) that can fall in the frame at any angle. It warns of
  halos and reflections; JSON has `brightStar`.
- **Field rotation on alt-az mounts**: `-mount altaz|derotated|eq`, by default
  the scope preset's. On alt-az mounts, minutes whose sub trails the object's
  edge by more than a star's width are not observable. Only large objects high
  in the sky lose time. The Frame line and JSON `frame.mount` name the mount.

### Changed
- **Moon ephemeris** is now Astronomy Engine's series on TT with Espenak &
  Meeus ΔT: within 18″ of SOFA's Moon98, where the old formula was up to 0.4°
  off.
- Moonlight uses each minute's own phase angle and distance, so the
  opposition surge fades within a night near full Moon.
- The narrow-table drop order is now STAR first, then UP after CON; with
  framing, SIZE moves next to SB.

### Fixed
- ΔT now covers every Espenak & Meeus range. The 1986–2005 polynomial alone
  read −919 s for 1960.
- An eclipse minute's light can no longer be NaN from rounding at tangency.
- Field rotation no longer checks a sizeless object at the frame's corner.
  Without `-aperture`, it uses the pixel blur only, not the Origin's
  diffraction.
- `-mount altaz` without `-scope` requires `-sub`.
- GaryImm: Arp 316, M 86 (Markarian's Chain) and NGC 7331 (Deer Lick Group)
  are sized and typed as their groups, without a member galaxy's surface
  brightness.
- The forecast table prints only when some source returned hours.
- `GetText` retries a 429 or 5xx once, as `GetJSON` does. This covers MPC
  elements, COBS, NOAA and the browser's lists. `Prune` also removes leftover
  temp files.
- The Draco's sub length is an assumed 60 s.

## [v0.22.0] — 2026-10-25

### Added
- **Washout detection**: when no 60 consecutive minutes (or the whole of a
  shorter window) reach weather quality 0.2 with seeing under 4″, the text
  report says so instead of printing the target table and plan. JSON has
  `sky.washout` and `sky.washoutReason`.
- The forecast table and JSON include the hour the window's last minutes read
  their cloud from.

## [v0.21.0] — 2026-10-24

### Changed
- **Emission lines are counted in photons**, not energy. The camera counts
  photons, and Hα had been charged 0.76 of an [O III] line's photons. The
  continuum offset `vContinuum` was refit (0.94 → 1.16 mag). Unfiltered and
  broadband sub counts rise about 14%.

### Fixed
- A `haflux` in a `-targets` file is bounded like `surfbr`. A typo could make
  an object immune to sky glow.
- Old COBS cache files are pruned after seven days (`fetch.Prune`). About
  2.5 MB per night had accumulated without limit.

## [v0.20.8] — 2026-10-20

### Added
- **Hα map fluxes**: emission nebulae of 30′ or more without a surface
  brightness now get one from Finkbeiner's full-sky Hα map
  (`scripts/hamap.py`), including the Heart, Soul, California, Rosette and
  Veil. GaryImm's coverage rose from 47% to 72%. These fluxes are used for
  the score only; SUBS keeps the class's typical value.

### Fixed
- 213 (later 281) galaxies carried aperture V magnitudes fainter than their
  own B. These are replaced with B (`scripts/galaxyv.py`, `TestGalaxyV`).
- `haflux.py` matches planetaries by name within 5′ and takes the declination
  sign of Frew's 00° rows from the PN G latitude. This adds 23 fluxes; NGC 2346
  and NGC 6741 had been placed north of the equator.
- The Hα map leaves a nebula unknown when the surrounding ring sits on the
  same complex.
- Positions corrected after a cross-check against eSky: NGC 6873, NGC 752,
  NGC 2506, NGC 6523 and M 24.

## [v0.20.7] — 2026-10-20

### Added
- 23 more camera presets (45 in all): IMX715, IMX366 (ASI4400MC Pro), IMX428,
  IMX290, IMX249, IMX178 mono, IMX568, IMX530, IMX250, MN34230, AR0130,
  SC2210, GSENSE4040 (front- and back-illuminated), IMX252, IMX287, and the
  KAF-16200, KAF-16803, ICX674/694/695/814/825/829 CCDs.
- **Per-chip mono response**, digitized from the makers' charts.
- `qe_digitize.py` gains `--mono`, `--xpx`, `--light-grid`, `--black-max`,
  `--grid-max`, `--sat-min` and `--per-watt`.

### Fixed
- **Sony's relative-response charts are per watt**: dividing the curve by
  wavelength gives the QE. Read as charted, they overstated Hα against [O III]
  by about 1.31×. 25 responses were redigitized.
- Origin read noise is measured from bias masters: Mark II 0.97 e⁻, original
  Origin 1.5 e⁻.

## [v0.20.6] — 2026-10-19

### Changed
- Origin read noise is 1.1 e⁻, measured from its bias master; it was the
  vendor sheet's 2.15 e⁻. The worked examples are recomputed.

## [v0.20.5] — 2026-10-19

### Changed
- Regenerated GaryImmFull and the cross-identified lists from Gary Imm's
  Compendium rev6b. It fixes most of the known position typos and alt-IDs.

### Fixed
- With `-filter`, the Moon line names the filter's loosened Moon limit.
- COBS pages are cached as one document. Newest-first pages fetched hours
  apart had repeated or skipped records.
- Gusts are capped at three times their own hour's mean wind, never below
  20 km/h. The table marks capped gusts with `~`.

## [v0.20.4] — 2026-10-18

### Added
- Vaonis Atlas scope preset.

### Fixed
- JSON `ra`/`dec` are J2000 for comets too.
- `-mono` with `-qe` and `-read-noise` gives SUBS without `-camera`. Without a
  known chip, dark current is the typical value, never the Origin's.
- `-alt-min` must be 0 or more; below the horizon, airmass clamps.
- The empty-list message names the Moon limit, `-horizon` and `-skip`.

## [v0.20.3] — 2026-10-16

### Added
- **More complete sub-exposure model**:
  - dark current per chip, doubling every 5 °C (`-dark`, `-sensor-temp`)
  - optics throughput (`-throughput`)
  - rejected subs (`-reject`)
  - stacking mode (`-stack mean|weighted`)
  - a residual-gradient ceiling (`-residual`; SUBS shows `never` past it)
  - seeing, diffraction and pixel blur of small objects
- **Comet magnitudes from COBS**: the last 30 days of observations shift the
  MPC magnitude (10P: 14.7 → 10.5; observed 10.2–11.8).
- **Planetary nebula Hα fluxes** (Frew et al. 2013) for 1499 planetaries,
  giving a line surface brightness that a central star's magnitude cannot
  fake.
- `scripts/crosscheck.py`: compares the lists against each other and SIMBAD.
- OS08B10 preset (Seestar S50 Pro).

### Changed
- **Models checked against real data**:
  - SUBS dims the object by the whole air column.
  - The H II [N II] and [S II] lines are doublets.
  - Moonlight is rescaled to each minute's albedo.
  - Upper-air seeing is used first, 7Timer only without it.
  - CAMS aerosols are thinned for the site's height above the model terrain
    (La Palma 0.37 → 0.16).
  - 7Timer transparency applies only without CAMS.
  - Nebulae lit by a star or cluster derive no surface brightness from its
    magnitude.
- **Clusters no longer get a sky-glow discount**: they had led every night.
- **Bortle classes follow DarkSkySites' map bands**: Bortle 6 is now 19.95,
  was 18.8.
- Cloud, temperature, dew point, wind and seeing are read from the nearest
  hour.

### Fixed
- `-focal` with `-sensor` alone ignored `-focal` in PX and SUBS.
- The first night of a polar night now carries on into the next day's night.
- With `-plan`, SUBS counts only the target's minutes within its blocks.
- Without `-scope`, SUBS needs `-aperture` and `-camera` instead of silently
  using the Origin's optics and sensor.
- Comet fragments (73P-B…) no longer take their parent's observations. Every
  COBS page is read.
- A fixed `-extinction` below ozone's 0.029 no longer dims filter bands more
  than unfiltered light.
- FILTER shows `none` when no filter beats going without.

## [v0.20.2] — 2026-10-15

### Removed
- `-origin`; use `-scope origin`. The web page reads old links and saved
  forms as `scope=origin`.

## [v0.20.1] — 2026-10-15

### Changed
- Camera presets verified against ZWO and Testar: IMX455 read noise is
  1.35 e⁻; IMX678 mono (ASI678MM) added.

## [v0.20.0] — 2026-10-15

### Added
- **`-camera`**: presets for 21 sensors (pixel size, resolution, QE, read
  noise and the colour channels' response to each line). `-mono` selects the
  mono version.
- **`-scope`**: 20 smart-telescope presets (Celestron Origin and Origin
  Mark II, ZWO Seestar S50/S30/S30 Pro/S50 Pro, DWARF II/3/mini/Draco, Vaonis
  Stellina/Vespera/Vespera II/Pro/3/Pro 2, Unistellar eVscope/eVscope 2/
  Odyssey and Sky-Watcher ORBIS 60EDX). Each sets the frame, optics, sub
  length and sensor.
- **`-filter-std`**: standard line filters in a fast beam lose part of the
  line to the blue shift. Fitted to Chroma's curves.
- **Solar-cycle airglow**: the 10.7 cm flux from NOAA SWPC scales the
  airglow (`-no-solar` turns it off). Shown on the Sky line and in JSON.
- The [N II] doublet as its own line; a band passes every line it reaches.

### Changed
- The Mark II's obstruction is 77 mm and its subs 10 s, so the Origin's SUBS
  change.
- SUBS past tonight's limit shows the number of nights like tonight it would
  take. The header gains a Filter line.
- Web: filter chips, a smart-telescope select and thumbnail loading/failed
  states.

### Fixed
- `-focal` with `-pixel` alone takes the base scope's sensor, so the field
  matches the scale.
- `-mono` is an error with `-scope`, whose cameras are colour.
- Web sky links and thumbnails use J2000 positions.

## [v0.19.1] — 2026-10-15

### Changed
- Tables have titles and fit the terminal. `-h` is grouped by topic. The web
  page has one hover card for pointer, keyboard and touch.

## [v0.19.0] — 2026-10-14

### Added
- **Sub-exposure estimate (SUBS)**: the number of subs for stacked SNR 5, 10
  and 20 per arcsec², from the modelled sky, the camera flags and Origin
  defaults. Shown in the table, JSON and the web hover card. Each colour
  channel is counted by SNR².
- **MAG column**: integrated V magnitude, else B (marked `B`).
- The target table fits the terminal width, cutting descriptions and dropping
  columns in a fixed order.

### Changed
- **The filter choice is by gain**: each choice is scored as worn all night.
  With a camera, FILTER is the choice that needs the fewest subs. Before, Hα
  won almost everything once the scores saturated under a dark sky.
- Line-filter bands are shares of the camera's 400–700 nm, not V's 88 nm. The
  old basis charged line filters 3.4× the sky per nm.
- Emission-line light is calibrated against Finkbeiner's Hα map
  (`scripts/calibrate_lines.py`).
- The Moon-limit factor for narrowband filters has a floor of 0.3.
- CON shows the IAU abbreviation. PX is hidden without framing.
- PNnet sizes and minor axes come from the authors' CSV export.

### Fixed
- GaryImm's M 27, 57, 76 and 97 had their central stars' magnitudes, about
  7 mag too faint. They now have the nebula's integrated V.
- Molecular clouds and dark nebulae get no exposure estimate.
- A comet brighter than magnitude 0 lost its magnitude.
- Packed gob string lengths must cover the blob exactly, so a version skew is
  a decode error rather than a panic.
- Comets: the declination's `Asin` is clamped, so rounding at the pole cannot
  drop a night.
- Weather: sentinel surface values are treated as null.

## [v0.18.2] — 2026-10-13

### Changed
- The web build embeds only the default list. Other lists are fetched as
  `lists/<Name>.gob.gz` on demand (app.wasm 23.0 → 19.3 MB). The wasm is
  compiled while it downloads.

## [v0.18.1] — 2026-10-13

### Added
- Seven catalogue lists (34 in all, about 44,000 entries): MCRN, Henize, DEM,
  Harris (globular clusters), Dias (open clusters), DCld and AM.

### Fixed
- A catalogue audit against SIMBAD and the source catalogues:
  - LDN sizes were Lynds' area × 60. They are now the diameter of a disc of
    that area (LDN 621: 2700′ → 454′).
  - LBN types follow Lynds' colour index.
  - OpenNGC's coarse nebula types take the Compendium's call.
  - Central-star magnitudes on planetaries are replaced (M 57: 15.8 → 8.8).
  - Compendium and Herschel400 coordinate typos are fixed.

## [v0.18.0] — 2026-10-13

### Added
- **`-csv`**: Telescopius observing-list format, which N.I.N.A.'s simple
  sequencer imports.
- **`-filter` accepts a list** of the filters you own. A FILTER column names
  the best one per target.
- **Tri-band, quad-band and broadband filters**: `tri`, `quad`, `cls`
  (Astronomik CLS) and `lpro` (Optolong L-Pro).
- **Zodiacal light** (Leinert et al. 1998) and **Milky Way background light**
  (Pioneer 10) per target direction.
- **Winkler (2022) scattered moonlight**, replacing Krisciunas & Schaefer's.
- Each filter band has its own extinction (Rayleigh λ⁻⁴, aerosol λ^−1.3).
- Moon rise and set use the almanac definition (upper limb, −50′).
- Lists: Cederblad, Gum, cometary globules (CG) and Abell clusters (ACO).
- Web: altitude curves under the thumbnail with shaded observable stretches,
  a forecast ribbon and a peak marker.
- Fuzz tests per package, plus gosentry/LibAFL tasks (`task fuzz-*`).
- `terrain2horizon.py` prints a horizon panorama.

### Changed
- Allocations per scoring run fell by 99.9% (type classification is cached,
  and gob strings are packed in one blob).
- SOFA comparisons are extended (ecliptic, azimuth, Moon distance and phase,
  dusk/dawn), and SOFA's reference results are pinned.

### Fixed
- Zodiacal and galactic light was brightened towards the horizon like
  airglow. It is now only dimmed by extinction.
- Sexagesimal accepts plain decimals only. `-fov` accepts `2X1`.
- CSV cells starting with `= + - @` are escaped against spreadsheet formula
  injection.
- JSON plan blocks no longer drop a 0° peak altitude.
- `-filter ,` is an error. `-filter-nm` requires a line filter.
- `terrain2horizon.py` no longer caches an Overpass response that reports a
  runtime error.

## [v0.14.0] — 2026-10-12

### Added
- **Per-band nebula filters** derived from the sky's spectrum: `-filter
  dual|ha|oiii|sii` and `-filter-nm`, replacing `-filter-k`.
- **`-type`**: galaxy, cluster, nebula, emission, comet or other.
- Lists: RCW, SNR (Green's optically detected remnants), Barnard, vdB, vdBH,
  Arp and HCG.
- `scripts/terrain2horizon.py`: builds a `-horizon` file from AWS terrain,
  OSM buildings and the Meta/WRI canopy height map.
- Web: red night-vision theme.
- MODELS.md, documenting every formula and model.

### Fixed
- Polar night: a polar night in progress carries on past noon
  (`ContinuePolar`), and the end of polar night is no longer lost.
- A stray non-flag argument (e.g. `-rotate true`) is an error. It used to
  silently drop every flag after it.
- Unknown keys in a `-targets` file produce a warning (YAML merge keys
  excepted).
- `emissionNames` match ignoring case and spaces (`M16` = `M 16`).
- A `Retry-After` beyond int range is no longer retried at once.
- Spans of a day or more are shown with `+Nd`.
- The Pages build fails on a stale embedded list.

## [v0.13.0] — 2026-10-10

### Added
- **Seeing estimate** from a Dewan Cn² integral over the ECMWF IFS upper-air
  profile. It is shown as FWHM in arcseconds and calibrated against the ESO
  DIMM archives at Paranal and La Silla (RMSE 0.49″ → 0.33″).
- **`-mosaic N` and `-rotate`**, with minor axes for OpenNGC/OpenIC, Messier,
  GaryImm, Herschel400, Pensack500, LBN and Caldwell.
- Caldwell list.
- Web: Aladin Lite sky links and DSS2 hover thumbnails (from the `alaskybis`
  mirror), Cloudflare Web Analytics, a logo and version footer.

### Changed
- The DESCRIPTION column is capped at 40 characters; JSON keeps the full
  text.

### Fixed
- Seeing estimate hardening: plausible bounds, a 300 m ground-layer minimum,
  hypsometric and barometric level checks and a WMO tropopause.
- `-mosaic 1` and `-rotate=false` are accepted without framing.

## [v0.9.1] — 2026-10-09

### Changed
- The web page moved to astrorecommender.org, proxied through
  proxy.astrorecommender.org.

## [v0.9.0] — 2026-10-09

### Added
- Rain gate: precipitation takes the sky factor to 0.
- A first upper-air seeing class.
- Mutation tests in every package (about 400 mutants), and tighter SOFA
  limits.
- Web: Copy link button.

### Fixed
- Precession clamps the declination's sine; a target at the pole made the
  score NaN.
- `-from`/`-to` on DST nights: a skipped clock is an error, and a repeated one
  is read as the widest window.
- `-targets` files: empty or comment-only files load as zero targets, every
  YAML document is read, and a second list is an error.
- Upper-air levels below the site or with non-physical values are dropped. An
  AOD above 10 counts as missing.
- Near the pole, `Window` skips the previous night's tail at a dark noon.
- Emission clusters (Rosette, Lagoon, Soul, IC 410, NGC 6823) are scored as
  nebulae.
- `-version` reports `dirty` once.
- Web: number fields no longer use iOS's decimal keypad, which has no minus
  sign.

## [v0.8.0] — 2026-10-08

### Added
- **Browser build**: the unchanged program compiled to WebAssembly and
  published on GitHub Pages. 7Timer and DarkSkySites go through a Cloudflare
  Worker for CORS. Shareable URLs and a theme switch.
- **Observable runs**: the OBSERVABLE column shows the longest stretch, and
  `-min-run` drops targets without one that long.
- `-twilight nautical`, `-skip FILE`, `-json`, and Moon rise/set in the
  header.
- Sharpless list. PNnet now embeds the live Planetary Nebulae.net catalogue
  with the authors' permission.
- Homebrew formula and cask (`dkorunic/tap`).

### Changed
- Lists are embedded as gob of the parsed result. OpenNGC loads in 2 ms
  instead of 87 ms (in wasm, 9 ms instead of 631 ms).
- Moonlight and sidereal-time sines are computed once per minute (−13%). The
  Moon separation is compared on its cosine (−39% on moonlit nights).
- YAML library: go.yaml.in/yaml/v4.

### Fixed
- `fetch` honours `Retry-After` and retries a 429 or 5xx once.
- Web: Reset button; `-date` is no longer remembered between visits.

## [v0.5.0] — 2026-10-07

### Added
- **Surface brightness against the sky**: an object's SNR relative to a
  pristine sky, so bright-surface objects shrug off moonlight. Adds `mag`,
  `bmag`, `surfbr` and `bsurfbr` to the lists and an SB column.

### Fixed
- Object types are classified once (`kind()`). Clusters with nebulosity no
  longer derive a surface brightness from their stars' magnitude; NGC 2362
  had ranked above every galaxy under a bright Moon.

## [v0.4.0] — 2026-10-06

### Added
- `-ra`, `-dec` and `-tol` sky-region filter.
- RA and DEC columns.
- `task demo` to re-render `demo.jpg`.

### Fixed
- The region header formats the centre and tolerance exactly. `-ra 24` means
  0h.
- `Target.Position` no longer panics on an empty track.

## [v0.3.0] — 2026-10-06

### Added
- `-focal`, `-sensor` and `-pixel`. The Origin preset is derived from its
  IMX678.
- Moon distance and opposition surge in the moonlight.
- Target lists: MWSC, Melotte, Collinder, PNnet and HASH.
- PGO profile (`task pgo`).

### Changed
- **The Moon is topocentric** for the separation rule (up to 1° of
  parallax).
- **Scoring is 2.3× faster** (hoisted alt-az trig; `Exp` instead of `Pow`).
- Network sources are fetched in parallel.

### Fixed
- Open-Meteo gusts counted against the wrong hour.
- Partial forecasts are recovered, and uncovered hours get the covered
  hours' mean, not a clear sky.
- Runs after dusk plan only the rest of the night. A dusk after midnight no
  longer skips to the next night.
- Zones across the date line (Samoa, Tonga, Kiritimati…) planned the next
  night.
- A `track` key in a `-targets` file could panic scoring.
- Oversized downloads are rejected rather than truncated and cached.
- Horizon: points at 0° and 360°, and vertical steps, are handled correctly.
- Emission types are matched across every list's spellings.
- Windows console colours. DST-crossing clock times carry the zone.
- Comets: a time.Duration overflow, NaN tracks, and invalid perihelion dates.
- Security: redirects are followed only on the same host over HTTPS (the API
  key is not leaked). JSON responses are capped at 1 MiB.

## [v0.2.0] — 2026-10-05

### Added
- Automatic zenith sky brightness from DarkSkySites when
  `DARKSKYSITES_API_KEY` is set.

## [v0.1.0] — 2026-10-05

Initial release: a Go port of the deep-sky part of
[uptonight](https://github.com/mawinkler/uptonight). It ranks objects from 9
embedded lists (GaryImm, GaryImmFull, Messier, Herschel400, Pensack500, LBN,
LDN, OpenNGC, OpenIC) for tonight's astronomical night. Features:
- altitude, Moon and size limits
- Krisciunas & Schaefer sky brightness from Bortle or SQM
- extinction
- Open-Meteo and 7Timer weather and CAMS aerosols
- MPC comets
- a local horizon file
- Origin framing (`-origin`, `-fov`, `-scale`)
- a nebula-filter heuristic
- a greedy night plan (`-plan`)
- an offline time-zone lookup and reverse geocoding

[v0.24.3]: https://github.com/dkorunic/astro-recommender/compare/v0.24.2...v0.24.3
[v0.24.2]: https://github.com/dkorunic/astro-recommender/compare/v0.24.1...v0.24.2
[v0.24.1]: https://github.com/dkorunic/astro-recommender/compare/v0.24.0...v0.24.1
[v0.24.0]: https://github.com/dkorunic/astro-recommender/compare/v0.23.0...v0.24.0
[v0.23.0]: https://github.com/dkorunic/astro-recommender/compare/v0.22.0...v0.23.0
[v0.22.0]: https://github.com/dkorunic/astro-recommender/compare/v0.21.0...v0.22.0
[v0.21.0]: https://github.com/dkorunic/astro-recommender/compare/v0.20.8...v0.21.0
[v0.20.8]: https://github.com/dkorunic/astro-recommender/compare/v0.20.7...v0.20.8
[v0.20.7]: https://github.com/dkorunic/astro-recommender/compare/v0.20.6...v0.20.7
[v0.20.6]: https://github.com/dkorunic/astro-recommender/compare/v0.20.5...v0.20.6
[v0.20.5]: https://github.com/dkorunic/astro-recommender/compare/v0.20.4...v0.20.5
[v0.20.4]: https://github.com/dkorunic/astro-recommender/compare/v0.20.3...v0.20.4
[v0.20.3]: https://github.com/dkorunic/astro-recommender/compare/v0.20.2...v0.20.3
[v0.20.2]: https://github.com/dkorunic/astro-recommender/compare/v0.20.1...v0.20.2
[v0.20.1]: https://github.com/dkorunic/astro-recommender/compare/v0.20.0...v0.20.1
[v0.20.0]: https://github.com/dkorunic/astro-recommender/compare/v0.19.1...v0.20.0
[v0.19.1]: https://github.com/dkorunic/astro-recommender/compare/v0.19.0...v0.19.1
[v0.19.0]: https://github.com/dkorunic/astro-recommender/compare/v0.18.2...v0.19.0
[v0.18.2]: https://github.com/dkorunic/astro-recommender/compare/v0.18.1...v0.18.2
[v0.18.1]: https://github.com/dkorunic/astro-recommender/compare/v0.18.0...v0.18.1
[v0.18.0]: https://github.com/dkorunic/astro-recommender/compare/v0.14.0...v0.18.0
[v0.14.0]: https://github.com/dkorunic/astro-recommender/compare/v0.13.0...v0.14.0
[v0.13.0]: https://github.com/dkorunic/astro-recommender/compare/v0.9.1...v0.13.0
[v0.9.1]: https://github.com/dkorunic/astro-recommender/compare/v0.9.0...v0.9.1
[v0.9.0]: https://github.com/dkorunic/astro-recommender/compare/v0.8.0...v0.9.0
[v0.8.0]: https://github.com/dkorunic/astro-recommender/compare/v0.5.0...v0.8.0
[v0.5.0]: https://github.com/dkorunic/astro-recommender/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/dkorunic/astro-recommender/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/dkorunic/astro-recommender/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/dkorunic/astro-recommender/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/dkorunic/astro-recommender/releases/tag/v0.1.0
