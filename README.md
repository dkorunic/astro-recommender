# astro-recommender

A small Go CLI that lists the best deep sky objects to image **tonight** from a given location. It looks at the whole of full astronomical night, from dusk to dawn (Sun below −18°).

It works like the deep sky part of [uptonight](https://github.com/mawinkler/uptonight), plus extras aimed at the **Celestron Origin** smart telescope:

- a check that each object fits the Origin's frame and pixel scale (or any telescope's, with `-fov`/`-scale` or the camera's `-focal`, `-sensor` and `-pixel`)
- moonlight and light-pollution handling, with optional narrowband filter support
- cloud, transparency, dew and wind forecasts

![astro-recommender CLI demo: forecast table, top 20 targets and legend](demo.jpg)

## Features

- **Tonight's window**: astronomical dusk to dawn (Sun below −18°), found by searching from the site's own solar noon so time zones and polar nights can't cut it short. A run during the night (after dusk, or after midnight) plans only the rest of it; `-date` plans another evening and `-from`/`-to` narrow the window.
- **Observability per minute**, following uptonight's rules: altitude between `-alt-min` and `-alt-max`, Moon separation of at least the illumination percentage in degrees (skipped while the Moon is below the horizon), and optionally above your measured **local horizon** profile (`-horizon`).
- **Ranking** by an imaging score that sums, over every observable minute, the forecast sky quality × atmospheric extinction × a sky-brightness weight, with uptonight's observable fraction (FOTO) shown alongside. Mean altitude breaks ties.
- **Sky brightness and moonlight**: the Krisciunas & Schaefer model of the moonless sky from `-bortle` or a measured `-sqm` value, brightened towards the horizon, plus scattered moonlight from the Moon's phase, altitude and distance to the target. Targets are weighted by sky-limited signal-to-noise.
- **Target-type sensitivity to sky glow**: clusters count half of it, and with `-filter` emission-line targets (emission and planetary nebulae, supernova remnants, Wolf-Rayet nebulae, plus known mislabelled ones) count only `-filter-k` of it, which also relaxes their Moon-distance limit.
- **Extinction per hour** from the site's elevation and the CAMS aerosol forecast (dust, smoke, haze), or a fixed `-extinction`; shown in the forecast table.
- **Weather**: Open-Meteo cloud cover by layer (thin high cloud counts half), dew-point spread and wind gusts, and 7Timer transparency and seeing, in an hourly table and folded into the score minute by minute. Missing data counts as perfect sky with a warning, never silently.
- **Framing for the Celestron Origin** (`-origin`) or any telescope (`-fov`/`-scale`, or `-focal` with `-sensor`/`-pixel`): keeps only objects that fit the frame's short side and are at least `-min-px` across, scores how well they fill it, and shows their size in pixels.
- **Comets** from the Minor Planet Center's daily elements, cached locally: two-body positions every minute (elliptic, parabolic and hyperbolic orbits), kept when brighter than `-comet-mag`, ranked like any other target.
- **Night plan** (`-plan 2h`): one target per time block, chosen by a greedy pass followed by a local search that swaps and moves targets between blocks.
- **Nine built-in target lists** (about 21,000 objects: Gary Imm's selection and full Compendium, Messier, Herschel 400, Pensack 500, NGC, IC, LBN, LDN) or your own uptonight-format YAML, with every object's constellation computed from the IAU boundaries.
- **Location handling**: offline IANA time-zone lookup from the coordinates, reverse geocoding of the site name, coordinates rounded to about 1 km before any request, and a fully offline mode.
- **Terminal output**: aligned, colour-coded tables with a legend; honours `NO_COLOR`, `CLICOLOR_FORCE` and `TERM=dumb`. Text from the network or from target files is stripped of control characters so it cannot inject terminal escape sequences.
- **Verified astronomy**: the hand-rolled Sun, Moon, sidereal-time, precession and altitude formulas are checked against IAU SOFA in the test suite (see [Accuracy](#accuracy)); a single static binary with no API keys.

## Install

```sh
go install github.com/dkorunic/astro-recommender@latest
```

Or from a checkout, using [Task](https://taskfile.dev/):

```sh
task build   # formats, then builds a static, PGO-optimised binary (profile: default.pgo)
task pgo     # regenerates default.pgo from the offline scoring benchmark
task test
task lint    # golangci-lint
```

Plain `go build .` works too.

It needs Go 1.27+. The target catalogue is built into the binary.

## Usage

```sh
astro-recommender -lat 45.815 -lon 15.982 -origin -filter -bortle 6 -n 5
```

```
Location: 45.8150, 15.9820 (Mjesni odbor Zrinjevac, Gradska četvrt Donji grad, Zagreb, Grad Zagreb, Hrvatska), Europe/Zagreb
Window:   2026-10-05 20:08 - 2026-10-06 05:22 (9h14m0s)
Moon:     24% illuminated, min separation 24°
Size:     4.1' - 45.0'
Targets:  GaryImm, 2 comets brighter than mag 12.0
Frame:    1.32° x 0.75°, 1.23"/px (3856 x 2180 px)
Sky:      Bortle 6 (zenith 18.80 mag/arcsec²), extinction 0.23-0.25 (elevation 126 m, aerosols) mag/airmass, filter k=0.25

HOUR   CLOUD  LOW/MID/HIGH  TRANSP  EXT   SEEING  DEW SPREAD  WIND/GUST
20:00  50%    0/0/100%      2/8     0.25  1.5-2"  6.0°C       5/13 km/h
21:00  50%    0/0/100%      2/8     0.24  1.5-2"  7.6°C       5/12 km/h
22:00  50%    0/0/100%      2/8     0.23  1.5-2"  7.6°C       4/10 km/h
23:00  54%    0/8/100%      2/8     0.23  1.5-2"  7.1°C       4/8 km/h
00:00  50%    0/0/100%      2/8     0.23  1.5-2"  6.7°C       4/8 km/h
01:00  50%    0/0/100%      2/8     0.23  1.5-2"  6.0°C       4/7 km/h
02:00  50%    0/0/100%      2/8     0.23  1.5-2"  5.9°C       3/7 km/h
03:00  7%     0/0/15%       2/8     0.23  1.5-2"  5.0°C       1/4 km/h
04:00  2%     0/0/3%        2/8     0.23  1.5-2"  3.3°C       1/1 km/h
05:00  0%     0/0/0%        2/8     0.23  1.5-2"  4.0°C       1/3 km/h

#  NAME      DESCRIPTION                  TYPE               CONSTELLATION  SIZE  FOTO  SCORE  MAX ALT      SKY   PX
1  NGC 281   PacMan Nebula                Emission Nebula    Cassiopeia     35'   1.00  0.24   79° @ 00:52  18.7  1707
2  IC 63     Gamma Cassiopeia Nebula      Emission Nebula    Cassiopeia     20'   1.00  0.24   75° @ 00:59  18.7  976
3  Sh2-173   Phantom of the Opera Nebula  Emission Nebula    Cassiopeia     25'   1.00  0.24   74° @ 00:20  18.7  1220
4  IC 1795   Fishhead Nebula              Emission Nebula    Cassiopeia     30'   1.00  0.24   74° @ 02:26  18.7  1463
5  Abell 85  CTB 1 or Garlic Nebula       Supernova Remnant  Cassiopeia     35'   1.00  0.23   73° @ 23:59  18.7  1707
```

The output ends with a legend explaining every column of the tables shown and the colours. In short:

- **FOTO**: the fraction of the window during which the object meets the altitude, horizon and Moon-distance limits (uptonight's metric).
- **SCORE**: imaging quality from 0 to 1, where 1 means a perfect minute for the whole window under a pristine dark sky. It accounts for weather, extinction, sky brightness and, with framing, how well the object fits the frame. This column decides the order.
- **MAX ALT**: the object's highest altitude in the window, and when it occurs.
- **SKY**: the average sky brightness at the object while it is observable, in V mag/arcsec². Higher is darker: about 22 is pristine and 18–19 is a suburb or a bright Moon. The filter isn't included.
- **PX**: the object's size in pixels at the frame's pixel scale; `-` without framing (`-origin`, `-fov`, `-scale`, `-focal`), since there is no frame to measure against.

On a colour-capable terminal the output is colour-coded green/yellow/red. Colours are turned off when output is piped, when `NO_COLOR` is set or when `TERM=dumb`. `CLICOLOR_FORCE=1` turns them on regardless.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-lat`, `-lon` | required | Location in degrees, north and east positive |
| `-tz` | from location | IANA time zone used for all displayed times. By default it's looked up offline from `-lat`/`-lon`, falling back to the system zone |
| `-date` | tonight | Evening to plan, `YYYY-MM-DD`. Without it, a run during the night (after dusk, or after midnight) plans only the rest of the night in progress |
| `-from`, `-to` | dusk, dawn | Imaging window in local `HH:MM`, e.g. `-from 22:00 -to 02:00`. Times before noon mean the next morning. Always clamped to full astronomical night |
| `-n` | `20` | Number of objects to list |
| `-plan` | `0` (off) | Print a night plan with one target per block of this length, e.g. `2h` |
| `-horizon` | none | Local horizon file (see below) |
| `-origin` | off | Framing for the Celestron Origin (IMX678, 3856 × 2180 px of 2.0 µm at 335 mm: 1.32° × 0.75°, 1.23″/px): keep only objects that fit the frame and are at least `-min-px` across, and score how well they fill it |
| `-fov`, `-scale` | Origin | Framing for another telescope: field of view `WxH` in degrees up to 180 (e.g. `2.5x1.7`) and pixel scale in ″/px between 0.01 and 1000. Either one turns framing on; the one you leave out uses the Origin's value. When both are given, a frame of more than 20,000 px across is rejected, which catches a field typed in arcminutes or a scale from another camera; with only one of them set there is nothing to compare and a field typed in arcminutes goes unnoticed, so check the `Frame:` line in the header |
| `-focal`, `-sensor`, `-pixel` | off | Describe the camera instead: focal length in mm, sensor size `WxH` in mm and pixel size in µm (e.g. `-focal 400 -sensor 23.5x15.6 -pixel 3.76`). `-sensor` sets the field of view (`2·atan(side / 2·focal)` per side) in place of `-fov`; `-pixel` sets the pixel scale (`206.265 · pixel / focal`) in place of `-scale`. Both need `-focal`, `-focal` needs at least one of them, and the pair they replace cannot be given with them. The 20,000 px check applies whenever both the field and the scale are given, by either route |
| `-min-px` | `200` | With framing: minimum object size in pixels (≈4.1′ on the Origin); must be 0 or more even when framing is off |
| `-filter` | off | A dual- or tri-band nebula filter is in use |
| `-filter-k` | `0.25` | Fraction of sky glow the filter passes: ~0.15 for ≤4 nm bands, ~0.4 for wide ones |
| `-bortle` | `0` (dark) | Bortle class (1–9) of the site, which sets its zenith sky brightness |
| `-sqm` | unset | Measured zenith sky brightness in mag/arcsec², from an SQM meter or the *World Atlas 2015* layer on [lightpollutionmap.info](https://www.lightpollutionmap.info). More precise than `-bortle`, which it overrides; the header shows the matching Bortle class. When neither is given and `DARKSKYSITES_API_KEY` is set, it is looked up on [DarkSkySites](https://www.darkskysites.com/api-access); a free API key can be requested at [darkskysites.com](https://www.darkskysites.com/api-access#apply) |
| `-extinction` | auto | Atmospheric extinction in magnitudes per airmass. By default it is estimated per hour from the site's elevation and the CAMS aerosol forecast. Setting it fixes the value; `0.2` is used when no estimate is available |
| `-alt-min`, `-alt-max` | `30`, `80` | Altitude limits in degrees |
| `-size-min`, `-size-max` | `10`, `300` | Object size limits in arcminutes, 0 or more; `-size-min 0` means no minimum. Framing replaces them with `-min-px` × scale up to the frame's short side (4.1′–45.0′ on the Origin), since the size is the major axis and its orientation in the frame is unknown, unless you set them explicitly; a `-size-max` above the short side is an error, because such objects cannot be framed |
| `-list` | `GaryImm` | Built-in target list (see below) |
| `-targets` | none | Your own uptonight-format targets YAML file; overrides `-list` |
| `-comet-mag` | `12` | Include comets brighter than this total visual magnitude |
| `-no-comets` | off | Skip comets |
| `-no-weather` | off | Skip the Open-Meteo and 7Timer forecasts |
| `-no-geocode` | off | Skip the reverse geocoding of the location |
| `-no-sqm` | off | Skip the DarkSkySites sky brightness lookup |
| `-version` | | Print the version, commit and build time, then exit |

### Target lists

All eight [uptonight target lists](https://github.com/mawinkler/uptonight/tree/main/targets) are built in, plus the full Gary Imm compendium and the Milky Way Star Clusters catalogue. Pick one with `-list`; names aren't case-sensitive:

| List | Objects | Contents |
|------|---------|----------|
| `GaryImm` (default) | 206 | Gary Imm's top astrophotography targets plus the top of his Deep Sky Compendium; sizes and positions checked against the 2026 (6th) edition |
| `GaryImmFull` | 3145 | All objects of Gary Imm's Deep Sky Compendium (2026, 6th edition), including his 0–5 rating (see below) |
| `Messier` | 110 | The Messier catalogue |
| `Herschel400` | 400 | The Astronomical League's Herschel 400 |
| `Pensack500` | 502 | Don Pensack's 500 best deep sky objects |
| `OpenNGC` | 8373 | The NGC, from [OpenNGC](https://github.com/mattiaverga/OpenNGC) |
| `OpenIC` | 5589 | The IC, from OpenNGC |
| `LBN` | 1116 | Lynds' Catalogue of Bright Nebulae |
| `LDN` | 1764 | Lynds' Catalogue of Dark Nebulae |
| `MWSC` | 3006 | Milky Way Star Clusters (Kharchenko et al. 2013): open and globular clusters, associations and moving groups; size is the diameter of the central part (r1); the 56 Messier clusters carry their M number and common name |

`GaryImmFull.yaml` is generated from the compendium spreadsheet by `scripts/imm2yaml.py` (Python standard library only):

```sh
scripts/imm2yaml.py IMM_Compendium_2026.xlsx > internal/catalog/targets/GaryImmFull.yaml
```

`MWSC.yaml` is generated by `scripts/mwsc2yaml.py` from the CDS catalogue:

```sh
curl -O https://cdsarc.cds.unistra.fr/ftp/J/A+A/558/A53/catalog.dat
scripts/mwsc2yaml.py catalog.dat > internal/catalog/targets/MWSC.yaml
```

The `imm2yaml.py` script checks each decimal RA/Dec against the raw h/m/s and d/m/s cells, and uses the raw value when they disagree. In the 2026 edition this fixed NGC 4526, NGC 5985 and NGC 7094. It also fixes three Dec values typed without seconds (NGC 3813, NGC 3998, NGC 5473), which would otherwise put those Ursa Major galaxies on the celestial equator.

Objects with unknown size (`-9999` in the list, or `0.0` in a few LDN entries) count as size 0: they are dropped by the default `-size-min`, and `-size-min 0` keeps them (with no framing penalty, since there is nothing to judge).

The CONSTELLATION column is computed from the official IAU boundaries for every target, comets included, rather than taken from the list. Across all ~18,000 list entries the two agree except for spelling variants ("Ophiucus", "Se1") and five objects. Three of those are list errors, such as R Aquarii listed in Aquila; the other two sit right on a boundary.

### Comets

Comets are added to whichever list you use, and ranked with the same scoring:

- **Data:** orbital elements come from the Minor Planet Center's [`CometEls.txt`](https://www.minorplanetcenter.net/iau/MPCORB/CometEls.txt). The file is cached for a day in your user cache directory (`~/Library/Caches/astro-recommender` on macOS), and a stale copy is used if the download fails.
- **Positions:** computed every minute from a two-body orbit (elliptic, parabolic or hyperbolic). Checked against JPL Horizons, they agree to within 0.003–0.02° when MPC and JPL share the same orbit solution.
- **Which comets:** those brighter than `-comet-mag` (default 12) at mid-window, using MPC's magnitude formula `m = H + 5·log Δ + 2.5·K·log r`. For 12P/Pons-Brooks at its 2024 perihelion this gives 4.42, matching JPL. Comet brightness predictions are uncertain by a magnitude or more, and MPC and JPL sometimes differ by several magnitudes for a given comet.
- **Display:** comets show their magnitude and their distances from the Sun (r) and the Earth (Δ) as the description, with `-` for size and pixels. A coma has no catalogue size, so size limits and framing don't apply to comets.

### Night plan

`-plan 2h` splits the window into 2-hour blocks, plus a shorter last block, and picks one target for each block. It prints the schedule above the ranked list:

```
TIME         NAME     DESCRIPTION          TYPE               SCORE  PEAK ALT
20:08-22:08  IC 1396  Elephant's Trunk     Dark Nebula        0.67   78° @ 21:37
22:08-00:08  Sh2-132  Lion Nebula          Wolf-Rayet Nebula  0.92   80° @ 22:17
00:08-02:08  NGC 281  PacMan Nebula        Emission Nebula    0.66   79° @ 00:50
02:08-04:08  IC 1795  Fishhead Nebula      Emission Nebula    0.44   74° @ 02:23
04:08-05:22  IC 405   Flaming Star Nebula  Emission Nebula    0.71   79° @ 05:13
```

First, each block in time order gets the unused target with the best score within that block, with mean altitude breaking ties. Then a local search, modelled on [astrogo](https://github.com/TuSKan/astrogo)'s `SwapOptimizedStrategy`, swaps targets between blocks, moves a target into an empty block or brings in an unused one, whenever that raises the total score. The result is a good plan for one night, though not guaranteed to be the best possible. Combine it with `-from`/`-to` to plan just part of the night.

### Local horizon

`-horizon horizon.txt` replaces the fixed altitude floor with your real horizon. A minute counts only if the target is above both `-alt-min` and the horizon in its direction. The file lists azimuth (0 = north, 90 = east) and the minimum visible altitude, in degrees. Altitudes between the listed directions are interpolated, wrapping from 360° back to 0°:

```
# azimuth altitude
0    25
45   60   # house
90   35
180  20
270  40   # trees
```

To measure it, stand where the telescope sits and use a compass and an inclinometer app to note the top of each obstruction every 15–30°.

## How objects are ranked

1. **Time window.** Each minute from astronomical dusk to astronomical dawn is checked. The search runs for 24 hours from the site's solar noon, worked out from its longitude, so polar night is capped at 24 hours and the time zone can't cut the night short. Without `-date`, a run during the night (after dusk, before dawn) plans only what's left of the night in progress; otherwise it plans the coming evening. If full night never happens (for example at high latitudes in summer), the program says so and lists nothing. `-from`/`-to` narrow this to part of the night, but never extend it past dusk or dawn. A window that falls entirely outside the night is an error. Moon illumination is taken at the middle of the window.
2. **Observable minutes**, following uptonight's rules:
   - the object is between 30° and 80° altitude (≥ 30° also covers uptonight's airmass ≤ 2 limit), and above the `-horizon` profile if given
   - it is at least *Moon illumination %* degrees from the Moon, e.g. 25% lit means 25° away
   - unlike uptonight, the Moon-distance rule is skipped while the Moon is below the horizon
3. **Sky glow sensitivity** (`k`) is the fraction of sky glow that counts against a target:
   - galaxies, reflection nebulae and dark nebulae get `k = 1`
   - open and globular clusters are bright and compact: `k = 0.5`
   - with `-filter`, emission-line targets get `k = -filter-k`. These are emission nebulae, planetary nebulae, supernova remnants and Wolf-Rayet nebulae, plus a few hydrogen-emitting objects the catalogue mislabels, such as IC 1396 and the Cave Nebula.

   `k` also scales the Moon-distance limit.
4. **Weather**, per minute:
   - **clouds** ([Open-Meteo](https://open-meteo.com/)): low and mid cloud count in full; thin high cloud counts half
   - **transparency** ([7Timer!](https://www.7timer.info/), 1 best, 8 worst): weight goes from ×1.0 down to ×0.5
   - **dew risk**: when temperature minus dew point drops from 4 °C to 1 °C, weight goes from ×1.0 down to ×0.7
   - **wind gusts**: from 20 to 40 km/h, weight goes from ×1.0 down to ×0.5
   - seeing is shown for reference only; it matters little at the Origin's 1.23″/px

   Open-Meteo covers about 3 months back to 2 weeks ahead, and 7Timer about 3 days ahead. Entirely outside those ranges, or if a service is down, the program warns and assumes perfect conditions. A forecast that ends partway through the night (7Timer's often does for a `-date` two or three days ahead) gives the remaining hours the mean of the covered ones, with a warning.
5. **Extinction**: the target's light dims by `10^(−0.4 · k · (airmass − 1))`, using Pickering's (2002) airmass formula. With k = 0.2, that is about ×0.83 at 30° altitude and ×1.0 near the zenith. Unless `-extinction` is set, k is estimated for each hour as
   `0.1066 · e^(−elevation / 7996 m)` (scattering by air molecules at 550 nm, less at higher sites) `+ 0.029` (ozone) `+ 1.086 · AOD₅₅₀` (aerosols: haze, dust, smoke).
   The aerosol optical depth (AOD) comes from the [Open-Meteo Air Quality API](https://open-meteo.com/en/docs/air-quality-api), which relays the CAMS forecast up to about 5 days ahead; hours beyond that use a typical 0.1. The forecast table's `EXT` column shows k per hour. A dusty or smoky night pushes it up sharply, penalising low targets and spreading moonlight more widely.
6. **Sky brightness**: the sky brightness at the target is computed each minute with the Krisciunas & Schaefer (1991) model:
   - the site's moonless zenith brightness: from `-sqm`, else the typical value for `-bortle`, else 22.0 mag/arcsec²
   - brightening towards the horizon
   - moonlight scattered towards the target, depending on Moon phase (with the extra brightening near full Moon), its distance, Moon altitude, target altitude and the angle between Moon and target

   Imaging faint targets is limited by the sky, so the signal-to-noise ratio in a fixed exposure scales as `1/√sky`. The weight is `min(1, √(B_dark / (k · B)))`, where `B_dark` is a pristine 22.0 mag/arcsec² sky. A target 30° from a bright Moon therefore scores much lower than one 120° away, and with a filter, emission targets keep most of their score.
7. **Score** = the sum over observable minutes of weather × extinction × sky weight, divided by the window length.
8. **Framing** (`-origin`, `-fov`, `-scale`, or `-focal` with `-sensor`/`-pixel`): objects filling 25–80% of the frame's short side score in full (11–36′ on the Origin). Smaller or tighter-fitting objects score less, and an object as large as the short side cannot be framed and is not listed.
9. Results are sorted by score; mean altitude breaks ties.

Extinction and sky brightness come from published models. The weather ramps, the cluster `k` and the framing curve are simple heuristics. Treat SCORE as a way to order tonight's options rather than an absolute measure of quality.

### Accuracy

The astronomy uses compact published formulas rather than a full ephemeris library. `TestSOFA` checks them against [SOFA](http://iausofa.org) (through [gofa](https://github.com/hebl/gofa), a test-only dependency) over 3000 random dates in 2000–2050; the limits it enforces are:

| Quantity | Formula | Difference from SOFA |
|---|---|---|
| Sidereal time | IAU GMST polynomial | < 0.3″ |
| Sun | Astronomical Almanac low precision | < 1′ |
| Moon | Astronomical Almanac low precision (geocentric) | < 0.4° |
| Moon from the site | the same, shifted by its horizontal parallax | < 0.4° against SOFA's Moon seen from the WGS84 site |
| Moon illumination | from Sun–Moon elongation | < 0.3 percentage points |
| Target altitude | J2000 catalogue positions precessed to date (IAU 1976, Lieske 1977), GMST | < 0.3′ against SOFA's full precession-nutation and apparent sidereal time (precession itself within 0.1″) |
| Earth position (for comets) | Sun formula, corrected to J2000 | < 0.0003 AU |

The Moon's position, for both its altitude and the separation rule, is topocentric (seen from the site, as in astroplan): its horizontal parallax (54′–61′) shifts it by up to 1° from the geocentric position. Nutation (< 20″) and refraction (about 2′ at 30° altitude) are ignored. All of this is far tighter than the 30° altitude and tens-of-degrees Moon limits need.

### Formulas and references

Everything below is implemented directly in Go from the cited source; no astronomy library is used at run time.

**Positions and time** (`internal/astro`)

| Quantity | Formula | Source |
|---|---|---|
| Sun RA/Dec | Mean longitude and anomaly, equation of centre to 2 terms, apparent ecliptic longitude | *The Astronomical Almanac*, "Low-precision formulas for the Sun's coordinates" (section C) |
| Moon RA/Dec | Ecliptic longitude (6 periodic terms) and latitude (4 terms), geocentric | *The Astronomical Almanac*, low-precision Moon formulas (section D) |
| Obliquity of the ecliptic | `23.439° − 0.0000004 n` (n = days since J2000) | *The Astronomical Almanac*, section C |
| Ecliptic → equatorial | Rotation by the obliquity | Standard (Meeus, *Astronomical Algorithms*, ch. 13) |
| Greenwich sidereal time | `280.46061837° + 360.98564736629° · n`, the linear terms of IAU 1982 GMST | Aoki et al. (1982), A&A 105, 359, as given by Meeus eq. 12.4 |
| Precession J2000 → date | IAU 1976 angles ζ, z, θ and the three-rotation formula | Lieske et al. (1977), A&A 58, 1; Meeus eqs. 21.2–21.4 |
| Altitude and azimuth | Spherical triangle from hour angle, declination and latitude | Meeus eqs. 13.5–13.6 |
| Angular separation | Spherical law of cosines | Meeus eq. 17.1 |
| Moon illumination | `(1 − cos ψ)/2` from the Sun–Moon elongation ψ; phase angle ≈ 180° − ψ | Meeus ch. 48 |
| Topocentric Moon | Geocentric vector at the distance given by the horizontal parallax (4 periodic terms), minus the observer's position on a spherical Earth rotated by local sidereal time | *The Astronomical Almanac*, low-precision Moon formulas (section D); Meeus ch. 40 |
| Astronomical twilight | Sun's geometric centre below −18° | Standard definition (USNO) |
| Earth heliocentric position (for comets) | Negated Sun vector, with general precession (1.3970°/century) removed to return to the J2000 frame | Almanac Sun formula; Lieske et al. (1977) precession rate |

**Comets** (`internal/comets`)

| Quantity | Formula | Source |
|---|---|---|
| Elliptic orbits | Kepler's equation `E − e sin E = M` by Newton–Raphson | Standard (Meeus ch. 30) |
| Parabolic orbits | Barker's equation, closed form via the cubic | Meeus ch. 34 |
| Hyperbolic orbits | `e sinh H − H = M` by Newton–Raphson | Standard |
| Orbit → ecliptic → equatorial | Rotation by ω, Ω, i, then by the J2000 obliquity 23.4392911° | Meeus ch. 33; MPC elements are J2000 ecliptic |
| Gaussian gravitational constant | `k = 0.01720209895` rad/day | Gauss (1809), IAU 1976 system of constants |
| Total magnitude | `m₁ = H + 5 log₁₀ Δ + 2.5 K log₁₀ r` | Minor Planet Center magnitude parameters (K = 2.5 n) |

**Atmosphere and sky** (`internal/atmos`)

| Quantity | Formula | Source |
|---|---|---|
| Airmass | `X = 1 / sin(h + 244 / (165 + 47 h^1.1))` | Pickering (2002), DIO 12, 1, §A |
| Extinction | Transmission `10^(−0.4 k (X − 1))` relative to the zenith | Standard |
| Extinction coefficient `k` | Rayleigh `0.1066 · e^(−h / 7996 m)` (τ = 0.098 at 550 nm, 8 km scale height) + ozone Chappuis band 0.029 (300 DU) + aerosols `1.086 · AOD₅₅₀` | Rayleigh after Hayes & Latham (1975), ApJ 197, 593, consistent with Bucholtz (1995); ozone from the Chappuis cross-section; AOD from CAMS via Open-Meteo. `1.086 = 2.5 log₁₀ e` |
| Surface brightness units | `B[nL] = 34.08 · e^(20.7233 − 0.92104 V)` and its inverse | Krisciunas & Schaefer (1991), PASP 103, 1033 |
| Dark-sky brightness vs altitude | Zenith brightness × `10^(−0.4 k (X − 1)) · X` with the scattering airmass `X = (1 − 0.96 sin² Z)^−½` | Krisciunas & Schaefer (1991) |
| Scattered moonlight | Scattering function `f(ρ) = 10^5.36 (1.06 + cos² ρ) + 10^(6.15 − ρ/40)`, Moon brightness `10^(−0.4 (3.84 + 0.026 α + 4·10⁻⁹ α⁴))`, scaled by `(60.27 / distance in Earth radii)²` and, within 7° of full, by the opposition surge `1.35 − 0.05 α`; attenuated by the Moon's airmass and scaled by `1 − 10^(−0.4 k X)` | Krisciunas & Schaefer (1991); the surge as in Thorstensen's skycalc |
| Bortle class ↔ zenith brightness | Mid-points of the SQM ranges usually quoted per class (21.9 … 17.3 mag/arcsec²) | Bortle (2001), *Sky & Telescope*, Feb 2001, with the common SQM mapping |
| Sky-limited SNR weight | `min(1, √(B_dark / (k · B)))`, `B_dark` = 22.0 mag/arcsec² | Sky-limited imaging: SNR ∝ 1/√B |

**Heuristics** (this project's own; see "How objects are ranked"): the weather ramps for cloud, transparency, dew spread and gusts; the sky-glow fractions `k` for clusters and filtered emission targets; the frame-fill curve (full credit at 25–80% of the short side); and the Moon-distance rule from uptonight.

**Constellations** (`internal/constellation`): IAU boundaries (Delporte 1930) at B1875.0 from CDS/VizieR [VI/49](https://vizier.cfa.harvard.edu/viz-bin/VizieR?-source=VI/49) (Davenhall & Leggett 1989); the target is precessed to B1875.0 with the IAU 1976 formula and tested by ray casting, following Roman (1987), PASP 99, 695, with the table and test taken from [astrogo](https://github.com/TuSKan/astrogo).

## Privacy

The program calls three free services, none of which need an API key: Open-Meteo (weather and air quality), 7Timer and OpenStreetMap Nominatim. With `DARKSKYSITES_API_KEY` set (and no `-sqm`/`-bortle`), it also asks DarkSkySites for the site's sky brightness (free API key on request at [darkskysites.com](https://www.darkskysites.com/api-access#apply)). The comet elements download from the Minor Planet Center sends no location, and the time zone lookup runs offline. Before sending, it rounds your coordinates to two decimals (about 1 km). Reverse geocoding asks only for suburb-level detail. Use `-no-weather -no-geocode -no-sqm -no-comets` (or leave `DARKSKYSITES_API_KEY` unset) to run entirely offline.

## Credits

- Planning ideas (airmass weighting, sky-brightness scoring, swap-optimised scheduling) from [astrogo](https://github.com/TuSKan/astrogo) by TuSKan (MIT).
- [uptonight](https://github.com/mawinkler/uptonight) by Markus Winkler (MIT): the observability rules and the built-in target lists in `internal/catalog/targets/`. The default `GaryImm.yaml` is based on Gary Imm's top astrophotography targets and his Deep Sky Compendium. Ten of its entries were corrected against the 2026 (6th) edition of the Compendium. Seven sizes were plainly wrong; for example the Soul Nebula was listed at 1.8′ instead of 90′. Three positions had been updated in the new edition.
- `GaryImmFull.yaml`: Gary Imm's Deep Sky Compendium (2026, 6th edition), data © Gary Imm. The spreadsheet states no redistribution terms.
- `OpenNGC.yaml` and `OpenIC.yaml` derive from [OpenNGC](https://github.com/mattiaverga/OpenNGC) by Mattia Verga, licensed [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/).
- `MWSC.yaml`: Milky Way Star Clusters, Kharchenko et al. 2013, A&A 558, A53, from CDS/VizieR catalog [J/A+A/558/A53](https://cdsarc.cds.unistra.fr/viz-bin/cat/J/A+A/558/A53).
- Comet orbital elements: [IAU Minor Planet Center](https://www.minorplanetcenter.net/).
- Constellation boundaries and lookup from [astrogo](https://github.com/TuSKan/astrogo) (MIT), transcribing the IAU boundaries (Delporte 1930) from CDS/VizieR catalog [VI/49](https://vizier.cfa.harvard.edu/viz-bin/VizieR?-source=VI/49) (Davenhall & Leggett 1989).
- Weather and aerosols: [Open-Meteo](https://open-meteo.com/) (CC BY 4.0; aerosol data from the Copernicus Atmosphere Monitoring Service) and [7Timer!](https://www.7timer.info/).
- Geocoding: [Nominatim](https://nominatim.openstreetmap.org/), data © OpenStreetMap contributors (ODbL).
- Time zones: [tzf](https://github.com/ringsaturn/tzf) (MIT), a Go counterpart of [node-geo-tz](https://github.com/evansiroky/node-geo-tz). Both use [timezone-boundary-builder](https://github.com/evansiroky/timezone-boundary-builder) data (ODbL), which is embedded and looked up offline.
