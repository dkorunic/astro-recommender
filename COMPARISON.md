# Comparison with other tools

How astro-recommender compares with other target-selection tools. Other tools' details come from their manuals, websites, source code, interfaces and authors' forum posts, as of October 2026; "not documented" means the vendor does not publish the method. astro-recommender's formulas are in [MODELS.md](MODELS.md).

| Tool | Platform | Ranking | Moon | Weather | Objects |
|---|---|---|---|---|---|
| **astro-recommender** | CLI, terminal UI, web (MIT) | physical score, per minute | scattered moonlight vs surface brightness | weights each minute | ~32,000 (44,000 entries) |
| Telescopius | web, mobile | filters; unranked tonight list | per-filter distance presets | no | 55 catalogues |
| SkyWarden | web | 100-point score | 30 points | drops cloudy objects | 12,755 |
| SkyTools 4 | Windows, paid | contrast, imaging SNR | in the sky model | no | large |
| Deep-Sky Planner 9 | Windows, paid | visual visibility classes | no | no | 1.6 million |
| AstroPlanner | macOS, Windows, shareware | plan wizard | not documented | no | 100+ catalogues |
| SkySafari 8 Pro | iOS, Android, macOS, paid | curated list, by transit | no | no | 780,000 |
| Stellarium | desktop (GPL) | lists by type, magnitude | no | no | 94,000+ |
| Cartes du Ciel / SkyChart | desktop (GPL) | none | no | no | OpenNGC + CDS |
| KStars | desktop (GPL) | filters | no | no | ~13,200 |
| N.I.N.A. Sky Atlas | Windows | filters, sorting | distance filter | no | ~10,300 |
| Astrospheric, Clear Outside | forecasts | none | — | forecast only | — |
| uptonight | CLI (MIT) | observable fraction | illumination % in degrees | no | 8 lists |

## What astro-recommender does differently

- **Sky against the object.** Each minute: `√((S + B_ref)/(S + B))`, with B from the zenith sky, solar-cycle airglow, zodiacal and galactic light per direction and Winkler (2022) moonlight, and S a measured surface brightness where a catalogue has one (`TestScoreSurfaceBrightness`).
- **Forecast weights minutes.** Cloud by layer, rain, dew and gusts scale each minute instead of gating the night. Uncovered hours get the covered hours' mean, with a warning.
- **Extinction from forecast aerosols.** Per hour: Rayleigh by elevation, ozone, CAMS AOD above the site, Pickering airmass.
- **Filters as a spectral model.** Line ratios by type, sky split into glow, Rayleigh and aerosol moonlight, per-band extinction, band shift in fast beams (`-filter-std`, fitted to Chroma's curves). Each filter you own is scored as worn all night; the best, or with framing the fewest-subs one, is named (`TestScoreFilter`: H II Hα > dual-band > CLS > L-Pro > none; planetaries dual-band > Hα > [O III] > CLS > L-Pro > none; galaxies and clusters never filtered).
- **SUBS per target.** Subs for SNR 5, 10, 20 from the same per-minute sky, with aperture, obstruction, throughput, QE, read noise, dark current at the night's temperature, seeing and optics blur, stacking mode, flat residual ceiling, and per-channel colour response (21 smart telescopes, 45 sensors).
- **Framing and mosaics in the score.** Frame fill scales the score, mosaics share it among panels (`TestMaxFramed`).
- **Field rotation.** On alt-az mounts, minutes whose sub trails the object's edge past a star's FWHM are not observable.
- **Tested positions.** Sun, Moon, sidereal time, precession, galactic coordinates and scored altitudes against IAU SOFA (`TestSOFA`, `TestSOFAGalactic`, `TestScoreSOFA`; [limits](README.md#accuracy)).
- **Calibrated seeing.** ECMWF upper-air profile through Dewan's Cn², fitted to ESO DIMM data at Paranal and La Silla (bias +0.03″, RMSE 0.33″). Used for SUBS' blur, not the score.
- **Reproducible catalogues.** 28 of 34 lists come from checked-in scripts and named upstream versions; uptonight's six carry documented corrections. `TestEmbeddedLists` fails on drift.
- **Open and portable.** MIT, static binary or browser page, no API key required, offline with the five `-no-*` flags, coordinates rounded to about 1 km. Clear night alerts through `astro-notify`.

## Per tool

**[Telescopius](https://telescopius.com/)** (free web, mobile)
- Selects: "What's in the sky tonight" is unranked, at least 30° for an hour, size limits by type; search by altitude, quadrant, type, catalogue, Moon distance, size, magnitude, surface brightness.
- Conditions: Lorentzian Moon-avoidance presets (22°, 34°, 45° at a 3% Moon for Hα/[S II], [O III], LRGB); forecast on a separate page.
- Catalogues: 55, including PGC, UGC, Sharpless, RCW, vdB, HCG, Zwicky.
- astro-recommender lacks: interactive survey-image framing; PGC, UGC, MCG and smaller lists (Henize's Milky Way objects, Herbig-Haro, Sandqvist, Wolf-Rayet stars); a mobile app; a gallery.

**[SkyWarden](https://skywarden.app/)** (free web)
- Selects: 100-point score: Moon 30, usable time 25, imaging ease 20, altitude 15, achievable tonight 10; `.hrz` horizon; one recommended filter for the night. Formulas unpublished.
- Conditions: light-pollution level 1–9; Open-Meteo cloud drops objects; seeing from the 300 hPa wind.
- Catalogues: 12,755 in 7 catalogues.
- astro-recommender lacks: planning over the year, a sky map, a light-pollution lookup in the CLI without a key.

**[SkyTools 4](https://skyhound.com/)** (paid, Windows)
- Selects: visual contrast model for observing lists; imaging SNR model with exposure calculator and scheduler.
- Conditions: sky background from Moon separation, altitude and light pollution, seeing model; no forecast documented in selection.
- Catalogues: 16–522 million stars by edition, integrated deep sky databases.
- astro-recommender lacks: a full exposure calculator (only sub counts for one sub length), a scheduler, visual contrast, an atlas, telescope control.

**[Deep-Sky Planner 9](https://knightware.biz/dsp/)** (paid, Windows)
- Selects: plans filtered by visual visibility classes from aperture, pupil, darkness, transparency.
- Conditions: darkness by hand, SQM or Falchi zone; no forecast.
- Catalogues: 1.6 million objects. Exports to N.I.N.A., SGP, APT, ACP.
- Positions: abridged VSOP87, ELP 2000-82B Moon (< 11″).
- astro-recommender lacks: about 50× the objects, export to SGP, APT and ACP, logging, arcsecond ephemerides.

**[AstroPlanner](https://astroplanner.net/)** (shareware, macOS, Windows)
- Selects: Plan Creation Wizard by telescope, site, skill level; user-contributed plans; scripting.
- Conditions: not documented. Catalogues: over 100.
- astro-recommender lacks: user-contributed plans, scripting, logging, telescope control.

**[SkySafari 8 Pro](https://skysafariastronomy.com/)** (paid, iOS, Android, macOS)
- Selects: "Tonight's Best", curated showpieces reaching 6° between astronomical dusk and dawn, by transit time.
- Conditions: none.
- Catalogues: 780,000 deep sky objects, 3 million galaxies, 750,000 solar system objects.
- astro-recommender lacks: a sky chart, telescope and camera control, plate solving, a much larger database.

**[Stellarium](https://stellarium.org/)** (free, GPL)
- Selects: "What's Up Tonight" by type and magnitude; Observability plugin for one object.
- Conditions: Bortle shapes the rendered sky, not the list.
- Catalogues: 94,000+, over a million extended.
- Positions: VSOP87 (1″), ELP 2000-82B, JPL DE430/431/440/441 when installed.
- astro-recommender lacks: rendered sky, arcsecond ephemerides, planets as targets, telescope control.

**[Cartes du Ciel / SkyChart](https://www.ap-i.net/skychart/)** (free, GPL)
- Selects: observing lists and tours, no ranking. Conditions: none.
- Catalogues: OpenNGC; PGC/LEDA and any CDS catalogue through CatGen.
- Positions: JPL DE440 for 2000–2050, Moshier's plan404 outside.
- astro-recommender lacks: charts, DE440 positions, CDS catalogues on demand.

**[KStars](https://kstars.kde.org/)** (free, GPL)
- Selects: "What's Up Tonight" and Observation Planner: filters by type, magnitude, date, site. Conditions: none.
- Catalogues: OpenNGC (~7,840), IC (5,386); Sharpless, Abell PNe, Hickson as downloads; stars to 16.5.
- Positions: VSOP87, Meeus lunar series; precession, nutation and refraction applied.
- astro-recommender lacks: charts, the Ekos capture suite and scheduler.

**[N.I.N.A.](https://nighttime-imaging.eu/) Sky Atlas** (free, Windows)
- Selects: filters by type, magnitude, constellation, size, surface brightness, altitude held, transit, Moon distance; sends to framing, sequencer or mount.
- Catalogues: ~10,300, synced from SIMBAD and NED in 2019.
- astro-recommender lacks: capture, interactive framing, sequencer, mount control. Complementary: `-csv` writes a list or plan its sequencer imports, without mosaic panes.

**[Astrospheric](https://www.astrospheric.com/), [Clear Outside](https://clearoutside.com/)** (forecasts)
- Astrospheric (North America): RDPS clouds, Rahill's seeing model, RAP smoke, GFS, ICON, NAM, NBM. Clear Outside: hourly cloud layers for seven days, model unpublished.
- astro-recommender lacks: regional high-resolution models, Rahill's operational seeing.

**[uptonight](https://github.com/mawinkler/uptonight)** (free Python CLI, this project's origin)
- Selects: observable for a minimum fraction of the night, sorted by it. Moon separation of at least its illumination in degrees.
- Catalogues: eight YAML lists. Positions: astroplan, astropy, Skyfield, optional refraction.
- astro-recommender lacks: planets as targets, MQTT for Home Assistant, a live mode.

## Limits

- **Models.** Weather ramps, framing curve, line ratios and typical surface brightnesses are heuristics. One nebula's line flux is good to a factor of three to five. Seeing is fitted at two Chilean sites.
- **SUBS** counts a clean image: about what Origin users report (M 51 in about 36 minutes with gaps and rejected subs, against their 35), 1–5× Seestar S50 users', most for faint galaxies ([MODELS.md](MODELS.md#sub-exposure-estimate)).
- **Forecasts** are global (Open-Meteo, ECMWF, 7Timer), not regional. Without `-sqm`, `-bortle` or a DarkSkySites key the CLI's sky defaults to 22.0.
- **Catalogues.** 44,000 entries, about 32,000 distinct, imaging-oriented; 30–100× smaller than the largest planetarium databases. A list changes only with a release; comets update daily from MPC.
- **Accuracy.** Almanac Sun (< 1′), Astronomy Engine Moon (< 30″), no nutation or refraction; altitudes within 0.3′ of SOFA. Not for mount pointing, occultations or planets (placed to about 12′ for STAR). Comets are two-body, without perturbations.
- **Not included.** Sky chart, telescope control, observing log, full exposure calculator, planets as targets, asteroids, variable or double stars. Imaging only, no visual scoring. Survey images are DSS2 previews on the terminal view and web cards.
