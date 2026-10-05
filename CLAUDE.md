# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

See README.md for user-facing behaviour and flags.


Single-binary Go CLI that ranks deep sky objects for a location for tonight's window: astronomical dusk to astronomical dawn (Sun < −18°, searched noon to noon). A port of the deep sky part of [uptonight](https://github.com/mawinkler/uptonight), extended for the Celestron Origin telescope (frame fit, moon/filter weighting, cloud forecast).

## Commands

```sh
task build                          # fmt (gci, gofumpt, go fix, betteralign) + static PGO binary
task lint                           # fmt + golangci-lint (.golangci.yml: default all, some disabled); keep at 0 issues
task fmt                            # NOTE: rewrites files in place
go build ./...
go vet ./...
go test ./...                       # unit tests beside each package; TestSOFA compares the formulas with SOFA
go test -run TestSOFA -v ./internal/astro   # prints the worst difference per quantity
go run . -lat <deg> -lon <deg> [-tz Europe/Zagreb] [-date YYYY-MM-DD] [-origin] [-filter] [-plan 2h] [-no-weather] [-n 20]
```

`go run . -h` lists all flags. Use `-no-weather -no-geocode -no-sqm -no-comets` for runs that must not hit the network or be deterministic. `CLICOLOR_FORCE=1` forces colors when stdout is not a TTY (e.g. to inspect escapes); `NO_COLOR` disables them.

## Architecture

`main.go` only wires the pipeline; all code lives in packages under `internal/` (one per component):

| Package | Contents | Imports (internal) |
|---|---|---|
| `astro` | Sun/Moon/sidereal time/alt-az, `Precess`, `Window` (searches from the site's solar noon), `Tonight` (the night in progress after midnight), `ClipWindow`, `EarthHelio` | num |
| `atmos` | `Airmass`, `Extinction(Coeff)`, `SkyBrightness` (K&S), `BortleMag`/`BortleClass` | — |
| `sanitize` | `Text`: strip control chars/invalid UTF-8 from untrusted text | — |
| `num` | `Finite`: NaN/Inf check that every parser of untrusted numbers runs before its range checks | — |
| `fetch` | `GetJSON`/`GetJSONHeader` (size-capped; non-200 → `*StatusError` (matches `ErrStatus`, carries the code) with the body still decoded), `GetText`, `Cached` (user cache dir, unique temp file + rename) | — |
| `geotz` | `Lookup`: offline IANA zone from coordinates (tzf, ~150 ms, ~11 MB of embedded data) | — |
| `geocode` | `Reverse` (Nominatim) | fetch, sanitize |
| `sqm` | `Lookup` (DarkSkySites zenith SQM; key from `DARKSKYSITES_API_KEY`) | atmos, fetch, num, sanitize |
| `weather` | Open-Meteo `Forecast`/`AerosolForecast`, 7Timer `AstroForecast` | fetch, sanitize |
| `constellation` | `Of`, `FullName` (IAU boundaries from astrogo) | astro |
| `catalog` | `Target`, embedded lists in `internal/catalog/targets/`, `Load`, `EmissionLine` | astro, constellation, num, sanitize |
| `comets` | `Targets` (MPC elements, two-body orbits) | astro, catalog, constellation, fetch, num, sanitize |
| `horizon` | `Horizon`, `Load`, `At` | num |
| `config` | `Config`, `Parse` (flags → validated config, returns errors; `-tz` defaults to `geotz.Lookup`) | atmos, catalog, geotz, horizon, num |
| `scoring` | `Sky`/`BuildSky`, `Result`, `Score`, `skyK`, `frameFill` (scoring), `ramp` | astro, atmos, catalog, config, weather |
| `plan` | `Slot`, `Make` (greedy + local search) | scoring |
| `output` | `Header`/`Weather`/`Plan`/`Results`, ANSI colors | atmos, catalog, config, geocode, plan, scoring, weather |

Only `main` exits (`fatal`); packages return errors. `version.go` holds `GitTag`/`GitCommit`/`GitDirty`/`BuildTime`, which Taskfile.yml and .goreleaser.yml set with `-X`; `-version` falls back to `debug.ReadBuildInfo` when they're empty. Tests sit beside each package. Every Go file starts with `SPDX-FileCopyrightText` and `SPDX-License-Identifier: MIT` headers; keep them on new files (`internal/constellation/constellation.go` also credits Rener Castro/astrogo).

The flow is: `config.Parse` → `catalog.Load` → `astro.Window` (dusk to dawn) → `astro.ClipWindow` (`-from`/`-to`, only ever narrows) → `scoring.BuildSky` (1-minute grid, per-minute Moon position and altitude, sky quality) → optional `comets.Targets` → `scoring.Score` → `output.Header`/`Weather`/`Plan` (`-plan`, via `plan.Make`)/`Results`.

- **Astronomy is hand-rolled on purpose, with no astro library.** It uses Astronomical Almanac low-precision formulas (`astro.SunRADec` ~0.01°, `astro.MoonRADec` ~0.4° geocentric), GMST for altitude, and illumination from elongation. Catalogue J2000 coordinates and comet tracks are moved to the equinox of date with `astro.Precess` (IAU 1976); without that, altitudes were up to 41′ off. `TestSOFA` (`internal/astro/sofa_test.go`) checks them against SOFA through `github.com/hebl/gofa`, a test-only dependency that must not be imported by non-test code; the limits are the README's Accuracy table, keep the two in sync. `astro.MoonTopo` shifts the Moon by its horizontal parallax (up to 1°) to the site, so `Sky.MoonPos` and `Sky.MoonAlt` are topocentric, as in astroplan. Nutation and refraction are ignored, which is fine against the 30° altitude and tens-of-degrees Moon limits.
- **Targets** are uptonight-format YAML. All eight uptonight lists in `internal/catalog/targets/` are embedded through `targetFS`. They're verbatim except `GaryImm.yaml`, where 10 sizes and positions were corrected against Gary Imm's 2026 Compendium. `GaryImmFull.yaml` (all 3145 Compendium objects, opt-in, not the default) is generated by `scripts/imm2yaml.py` from the XLSX. Regenerate it rather than hand-editing; the script fixes known coordinate typos and carries an extra `rating` field that the loader ignores (it used to emit `mag` too; don't bring it back, nothing reads it). Its constellation labels are unreliable, so `TestConstellationLists` skips it. `-list` picks one (GaryImm by default) and `-targets` loads a file. OpenNGC and OpenIC are CC BY-SA 4.0. Unknown sizes are `-9999`. RA/Dec are `"hh mm ss"` / `"[+-]dd mm ss"` strings parsed by `astro.Sexagesimal`, which takes the sign from the text so that `-00 ..` works and rejects empty input; `catalog.Load` also rejects RA outside 0–24h and |Dec| > 90°. `size` is the major axis in arcminutes.
- **Observable minute** (uptonight rules): altitude within `-alt-min`..`-alt-max` (30–80°; the airmass ≤ 2 rule is implied by ≥ 30°), and Moon separation ≥ illumination % in degrees. The separation rule is skipped while the Moon is below the horizon, which uptonight does not do.
- **Ranking:**
  - `FOTO` is the raw observable fraction (uptonight's metric, with the Moon-distance limit scaled by `k`).
  - Each observable minute's `weight` is `Quality × atmos.Extinction(alt) × min(1, √(RefNL / (k × atmos.SkyBrightness)))`, and `SCORE` is the sum of the weights divided by the window length, × `frameFill` (scoring) when framing. Mean altitude breaks ties.
  - `atmos.SkyBrightness` is Krisciunas & Schaefer (1991) in nanoLamberts: the site's zenith brightness (`Config.ZenithMag()`: `-sqm`, else `atmos.BortleMag[-bortle]`, else 22.0) brightened towards the horizon, plus scattered moonlight from the Moon's altitude, its angle to the target, its phase angle (with the opposition surge, ×1.35 at full tapering to none at 7°) and its distance (`astro.MoonDistance`, as `(60.27/d)²`). `extinction` uses Pickering airmass. The per-minute coefficient `Sky.Ext` comes from `Sky.ExtinctionAt`: `atmos.ExtinctionCoeff(elevation, AOD)`, with the AOD from `weather.AerosolForecast` (Open-Meteo Air Quality/CAMS, which also returns the elevation) and `atmos.TypicalAOD` past the forecast range. It falls back to `-extinction` when there's no data, and an explicit `-extinction` skips the fetch.
  - `k = skyK(target)` is the fraction of sky glow that counts against the target: 1 for broadband, 0.5 for clusters, and `-filter-k` for emission-line targets with `-filter`. Emission-line is decided by type plus the `emissionNames` (catalog) overrides.
- **Framing**: `-origin`, `-fov WxH`, `-scale`, or `-focal` (mm) with `-sensor WxH` (mm, replaces `-fov`, via `sensorFOV`) and/or `-pixel` (µm, replaces `-scale`, via `frameScale`) turn on `cfg.Framing`; `frameFOV` enforces the pairings and validates `-focal`. Unset values default to the Origin's (`origin*` constants). The sensor-size check (`maxSensorPx`) runs only when both a field (`-fov`/`-sensor`) and a scale (`-scale`/`-pixel`) were given: checking a lone `-fov` against the Origin's scale rejected valid wide fields (`-fov 10x7`), so a lone `-fov` in arcminutes is deliberately not caught beyond the 180° bound. Framing replaces the size defaults with `-min-px × scale` up to the *short* side (the size is the major axis with unknown position angle, so `frameFill` also divides by `FOVShort`), using `flag.Visit` so explicit `-size-min`/`-size-max` still win; an explicit `-size-max` above the short side is an error, since `scoring` drops any target with `Size >= FOVShort` (explicit check, not via `frameFill`, which only ever sees fills below 1). Unknown sizes (`!Target.HasSize()`: zero or negative, the single place that defines the sentinel) count as 0 for the limits and get `Frame = 1`. Without framing `-size-max` is inclusive; several list objects are exactly 300′. `validate` also enforces `Scale > 0` and `FOVLong >= FOVShort > 0` whenever `Framing` is set, so a hand-built `Config` cannot reach `FramePx` with a zero scale.
- **Weather** (both sources skipped with `-no-weather`; any failure is a stderr warning and scoring falls back to perfect sky):
  - `weather.Forecast` calls the Open-Meteo hourly API (no key, ~3 months back to 2 weeks ahead) and returns data keyed by Unix hour. Effective cloud combines the low, mid and high layers, with high (thin cirrus) counted half. Dew-point spread (4→1 °C) and gusts (20→40 km/h) reduce the per-minute sky factor through `ramp()`. Open-Meteo gusts are the maximum over the *preceding* hour, so `hourly.weather` stores each hour's gust from the next entry; the score and the table both read the hour that starts at the key. Null values are skipped, never treated as clear.
  - `weather.AstroForecast` calls 7Timer ASTRO (3-hourly, ~3 days ahead only; forecast points sit on UTC hours divisible by 3, see `weather.AstroKey`). Its transparency (1–8) scales the sky factor from 1 down to 0.5; seeing is display-only.
  - Coordinates are deliberately rounded to 2 decimals before sending.
- **SQM lookup**: `main` calls `sqm.Lookup` just before `BuildSky` (after the early exits, so they never hit the network) only when `DARKSKYSITES_API_KEY` is set and none of `-sqm`, `-bortle` (`Config.SkySet`, via `flag.Visit`, so an explicit 0 also wins) or `-no-sqm` was given. It sends the `x-api-key` header (`fetch.GetJSONHeader`, which canonicalizes caller header keys), rounds coordinates to 2 decimals, surfaces the response's `error` field whatever the status, rejects values outside `atmos.MinSQM`–`MaxSQM` (also the `-sqm` bounds), and fills `Config.SQM`/`SQMSource` (shown in the header). Failure is a warning; the sky stays dark (22.0). Never hardcode the key.
- **Geocoding**: `geocode.Reverse` uses OpenStreetMap Nominatim (`zoom=14`, suburb level) with the same 2-decimal rounding. Nominatim's policy requires the identifying `User-Agent` and at most 1 request/s. Failure is a warning only.
- **Colors**: `tabwriter` counts ANSI escape bytes as width. Alignment holds only because `paint()` codes are always 2 characters and every cell of a column is painted the same number of times on every row, the header included. Keep that invariant when adding columns: paint plain cells with `"39"`.
- **Horizon** (`-horizon`): `horizon.at(az)` interpolates (azimuth, altitude) points, wrapping across north; an empty horizon returns −90, meaning no limit. Using it needs azimuth, so scoring calls `altAz`, not `altitude`.
- **Plan** (`-plan`): `scoring.Score` keeps per-minute `Alt` and `Weight` slices on each `Result`, so `plan.Make` scores any block by summing that slice instead of recomputing. `greedyPlan` (mean altitude breaks ties) is followed by `improvePlan`, a local search over a target × block score matrix. It swaps blocks, moves a target into an empty block and refills the block it leaves, and swaps in unused targets. It stops when nothing improves, giving a local optimum.
- **Comets** (`internal/comets`): `comets.Targets` downloads MPC `CometEls.txt` through `fetch.Cached` (user cache dir, 1 day, falling back to a stale copy), parses the fixed columns, keeps comets brighter than `-comet-mag` at mid-window (`m = H + 5 log Δ + 2.5 K log r`), and gives each a per-minute `catalog.Target.Track`. `scoring` uses `Track` instead of fixed RA/Dec when it's set; comets skip the size limits and framing. `astro.EarthHelio` subtracts general precession because MPC elements are J2000 while the Sun formula is of date; without that a near comet is ~0.7° off. Positions were verified against JPL Horizons.
- **Constellations**: `constellation.Of(ra, dec)` takes J2000 degrees, precesses to B1875 (`astro.PrecessT`) and ray-casts against the boundary loops (Ursa Minor winds around the pole, so its parity is inverted). `catalog.Load` overwrites every list's constellation field with it, since the lists contain errors and spelling variants; comets use it too. A test bounds disagreements with the lists at 5.
