# Models and formulas

Formulas, sources and simplifications behind the ranking. Usage is in the [README](README.md), which also has the measured errors against SOFA ([Accuracy](README.md#accuracy)). All of it is implemented in Go from the cited sources; no astronomy library is used at run time.

## Contents

- [How the pieces fit together](#how-the-pieces-fit-together)
- [Time and positions](#time-and-positions)
- [The night window](#the-night-window)
- [The Moon](#the-moon)
- [Comets](#comets)
- [Atmosphere and sky](#atmosphere-and-sky)
- [Object surface brightness](#object-surface-brightness)
- [Weather](#weather)
- [Scoring a target](#scoring-a-target)
- [Framing and mosaics](#framing-and-mosaics)
- [Sub-exposure estimate](#sub-exposure-estimate)
- [Seeing estimate](#seeing-estimate)
- [Night plan](#night-plan)
- [Constellations](#constellations)
- [Local horizon](#local-horizon)
- [Heuristics](#heuristics)
- [References](#references)

## How the pieces fit together

The night is sampled on a **1-minute grid**. Target-independent values (sidereal time, Moon position and light, extinction, weather quality) are computed once per minute (`scoring.Sky`); each target then walks the grid (`scoring.scoreTarget`):

```math
w_i \;=\; \underbrace{Q_i}_{\text{weather}} \;\times\; \underbrace{T(h_i,\kappa_i)}_{\text{extinction}} \;\times\; \underbrace{W_{\text{sky}}(S, B_i)}_{\text{sky glow vs. object}}
```

```math
\text{SCORE} \;=\; F_{\text{frame}} \cdot \frac{1}{N}\sum_{i\,\in\,\text{observable}} w_i ,
\qquad
\text{UP} \;=\; \frac{\#\,\text{observable minutes}}{N}
```

N is the window length in minutes. Each factor lies in 0–1.

| Symbol | Meaning | Section |
|---|---|---|
| h | target altitude | [Time and positions](#time-and-positions) |
| Q | weather quality (cloud, rain, dew, gusts, transparency) | [Weather](#weather) |
| κ | extinction coefficient, mag per airmass | [Atmosphere and sky](#atmosphere-and-sky) |
| T | fraction of light surviving extinction, relative to the zenith | [Atmosphere and sky](#atmosphere-and-sky) |
| B | sky brightness at the target (light pollution and airglow, with the solar cycle + zodiacal and galactic light + moonlight) | [Atmosphere and sky](#atmosphere-and-sky) |
| S | the object's own surface brightness | [Object surface brightness](#object-surface-brightness) |
| w, f | through a `-filter`: the target's share of light in its bands, and the bands' share of each part of the sky | [Nebula filters](#nebula-filters), [Broadband filters](#broadband-filters) |
| F_frame | how well the object fills the camera frame | [Framing and mosaics](#framing-and-mosaics) |

## Time and positions

`internal/astro`

**Time base**, days since J2000.0:

```math
n = \frac{t_{\text{Unix}}}{86400} - 10957.5 ,\qquad T = \frac{n}{36525}\ \text{(Julian centuries)}
```

ΔT (about 70 s) is applied only to the Moon series; elsewhere UT stands for TT.

**Sidereal time.** Linear terms of IAU 1982 GMST ([Aoki et al. 1982][aoki], [Meeus][meeus] eq. 12.4), plus east longitude λ:

```math
\text{LST} = 280.46061837^\circ + 360.98564736629^\circ \cdot n + \lambda
```

**Sun.** *Astronomical Almanac* low-precision formulas ([section C][aa-sun]), mean longitude L, mean anomaly g:

```math
L = 280.460^\circ + 0.9856474^\circ n ,\quad
g = 357.528^\circ + 0.9856003^\circ n ,\quad
\lambda_\odot = L + 1.915^\circ \sin g + 0.020^\circ \sin 2g
```

Latitude 0, obliquity `ε = 23.439° − 0.0000004° n`. The inverse rotation gives a target's ecliptic coordinates (`astro.Ecliptic`).

```math
\tan\alpha = \frac{\cos\varepsilon \cos\beta \sin\lambda - \sin\varepsilon \sin\beta}{\cos\beta\cos\lambda},\qquad
\sin\delta = \sin\varepsilon\cos\beta\sin\lambda + \cos\varepsilon\sin\beta
```

**Precession** of J2000 positions to the mean equinox of date, IAU 1976 ([Lieske et al. 1977][lieske], [Meeus][meeus] eqs. 21.2–21.4), once per target at mid-window:

```math
\begin{aligned}
\zeta &= 2306.2181'' T + 0.30188'' T^2 + 0.017998'' T^3 \\
z     &= 2306.2181'' T + 1.09468'' T^2 + 0.018203'' T^3 \\
\theta&= 2004.3109'' T - 0.42665'' T^2 - 0.041833'' T^3
\end{aligned}
```

```math
\begin{aligned}
A &= \cos\delta_0 \sin(\alpha_0+\zeta) \\
B &= \cos\theta\cos\delta_0\cos(\alpha_0+\zeta) - \sin\theta\sin\delta_0 \\
C &= \sin\theta\cos\delta_0\cos(\alpha_0+\zeta) + \cos\theta\sin\delta_0 \\
\alpha &= \mathrm{atan2}(A,B) + z ,\qquad \delta = \arcsin C
\end{aligned}
```

C is clamped to ±1 against rounding near the pole. Nutation (< 20″) and refraction (about 2′ at 30°) are ignored.

**Altitude and azimuth** ([Meeus][meeus] eqs. 13.5–13.6), `H = LST − α`, azimuth from north through east:

```math
\sin h = \sin\varphi\sin\delta + \cos\varphi\cos\delta\cos H ,\qquad
A = \mathrm{atan2}\!\big(-\sin H\cos\delta,\ \sin\delta\cos\varphi - \cos\delta\sin\varphi\cos H\big)
```

In the scoring loop `sin φ`, `cos φ`, `sin δ`, `cos δ` are per target (`astro.Horizontal`), `cos H` uses the difference formula with per-minute LST sines, altitude limits compare as sines, and azimuth is computed only with a horizon.

**Separation.** Spherical law of cosines ([Meeus][meeus] eq. 17.1); in the loop, dot products of unit vectors, compared before any `arccos`.

**Galactic coordinates** (`astro.Galactic`): IAU 1958 frame ([Blaauw et al. 1960][blaauw]), α_P = 192.85948°, δ_P = 27.12825°, l_NCP = 122.93192°:

```math
\sin b = \sin\delta\sin\delta_P + \cos\delta\cos\delta_P\cos(\alpha-\alpha_P) ,\qquad
l = l_{\text{NCP}} - \mathrm{atan2}\big(\cos\delta\sin(\alpha-\alpha_P),\ \sin\delta\cos\delta_P - \cos\delta\sin\delta_P\cos(\alpha-\alpha_P)\big)
```

## The night window

`astro.Window`, `astro.Tonight`, `astro.ClipWindow`, `astro.ContinuePolar`

Night is the Sun's centre below −18°, or −12° with `-twilight nautical` ([USNO][usno-twilight]). The altitude is stepped each minute for 24 h from the site's **solar noon** nearest local noon (`12:00 UTC − λ/15 h`), so zones cannot cut a night, date-line zones get their own, and polar night is capped at 24 h.

- Dark at noon with light soon after: the previous night's tail, skipped.
- Dark at noon lasting past midnight (> 12 h): tonight's (85° N on 31 December).
- Never dark: no night.
- Without `-date`, a run during the night plans the remainder; a polar night in progress continues into the next one, up to 24 h from now.
- `-from`/`-to` only narrow the window. A clock before noon is next morning's.

## The Moon

`internal/astro`

**Position and distance.** [Astronomy Engine][astronomy-engine]'s `CalcMoon` (`moon.go`), [Montenbruck & Pfleger][montenbruck]'s version of the Improved Lunar Ephemeris: 104 periodic terms for longitude, latitude argument, its factor and parallax, ten for the latitude's N, eleven planetary terms. Distance r = 1/(0.999953253 sin π) Earth radii. Run on TT with [Espenak & Meeus' ΔT][deltat]. Against SOFA's Moon98 (`TestSOFA`): position within 18″, distance within 0.0034 Earth radii.

**Topocentric shift** ([Meeus][meeus] ch. 40), spherical Earth. Used for the Moon's altitude and all separations:

```math
\vec{m}_{\text{topo}} = r\,\hat{u}(\alpha,\delta) - \big(\cos\varphi\cos\text{LST},\ \cos\varphi\sin\text{LST},\ \sin\varphi\big)
```

**Illumination and phase** from elongation ψ ([Meeus][meeus] ch. 48):

```math
\text{illuminated fraction} = \frac{1-\cos\psi}{2} ,\qquad \text{phase angle } \alpha_\text{phase} \approx 180^\circ - \psi \quad (0 = \text{full})
```

Illumination for the Moon-distance rule is taken at mid-window; moonlight uses each minute's phase angle and distance. `TestSOFA`: phase angle within 0.17°, illumination within 0.14 points.

**Rise and set** (header, JSON `moon.up`): topocentric centre crossing −50′ (34′ refraction + 16′ semi-diameter). The moonlight and distance rule use the geometric horizon.

**Lunar eclipses** (`eclipse.go`, `astro.LunarEclipse`). Geometry of Astronomy Engine's `_CalcShadow`: in the plane across the shadow axis at the Moon, u of the Sun's distance beyond Earth, the umbra's radius is k = R☉ − (1 + u)(R☉ − R⊕), the penumbra's p = −R☉ + (1 + u)(R☉ + R⊕), with R⊕ = 6371 + 88 km and R☉ = 695 700 km. With r the Moon's distance from the axis and R☾ = 1737.4 km: penumbral while r < p + R☾, partial while r < k + R☾, total while r + R☾ < k. `TestLunarEclipse`: seven eclipses of 2025–2028 within 2 minutes of its contacts.

From a point c off the axis, the Sun's and Earth's discs have radii (p − k)/2 and (p + k)/2, c apart; the point sees one less their overlap. `astro.LunarEclipse`'s `Light` averages that over 256 equal-area points of a uniformly bright Moon disc; a point seeing no Sun gets `umbraLight` = 10⁻⁴ (Danjon L = 4; moonlight that faint is far under any sky). Sun limb darkening is left out.

`scoring.BuildSky` fills `Sky.Eclipse` only when the mid-window Moon is within 1.7° + 0.6°/h × half the window of the antisolar point. The light multiplies the moonlight and the Moon-distance limit.

## Comets

`internal/comets`

MPC `CometEls.txt` ([format][mpc-format]): T, q, e, ω, Ω, i (J2000 ecliptic), H, K. Unperturbed two-body orbits, k = 0.01720209895 rad/day, Δt from perihelion in days; light-time ignored, T taken as UT.

**True anomaly ν and heliocentric distance r:**

- **Parabolic** (|e − 1| < 10⁻⁹): Barker's equation solved in closed form ([Meeus][meeus] ch. 34):

  ```math
  W = \frac{3k}{\sqrt{2q^3}}\,\Delta t ,\quad Y = \sqrt[3]{\tfrac{W}{2} + \sqrt{\tfrac{W^2}{4}+1}} ,\quad s = Y - \tfrac{1}{Y} = \tan\tfrac{\nu}{2} ,\quad r = q(1+s^2)
  ```

- **Elliptic**: Kepler's equation `E − e sin E = M` solved by Newton–Raphson, starting from E = π for e > 0.8 ([Meeus][meeus] ch. 30):

  ```math
  a = \frac{q}{1-e} ,\quad M = \frac{k}{a^{3/2}}\Delta t ,\quad
  \nu = 2\mathrm{atan2}\!\big(\sqrt{1+e}\sin\tfrac{E}{2},\ \sqrt{1-e}\cos\tfrac{E}{2}\big) ,\quad r = a(1 - e\cos E)
  ```

- **Hyperbolic**: `e sinh H − H = M` by Newton–Raphson:

  ```math
  a = \frac{q}{e-1} ,\quad \nu = 2\arctan\!\Big(\sqrt{\tfrac{e+1}{e-1}}\tanh\tfrac{H}{2}\Big) ,\quad r = a(e\cosh H - 1)
  ```

**Heliocentric ecliptic position**, `u = ω + ν` ([Meeus][meeus] ch. 33):

```math
x = r(\cos\Omega\cos u - \sin\Omega\sin u\cos i) ,\quad
y = r(\sin\Omega\cos u + \cos\Omega\sin u\cos i) ,\quad
z = r\sin u\sin i
```

**Earth** is the Sun formula reversed, with the general precession 1.3969713°/century subtracted from λ☉ since MPC's elements are J2000:

```math
R = 1.00014 - 0.01671\cos g - 0.00014\cos 2g ,\qquad (X_\oplus, Y_\oplus) = -R\,(\cos\lambda_\odot, \sin\lambda_\odot)
```

The Earth–comet vector is rotated by the J2000 obliquity 23.4392911° to J2000 RA/Dec and Δ, then precessed to date.

**Brightness** at mid-window; fainter than `-comet-mag` is dropped:

```math
m = H + 5\log_{10}\Delta + 2.5\,K\log_{10} r
```

MPC's H and K drift 1–4 mag from observation over an apparition, so the law is shifted by the median offset of [COBS][cobs] observations in the 30 days before the night, when a comet has at least three (`comets.FetchObserved`; description `(COBS)`). Keys are `10P`, a packed provisional designation, or a fragment's own (`73Pbt`). All pages of COBS's answer are read; one failing keeps the elements' magnitudes. Not in the browser (no CORS).

A comet has one position per minute (`Target.Track`), no size limits or framing, and is scored sky-limited.

## Atmosphere and sky

`internal/atmos`

### Airmass and extinction

**Airmass**: [Pickering (2002)][pickering], h in degrees:

```math
X(h) = \frac{1}{\sin\!\big(h + 244/(165 + 47\,h^{1.1})\big)}
```

**Transmission** relative to the zenith:

```math
T(h,\kappa) = 10^{-0.4\,\kappa\,(X(h)-1)}
```

**Extinction coefficient** at 550 nm:

```math
\kappa = \underbrace{0.1066\,e^{-z/7996\,\text{m}}}_{\text{Rayleigh}} + \underbrace{0.029}_{\text{ozone}} + \underbrace{1.086\cdot\text{AOD}_{550}}_{\text{aerosols}}
```

- **Rayleigh**: 0.098 optical depth at 0.55 µm ([Hayes & Latham 1975][hayes]; [Bucholtz 1995][bucholtz]), 8 km scale height.
- **Ozone**: Chappuis band, 300 DU.
- **Aerosols**: hourly [CAMS][cams] AOD via Open-Meteo, thinned by `e^(−Δz/1.5 km)` for the site's height above its cell's ground (`atmos.AerosolAbove`), taken from the ECMWF IFS 0.25° cell (`weather.GridElevation`). A site below its cell, or with the cell unknown, keeps the AOD. La Palma: κ 0.37 → 0.16, measured 0.13–0.15. 1.086 = 2.5 log₁₀ e.

The three parts are kept apart for the [filter bands](#nebula-filters). A fixed `-extinction` is split by `atmos.SplitExtinction`: ozone first, Rayleigh up to 0.1066, the rest aerosol. Past CAMS's range (about 5 days) AOD is 0.1 at sea level, thinned likewise. With no elevation from either forecast, `-extinction`'s value (default 0.2) is used.

### Sky brightness: Krisciunas & Schaefer (1991), moonlight after Winkler (2022)

B is in nanoLamberts ([K&S][ks]):

```math
B_{\text{nL}} = 34.08\, e^{\,20.7233 - 0.92104\,V} \qquad (V \text{ in mag/arcsec}^2)
```

**Zenith dark sky**: `-sqm`, else `-bortle`, else DarkSkySites (with a key), else 22.0.

| Bortle | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 |
|---|---|---|---|---|---|---|---|---|---|
| band (mag/arcsec²) | ≥ 21.96 | 21.7–21.96 | 21.3–21.7 | 20.8–21.3 | 20.3–20.8 | 19.6–20.3 | 18.9–19.6 | 18.0–18.9 | < 18.0 |
| `-bortle` value | 21.98 | 21.83 | 21.5 | 21.05 | 20.55 | 19.95 | 19.25 | 18.45 | 17.5 |

Bands are DarkSkySites' map's (its classifier, October 2026), values the bands' middles (class 9 half a class under 18.0). Other SQM–Bortle tables differ by up to a class ([Bortle 2001][bortle] defined the classes by eye).

<a id="airglow"></a>**Solar cycle** (`atmos.AirglowExcess`, `internal/solar`). Airglow scales with the 10.7 cm flux S as 1 + m(S − 129) ([Noll et al. 2012][noll]), and is 0.6 of the natural 22.0 ([Benn & Ellison 1998][benn]) whose flux is 90 sfu, solar minimum ([Falchi et al. 2016][falchi]). With m = 0.006:

```math
\Delta B = 0.6\,B_{22.0}\left(\frac{1 + 0.006\,(S - 129)}{1 + 0.006\,(90 - 129)} - 1\right)
```

S is clamped to 65–250 sfu. 0.38 mag brighter at 180 sfu, 0.14 darker at 65. S is NOAA SWPC's monthly mean for the night's UTC month, else within 30 days after the last daily value the 30-day mean ([Patat 2008][patat]), else NOAA's prediction (to 2030). Unknown flux, `-no-solar` or a measured `-sqm`: no change. Hour-of-night and seasonal airglow changes are not modelled.

**Towards the horizon**, K&S's scattering airmass X_s at Z = 90° − h:

```math
X_s(h) = \big(1 - 0.96\sin^2 Z\big)^{-1/2} ,\qquad
B_{\text{dark}}(h) = B_{\text{zen}}\cdot 10^{-0.4\,\kappa\,(X_s-1)}\cdot X_s
```

**Zodiacal light** (`atmos.ZodiacalExcess`). [Leinert et al. (1998)][leinert] Table 17 by ecliptic longitude from the Sun Δλ and latitude β, bilinear, the pole value past β = 75°, blanks near the Sun filled from the row's first tabulated latitude. The configured zenith is taken to include the value at (135°, 45°), a mid-latitude zenith. Heliocentric and seasonal factors are left out. Pristine sky: ecliptic 90° from the Sun +0.5 mag/arcsec², pole −0.06.

**Galactic light** (`atmos.GalacticExcess`). Pioneer 10's background starlight, [Leinert et al. (1998)][leinert] Tables 35 (440 nm) and 36 (640 nm), stars brighter than V 6.5 removed; G_V = blue^0.45 · red^0.55. The 28 cells of Pioneer's Sun gap are filled log-linearly along latitude; the ±80° rows' mean at the poles. The zenith is taken to include the table's solid-angle median, 81 S10. Pristine sky: galactic poles 0.3 mag/arcsec² darker, plane 0.5–1.1 brighter, Sagittarius 1.7. Positions are mid-window, comets too.

```math
B_{\text{zen}}(\text{target}) = \max\big(B_{\text{zen}} + Z(\Delta\lambda,\beta) - Z(135^\circ, 45^\circ) + G_V(l,b) - G_{\text{ref}},\ Z_{\text{pole}} + G_{\min}\big)
```

**Off the zenith** the excess E = B_zen(target) − B_zen comes from beyond the atmosphere, so it takes no X_s brightening and is dimmed by the whole column at the Pickering airmass:

```math
B_{\text{dark}}(h) = B_{\text{zen}}\cdot 10^{-0.4\,\kappa\,(X_s-1)}\cdot X_s + E\cdot 10^{-0.4\,\kappa\,X}
```

**Moonlight**, while the Moon is up, after [Winkler (2022)][winkler]. V flux outside the atmosphere from phase angle α (degrees, 0 = full), with K&S's magnitude law, mean distance scaling, and [thorsky][thorsky]'s opposition surge:

```math
m_{\text{moon}} = -12.73 + 0.026\,\alpha + 4\cdot10^{-9}\alpha^4 ,\qquad
F^* = 10^{-0.4\,m_{\text{moon}}}\cdot \Big(\frac{60.27}{r}\Big)^2 \cdot
\begin{cases} 1.35 - 0.05\,\alpha & \alpha < 7^\circ \\ 1 & \text{otherwise}\end{cases}
```

Single scattering in a layer of τ = 0.921 κ, the beam reaching each height through only the air above it, gives the gradation function:

```math
\Gamma = X\,\frac{e^{-\tau X} - e^{-\tau X_{\text{moon}}}}{X_{\text{moon}} - X} \qquad \big(\to \tau X e^{-\tau X}\ \text{as}\ X_{\text{moon}} \to X\big)
```

The normalised phase function mixes Rayleigh ([Bucholtz 1995][bucholtz], χ = 0.0148) and Henyey–Greenstein (g = 0.8) by the minute's κ_R and κ_A:

```math
p(\rho) = \frac{\kappa_R}{\kappa_R+\kappa_A}\,\frac{3(1-\chi)}{16\pi(1+2\chi)}\Big(\frac{1+3\chi}{1-\chi} + \cos^2\rho\Big) + \frac{\kappa_A}{\kappa_R+\kappa_A}\,\frac{1}{4\pi}\,\frac{1-g^2}{(1 + g^2 - 2g\cos\rho)^{3/2}}
```

```math
B_{\text{moon}} = 10^{\delta}\, F^*\, p(\rho)\, \Gamma \,/\, 4.2545\cdot10^{10}
```

ρ is the Moon–target angle; the divisor converts sr⁻¹ to arcsec⁻². δ = 0.078 is Winkler's V offset, fitted at Sutherland, whose albedo 0.74 it holds; B_moon is rescaled to the minute's (κ_R + κ_A)/κ (`moonAlbedo`). Full Moon 60° up, target 90° away at 60°: 18.48 mag/arcsec². Within about 10° of the Moon, up to a magnitude brighter than K&S. Clean-site g likely makes the sky too dark far from the Moon at κ ≳ 0.3 ([Andrews et al. 2006][andrews]).

```math
B = B_{\text{dark}} + B_{\text{moon}}
```

SKY is B in mag/arcsec² averaged over observable minutes. The target-independent part is `atmos.MoonLight`.

## Object surface brightness

`catalog.Target.SurfaceBrightness`

V surface brightness S, by the first rule that applies:

0. **Measured line flux** (`haflux`, `FluxSB`). 1522 planetaries over 14 lists from [Frew, Bojičić & Parker (2013)](https://cdsarc.cds.unistra.fr/viz-bin/cat/J/MNRAS/431/2) (`scripts/haflux.py`), and 675 emission nebulae of ≥ 30′ with no surface brightness, over 17 lists, from [Finkbeiner (2003)][finkbeiner] (`scripts/hamap.py`): mean over the major-axis disc less the median of a 1.25–2 radii ring. Another 343 stay unknown because the ring's median is ≥ 80% of the disc mean (`RING_MAX`). The Hα flux over its share gives every line by the type's ratios, each line's photons through V (`LineV`) over the ellipse, against V = 0. SUBS sets map fluxes aside for its class's typical value (`ExposureSB`, [below](#sub-exposure-estimate)). SHASSA's 32 Å filter passes both [N II] lines, so south of Dec +15° a map flux can read up to 0.33 mag bright.
1. **Measured V** (`surfbr`, the Compendium).
2. **Measured B** (`bsurfbr`, [OpenNGC][openngc], within the 25 mag isophote) less a colour: the object's own B − V within −0.3…1.5, else (`~`) typical by kind (`TestTypicalBV`):

   | Kind | typical B − V |
   |---|---|
   | galaxies, galaxy groups | 0.8 (median offset of Compendium V vs OpenNGC B over 723 shared galaxies) |
   | planetary nebulae | 0.7 |
   | other emission-line objects | 0 |
   | everything else | 0.5 |

3. **Derived** (`~`), single galaxies and nebulae only: integrated V (or B less the typical colour) over a uniform ellipse, in arcminutes, a disc of the major axis without a minor. `scripts/galaxyv.py` replaces a galaxy's V more than 0.3 fainter than its B (aperture value) or 1.5 brighter (not a total) with the B.

   ```math
   S = m + 2.5\log_{10}\!\Big(\frac{\pi}{4}\,(60\cdot\text{major})(60\cdot\text{minor})\Big)
   ```

4. **Unknown** (S = 0), scored sky-limited:
   - star clusters, whose light is in points and whose faint members are what a night changes;
   - galaxy groups (one member's magnitude, the group's size);
   - clusters with nebulosity (the stars' magnitude);
   - nebulae whose catalogued magnitude is a star's or cluster's (`starMag`: reflection, Wolf-Rayet and variable types; `starMagNames`);
   - comets and objects without data.

`catalog.Target.kind()` maps the lists' type spellings to galaxy, group, cluster, cluster+nebula, nebula or other.

## Weather

`internal/weather`, `scoring.BuildSky`

Linear **ramp**, 1 at `good`, `floor` at `bad`:

```math
\text{ramp}(v;\ \text{good},\text{bad},\text{floor}) = 1 - \text{clamp}_{[0,1]}\!\Big(\frac{v-\text{good}}{\text{bad}-\text{good}}\Big)(1-\text{floor})
```

**Effective cloud** (Open-Meteo), layers overlapping randomly, high cirrus at half:

```math
C = 1 - (1 - c_\text{low})(1 - c_\text{mid})(1 - c_\text{high}/2)
```

```math
Q = (1 - C)\cdot
\underbrace{\text{ramp}(T - T_d;\,4,1,0.7)}_{\text{dew risk}}\cdot
\underbrace{\text{ramp}(\text{gust};\,20,40,0.5)}_{\text{km/h}}\cdot
\underbrace{\text{ramp}(\text{precip};\,0,0.1,0)}_{\text{mm/h}}\cdot
\underbrace{\text{ramp}(\text{transp};\,1,8,0.5)}_{\text{7Timer}}
```

- **Gust** counts at most 3× the hour's mean wind, a cap never below 20 km/h (`Gust`); mountain grid points forecast 5–10×.
- **Rain**: unknown does nothing. Open-Meteo's gusts and precipitation are over the preceding hour, so each hour reads the next entry. Cloud, temperature, dew, wind and seeing come from the nearest hour.
- **Transparency**: [7Timer!][7timer] (3-hourly, about 3 days), only for hours without CAMS AOD, whose haze the extinction already charges.

Uncovered minutes get the mean of the covered ones; no forecast is Q = 1. Weather does not affect UP or the observable runs.

**Washout** (`Sky.Washout`). A minute is usable at Q ≥ 0.2 (`MinQuality`) with zenith seeing < 4″ (`MaxSeeing`), or outside the weather forecast (`Sky.Covered`). No 60 usable minutes in a row (`MinUsable`), or a shorter window not usable throughout: the text report replaces the table and plan with the reason. `-json`/`-csv` still list targets, with `sky.washout`. Only the upper-air estimate can reach 4″.

## Scoring a target

`internal/scoring`

### Which targets are scored

Dropped first: major axis outside `-size-min`…`-size-max` (unknown counts as 0; comets exempt), outside the `-ra`/`-dec` box (±`-tol` per axis, RA at 15°/h, wrapping, no cos δ), named in `-skip`, or of no `-type` class (`catalog.Target.Is`).

### Is this minute observable?

Rules from [uptonight][uptonight], all required:

1. `-alt-min ≤ h ≤ -alt-max` (30°–80° by default).
2. Above the horizon profile at the target's azimuth.
3. **Field rotation** (`-mount altaz`, framing, not comets). The field turns at ω cos φ cos A / cos h (ω the sidereal rate, [Meeus][meeus] ch. 14), with cos h cos A = sin δ cos φ − cos δ sin φ cos H. In one sub of t seconds, the edge r pixels out (half the major axis, at most the frame's half-diagonal; sizeless objects unchecked) moves rate × t × r, which must not exceed a star's FWHM: seeing × X^0.6, diffraction 1.03 λ/D and 0.68 of a pixel in quadrature (the pixels' alone without `-aperture` or `-scope`).
4. **Moon distance**, while the Moon is up: at least `illumination %` degrees, × a filter's `moonK` ([below](#nebula-filters)), × the eclipse light.

The longest unbroken run is OBSERVABLE; shorter than `-min-run`, the target is dropped.

### The sky-glow weight

Every target, clusters included, takes the whole sky. SNR at fixed exposure:

```math
\text{SNR} \propto \frac{S\,t}{\sqrt{(S + B)\,t}}
```

Against the same object under a pristine 22.0 sky B_ref, in nanoLamberts:

```math
W_{\text{sky}} = \min\!\Bigg(1,\ \sqrt{\frac{S + B_{\text{ref}}}{S + B}}\Bigg)
```

S ≫ B gives W ≈ 1; S ≪ B or unknown S gives `√(B_ref / B)`.

### Nebula filters

`-filter dual|tri|quad|ha|oiii|sii` (`scoring.passes`, `atmos.SkyParts`, `atmos.BroadSky`, `catalog.Target.Lines`) applies to emission-line targets: emission, planetary and Wolf-Rayet nebulae, remnants, and named exceptions such as IC 1396. Bands are `-filter-nm` wide (7 nm by default), or a `-scope` built-in's width per line (`config.Config.BandNM`). Overlapping bands are clipped at the lines' midpoint (`scoring.lineBands`), so shared sky is charged once.

<a id="filters-in-a-fast-beam"></a>**Filters in a fast beam** (`-filter-std`, `scoring.beam`). A filter tilted by θ passes λ·√(1 − sin²θ/n²). For focal ratio N, tan θ = r/2N across the pupil, r from the obstruction ε to 1. A line in a top-hat band [lo, hi] passes while n²(1 − (λ/lo)²) ≤ sin²θ ≤ n²(1 − (λ/hi)²), and its share is the pupil area between:

```math
t = \frac{\min(1, r^2_{\text{hi}}) - \max(\varepsilon^2, r^2_{\text{lo}})}{1-\varepsilon^2},\qquad r^2 = \frac{\sin^2\theta}{1-\sin^2\theta}\,(2N)^2
```

n = 2.05 (`filterN`) is fitted to [Chroma's curves][chroma] of 3, 5, 8 nm filters at f/2–f/5 (`scripts/filter_shift.py`, 55 curves, rms 0.06). The sky stays the band's. Line transmission at f/2.2, 77 mm obstruction:

| Band | Hα 656.3 | [N II] 654.8 | [N II] 658.3 | [O III] 500.7 |
|---|---|---|---|---|
| 3 nm | 0.16 | 0.69 | 0 | 0.33 |
| 7 nm | 0.87 | 1 | 0.16 | 1 |

IC 1795 at Bortle 6, 14 October 2026, Origin, Hα 7 nm: 348 subs unfiltered, 136 if made for the beam, 214 standard. Broadband edges are left alone.

**The sky in a band.** Glow (`B_dark`: airglow, light pollution, zodiacal, galactic), Rayleigh moonlight (λ⁻⁴) and aerosol moonlight (λ^−1.3, [Ångström][angstrom]). The V sky ([Bessell 1990][bessell]) stands for the 400–700 nm camera band (`atmos.CameraLo`–`CameraHi`). A band Δλ at λ passes:

```math
f_{\text{glow}} = \frac{\Delta\lambda}{300},\qquad
f_{\text{R}} = \frac{\Delta\lambda}{300}\,\frac{(\lambda/551)^{-4}}{\langle(\lambda/551)^{-4}\rangle_{400-700}},\qquad
f_{\text{M}} = \frac{\Delta\lambda}{300}\,\frac{(\lambda/551)^{-1.3}}{\langle(\lambda/551)^{-1.3}\rangle_{400-700}}
```

Per nm, [O III] gets 2.9× the Rayleigh moonlight of Hα. Moonlight is flat per nm before scattering ([Thejll et al. 2014][moon-colour] for the Moon's colour). Glow is flat (white LEDs, the conservative case).

**The target's light.** Typical line ratios to Hα per type (`catalog.Target.Lines`): Hβ = Hα/2.86 (case B, [Osterbrock & Ferland 2006][agn2]), [N II] 1:3 over 654.8/658.3, [O III] 495.9 a third of 500.7, normalised. The table's shares are energy; scoring counts photons (`catalog.Target.Photons`, each share × λ, renormalised).

| Type | Hα | [O III] 500.7 | [S II] 671.6+673.1 | [N II] 654.8+658.3 | Basis | shares Hα, [O III] (both lines), [S II], Hβ, [N II] |
|---|---|---|---|---|---|---|
| H II region (and every other emission-line target) | 1 | 0.18 | 0.19 | 0.36 | the average of 13 classical O-star H II regions in WHAM's 1° beam ([Madsen, Reynolds & Haffner 2006][madsen]): [N II] 658.3 0.27 and [S II] 671.6 0.11, one line of each doublet, so the doublets are 0.27 × 4/3 and 0.11 × (1 + 1/1.45), 671.6/673.1 at low density; [O III] scatters most | 0.47, 0.11, 0.09, 0.16, 0.17 |
| planetary nebula | 1 | 2.8 | 0.05 | 1 | [O III]/Hβ 6–9 (Magellanic Cloud averages), Hα = 2.86 Hβ; over 613 planetaries log Hα/[S II] peaks at 1.2 and log Hα/[N II] at 0 ([Riesgo & López 2006][riesgo]) | 0.16, 0.61, 0.01, 0.06, 0.16 |
| supernova remnant | 1 | 0.5 | 0.6 | 0.8 | [S II]/Hα > 0.4 ([Mathewson & Clarke 1973][mathewson]) and [N II] 658.3/Hα > 0.5 ([Fesen, Blair & Kirshner 1985][fbk]) define them; [O III] varies | 0.29, 0.20, 0.18, 0.10, 0.23 |
| Wolf-Rayet nebula | 1 | 1 | 0.2 | 0.6 | NGC 6888's dereddened slits ([Mesa-Delgado et al. 2014][mesa]): [O III] about Hα, the N-enriched ejecta's [N II] 0.4–0.9 of it | 0.29, 0.38, 0.06, 0.10, 0.17 |

w is the photon share in the filter's bands, every line a band reaches: a 7 nm Hα band (652.8–659.8) passes both [N II] lines; a 30 nm [O III] band reaches Hβ. Sky-limited gain under glow alone, w²/f_glow, 7 nm bands:

| Target | `ha` | `oiii` | `sii` | `dual` | `tri` | `quad` |
|---|---|---|---|---|---|---|
| H II region | 19.9 | 0.2 | 0.4 | 12.1 | 11.1 | 10.2 |
| planetary nebula | 6.5 | 7.4 | 0.0 | 13.8 | 10.4 | 8.0 |
| supernova remnant | 13.7 | 0.6 | 1.6 | 10.1 | 8.4 | 9.9 |
| Wolf-Rayet nebula | 11.6 | 2.6 | 0.2 | 12.6 | 10.4 | 9.0 |

**Against the camera band.** A line is collected once while the camera holds `atmos.CameraBand` = 300/88 V bands of sky, so an emission-line target's S is divided by that in the score; a continuum's is unchanged. S itself is its lines ([below](#emission-line-light)).

**The weight:**

```math
W_{\text{filter}} = \min\!\Bigg(1,\ \sqrt{\frac{w^2\,(S + B_{\text{ref}})}{w\,S + f_{\text{glow}}B_{\text{glow}} + f_{\text{R}}B_{\text{R}} + f_{\text{M}}B_{\text{M}}}}\Bigg)
```

With w = 1 and f = 1 this is the broadband weight.

**Extinction in the bands:**

```math
\kappa_\lambda = \kappa_{\text{R}}\Big(\frac{\lambda}{551}\Big)^{-4} + \kappa_{\text{O}_3} + \kappa_{\text{aer}}\Big(\frac{\lambda}{551}\Big)^{-1.3}
```

Ozone is held at 550 nm. A multi-band filter takes the mean weighted by light passed. κ = 0.2 gives 0.13 at Hα and 0.26 at [O III]. Unfiltered, an emission-line target is dimmed at its lines' own coefficient, so no filter wins on extinction alone.

**A list of filters.** Each choice that passes some of the target's light is scored as worn all night, in one pass over the grid. FILTER (`Result.Filter`) is the one that gains most, no filter on a tie:

- without a camera model: the largest uncapped weight, Σ Q·T·√(SNR² ratio);
- with framing and an `ExposureSB`: the fewest subs for the good level, over the choice's own observable minutes (`subsGain` = m(1 − L²/C), [below](#sub-exposure-estimate)).

The capped weight would saturate at 1 for every helpful filter under a dark sky, and the total Σ Q·SNR²_sub would reward a filter for the extra moonlit minutes its Moon limit admits.

**The Moon limit** scales by `moonK` = f_R / w², between 0.3 (`minMoonK`: halo and gradients near the Moon) and 1. At full Moon (100°), 7 nm bands:

| Target | `ha` | `oiii` | `sii` | `dual` | `tri` | `quad` |
|---|---|---|---|---|---|---|
| H II region | 30° | 100° (no gain) | 85° | 30° | 30° | 30° |
| planetary nebula | 30° | 30° | 100° | 30° | 30° | 30° |
| supernova remnant | 30° | 100° (no gain) | 30° | 30° | 30° | 30° |
| Wolf-Rayet nebula | 30° | 43° | 100° | 30° | 30° | 30° |

### Broadband filters

`-filter cls|lpro` (`config.Broadband`, `scoring.broadPasses`):

| Choice | Passbands (nm) | Stands for | f_glow | f_R | f_M |
|---|---|---|---|---|---|
| `cls` | 450–520, 640–690 | Astronomik CLS (published edges); Optolong CLS and IDAS LPS-D2 are close | 0.40 | 0.36 | 0.39 |
| `lpro` | 400–430, 440–540, 555–575, 625–700 | Optolong L-Pro; IDAS LPS-D1/P2/D3 and Baader's Moon & Skyglow are alike | 0.75 | 0.80 | 0.77 |

`lpro`'s edges are read off Optolong's reference curve (±5 nm). f_glow is the passbands' share of 300 nm; f_R and f_M weight it by `∫(λ/551)⁻⁴ dλ` and λ⁻¹·³. Under flat (LED) glow, gain against sodium or mercury is not modelled.

- Emission-line target: w = 1 (all lines pass); sky-limited gain 1/f_glow, 2.5 for `cls`, 1.33 for `lpro` (`dual`: 12.1 for an H II region).
- Continuum (galaxies, clusters, reflection nebulae, comets): w = f_glow, gain f_glow < 1, never chosen.

Extinction is the band mean (continuum) or the passed lines' (`moonK` as above: 36° and 80° at full Moon for emission-line targets, unchanged for continuum).

### Putting it together

```math
w_i = Q_i \cdot T(h_i,\kappa_{\lambda,i})\, W_i ,\qquad
\text{SCORE} = F_{\text{frame}}\cdot\frac{1}{N}\sum_i w_i\ \ \text{for the choice that gains most}
```

Each choice is summed over its own observable minutes, with κ_λ the band's, or unfiltered V's (continuum) or the lines' (emission-line). Sorted by SCORE; mean altitude breaks ties. Extinction does not also dim S.

<a id="emission-line-light"></a>**Emission-line light from V** (`scoring.lightNL`). V measures the lines only in part and includes stars and continuum:

```math
S_{\text{lines}} = \frac{S_V}{f_V}\,10^{-0.4\,c},\qquad f_V = \sum_\ell p_\ell\,T_V(\lambda_\ell)
```

p_ℓ are the photon shares, T_V Bessell's V at each line (`catalog.LineV`: Hα 0.032, [O III] 0.481 and 0.337, [S II] 0.016, Hβ 0.111, [N II] 0.035 and 0.028). c = 1.16 mag (`vContinuum`) is the median over 13 emission nebulae ≥ 30′ against [Finkbeiner (2003)][finkbeiner]'s Hα map (`scripts/calibrate_lines.py`), interquartile 0.19–2.45 mag, so one nebula is good to a factor of three to five. Lines are brighter than V by 1.61 mag (H II), 1.33 (remnant), 0.74 (Wolf-Rayet) and 0.29 (planetary). A `FluxSB` has no stars and is undone exactly, without c. SB shows V.

## Framing and mosaics

`scoring.frame`, `internal/config`

Enabled by `-scope`, `-fov`, `-scale`, or `-focal` with `-sensor`/`-pixel`/`-camera`; unset values are the scope's, else the Origin Mark II's.

```math
\text{FOV}_{\text{side}} = 2\arctan\frac{d}{2f}\ \ \text{(in arcmin)} ,\qquad
\text{scale} = 206.265\,\frac{p}{f}\ \ \text{arcsec/px}
```

**Size limits.** Minimum `-min-px × scale / 60` arcminutes; maximum the frame's short side (the position angle is unknown), the largest grid's short side with `-mosaic`, its long side with `-rotate`. Explicit `-size-min`/`-size-max` win.

**Cores** (`catalog.Target.AtCore`, `scoring.framable`). When a whole object fails but its usual core passes, it is framed at the core (hand table of eleven, centred on SIMBAD's parts or plate-solved frames). Position and framing axes change; size, magnitude and surface brightness remain the whole's.

**Fill and score.** A c × r grid at 10% overlap:

```math
W = \text{FOV}_\text{long}\,\big(c - 0.1(c-1)\big) ,\qquad H = \text{FOV}_\text{short}\,\big(r - 0.1(r-1)\big)
```

Fill is `major / min(W, H)`; with `-rotate`:

```math
\text{fill} = \min\big(\max(\tfrac{\text{major}}{W}, \tfrac{\text{minor}}{H}),\ \max(\tfrac{\text{major}}{H}, \tfrac{\text{minor}}{W})\big)
```

```math
F_{\text{fill}}(f) =
\begin{cases}
f / 0.25 & f < 0.25 \\
1 & 0.25 \le f \le 0.8 \\
\max(0, (1 - f)/0.2) & f > 0.8
\end{cases}
\qquad
F_{\text{frame}} = \max_{\text{grids}}\ \frac{F_{\text{fill}}}{\text{panels}}
```

Fill ≥ 1 is dropped. The fewest panels win a tie. Unknown size gets F = 1.

**Sampling** (`Sky.Sampling`, display only). Star FWHM F = the window's mean zenith seeing (else 2.5″), in quadrature with diffraction 1.03 λ/D when the aperture is known (`ApertureSet`). F over the pixel scale, rounded to 0.1 as shown: < 1 severely undersampled, < 2 undersampled, 2–3 well sampled, up to 5 oversampled, above that severely oversampled.

**STAR** (`internal/stars`). The brightest of 517 Yale BSC stars (V ≤ 4.0, CDS V/50, T CrB left out; J2000, no proper motion) and the planets reaching V 4, within the frame's or grid's half-diagonal, without framing the Origin Mark II's. A warning only.

Planets (`stars.Planets`) at mid-window from [Standish][jpl-approx] Table 1, less the Earth-Moon barycentre, no light time. `TestPlanetsSOFA` against Plan94: 0.7′ Mercury, 1.4′ Venus, 2.7′ Mars, 10.5′ Jupiter, 11.1′ Saturn. Magnitudes: [Meeus][meeus] ch. 41, Saturn's rings −2.60 sin|B| + 1.25 sin²B, its ΔU term left out.

## Sub-exposure estimate

`scoring.Subs`, `catalog.Target.ExposureSB`, `internal/config`

With framing, each listed target gets the subs for stacked SNR **5, 10, 20** per arcsec² at its mean surface brightness (× √`levelScale` for an object fainter than a pristine sky), through its FILTER. SUBS shows the good level.

**Camera.** Defaults are the Origin Mark II's or `-scope`'s, each overridden by its flag: D = 152 mm, d = 77 mm, IMX678 QE 0.83, read noise 0.97 e⁻, 10 s, RGGB unless `-mono`. The Origin's read noises are upper bounds from its saved bias masters (Mark II 3·29.0·0.0112 = 0.97 e⁻; original 3·30.4·0.0165 = 1.5 e⁻). Framing without `-scope` has no estimate without `-aperture` and `-camera` (or `-mono`, `-qe`, `-read-noise`); the Frame line names what is missing.

**Throughput** (`-throughput`), by design: refractor 0.88 (about ten coated surfaces and the IR-cut window), prime-focus mirror 0.81 (the Origins, Unistellar), other 0.85.

**Dark current** (`-dark`, `-sensor-temp`). Per chip at a 25 °C sensor from ZWO's charts: IMX571 0.065, IMX533 0.063, IMX676 0.060, IMX585 0.25, IMX294/IMX492 0.37, IMX178 0.95 e⁻/s; KAF-16803 3 (onsemi), KAF-16200 1.7 (Moravian's 0.08 at 0 °C, extrapolated). Others take the CMOS median 0.16 (`typicalDark`, `TestTypicalDark`). Doubling every 5 °C. Uncooled sensor: air + 10 °C (forecast; 10 °C air without one). Not modelled: amplifier glow, hot pixels, master dark noise.

<a id="smart-telescopes"></a>**Smart telescopes** (`config.Scopes`). 21 presets with aperture, focal length, obstruction, saved frame, sub length, mount and built-in filter. A scope without a sensor preset (OmniVision's, IMX347, IMX415) or a published obstruction (Unistellar, Draco) needs `-qe`, `-read-noise` (and `-obstruction`) for SUBS. `TestScopes` checks frames against the makers' stated fields within 3%, except the eVscope and Vespera 3 / Pro 2.

<a id="camera-presets"></a>**Camera presets** (`config.Cameras`). 45 chips: pixel size, resolution, maker's peak QE and read noise at its deep-sky gain, for colour and mono versions. Per-chip sources and exceptions are commented in `internal/config/cameras.go`.

- **Colour** `Response`: red, green, blue relative response per line and over 400–700 nm (`Broad`), digitized from the maker's QE chart by `scripts/qe_digitize.py`, relative to the chart's peak over its whole range.
- **Mono** `MonoResponse`: the maker's chart, else a relative's; `sonyMono` (the IMX585's STARVIS 2 curve: Hα 0.88, continuum 0.95) is the default and for `-mono` without `-camera`.
- **Sony "relative response" charts are per watt**, as reproduced by most makers: read as charted they overstate Hα against [O III] by 656/501 = 1.31×. `qe_digitize.py --per-watt` divides by λ. Verified against Allied Vision's absolute curves, ZWO's QE charts and FLIR's EMVA 1288 ratios. Other charts are per photon, or read as charted where unlabelled (IMX678, IMX485, SC2210, MN34230).
- `os08b10` is the least certain: OmniVision's Nyxel 2 chart, attributed to the chip by a ZWO user group slide, and Cuiv's 0.84 e⁻ at gain 150.
- A version is left out where no astro camera ships it or its peak QE is unpublished. IMX185 has no preset.

At equal QE, read noise and pixels, the response alone moves an emission nebula's SUBS by up to 2× across the presets.

**Photon rate.** V = 0 gives 1005 photons s⁻¹ cm⁻² Å⁻¹ at 550 nm (Bessell 1998), `atmos.PhotonsV0` over 88 nm. Per pixel:

```math
R = \frac{884\,400\;x}{\mathrm{NL}(0)}\;\frac{\pi}{4}\left(D^2 - d^2\right)\;\tau\;QE_\mathrm{peak}\;q\;\Omega ,\qquad \Omega = \mathrm{scale}^2
```

with τ the throughput and q by source:

- **Sky and continuum**, unfiltered or broadband: spread over 400–700 nm (× 300/88 against V) at each channel's `Response.Broad`, or the mono's `Broad` (`sonyMono` 0.95). Moonlight per nm by `BandMean`: Rayleigh 1.30, Mie 1.04 of V's.
- **Through a line filter**: each band's sky (`BandSky`, against V) and the object's lines on every channel at that dye's response. IMX678, from ZWO's ASI678MC manual:

| Line | red | green | blue |
|---|---|---|---|
| Hβ 486.1 | 0.04 | 0.94 | 0.79 |
| [O III] 500.7 (+ 495.9) | 0.05 | 0.97 | 0.53 |
| Hα 656.3, [N II] | 0.77 | 0.34 | 0.11 |
| [S II] 671.6/673.1 | 0.72 | 0.39 | 0.12 |

- **An emission-line object** uses the line response always, also unfiltered and broadband, since a band collects each line once.

Object and sky per minute are the score's, in absolute photons: the object is not moved against the camera band, and is dimmed by the zenith's extinction too (`atmos.Absolute`), as it is catalogued above the atmosphere and the sky measured on the ground.

**Per sub.** Channels c (¼, ½, ¼ for RGGB; one for mono) with share f_c, rates S_c, B_c, dark D, read noise RN:

```math
\mathrm{SNR}^2_\mathrm{sub} = \sum_c f_c\,\frac{(S_c t)^2}{S_c t + B_c t + D t + RN^2}\;\frac{1}{\Omega}\quad\text{per arcsec}^2
```

`TestSubsBayerChannels`: through Hα colour IMX678 needs 3.7× mono's subs read-noise limited, 2.3× sky limited.

**Stacking** (`-stack`). Each minute is weighted by its quality q_i. Per sub, over n observable minutes, with σ² the denominator above:

```math
m = \frac{1}{n\,\Omega}\sum_i q_i \sum_c f_c \frac{S_{c,i}^2}{\sigma_{c,i}^2}\quad(\text{weighted}),\qquad m = \frac{1}{n\,\Omega}\sum_c f_c \frac{\left(\sum_i q_i S_{c,i}\right)^2}{\sum_i q_i\,\sigma_{c,i}^2}\quad(\text{mean})
```

**Ceiling** (`-residual`, ε = 0.2%). Flats and gradients leave εB, which averaging does not remove:

```math
\mathrm{SNR}^2_\mathrm{max} = \sum_c f_c \left(\frac{\sum_i q_i S_{c,i}}{\varepsilon \sum_i q_i B_{c,i}}\right)^2 ,\qquad \frac{1}{L^2} = \frac{1}{N_L\,m} + \frac{1}{\mathrm{SNR}^2_\mathrm{max}} \;\Rightarrow\; N_L = \left\lceil \frac{L^2/m}{1 - L^2/\mathrm{SNR}^2_\mathrm{max}} \right\rceil ,\qquad L = 5,\ 10,\ 20
```

A level at or past the ceiling is `never` (JSON `snrLimit`). Counts cap at 10⁹ (`maxSubs`). FILTER uses the same N (`TestScoreFilterCameraModes`).

**Blur** (`camera.blur`). FWHM F: seeing ([upper-air estimate](#seeing-estimate), else 7Timer's class, else 2.5″) × X^0.6, diffraction 1.03 λ/D at 550 nm and 0.68 of a pixel, in quadrature. An ellipse of axes a × b keeps ab/√((a² + F²)(b² + F²)). Only objects under about an arcminute move.

**Subs that fit** (`Estimate.Max`). Observable time (with `-plan`, within its blocks) ÷ (sub + 1 s) × (1 − `-reject`), shared among mosaic panels.

**Surface brightness** (`ExposureSB`). The object's own (a non-planetary's map flux set aside: `levelScale` was fitted to catalogued values), else `typicalSB` by class (`TestTypicalSB`, marked `~`): galaxies 22.52, planetaries 21.10, emission nebulae 21.90, other nebulae 20.73, reflection nebulae 22.0 (a guess). None for clusters, galaxy groups, comets, and dark nebulae unless imaged for the emission behind them.

**Fainter objects' levels** (`scoring.levelScale`). A fixed SNR grows as 1/S², but reported totals grow about as 1/S, so below B_ref (in the score's terms) the levels are √(S/B_ref) of 5, 10, 20; JSON's `snrPerSub` and `snrLimit` are against them.

Check against reported totals, per panel, under 21.3 mag/arcsec² at 45.8° N, new Moon, no weather, 10 s, `-mosaic 8`, dual-band for emission nebulae (Seestar S50 users' at AstroBin and Cloudy Nights, 2026):

| Target | Seestar S50 | Origin | reported |
|---|---|---|---|
| M 42 | 2.5 h | 0.3 h | 0.5–2 h; Origin 10 min – 2 h |
| M 51 | 3.7 h | 0.5 h | 1.5–3 h; Origin about 35 min |
| M 81 | 6.1 h | 0.8 h | 3–5 h |
| M 101 | 22 h | 2.7 h | 1.6–9 h |
| M 33 | 15 h | 2.0 h | 3–6 h |
| NGC 7000 | 4.6 h | 0.6 h | 2–5 h |
| Veil, NGC 6992 | 4.0 h | 0.5 h | 1–3 h |
| Heart, IC 1805 | 2.3 h | 0.3 h | 2–9.5 h |
| Bubble, NGC 7635 | 2.3 h | 0.3 h | 2–4 h |

SUBS is a clean image's count: the Origin lands a little under its users' reports, the S50 at 1–5×, most for faint galaxies.

**FILTER at the Origin's 10 s colour subs.** IC 1795, 45.8° N, 14 October 2026, `-no-weather -no-solar`, good-level subs:

| Sky | none | `ha` | `dual` | `cls` | chosen |
|---|---|---|---|---|---|
| default 22.0 | 120 | 120 | 107 | 87 | `cls` |
| Bortle 4 | 176 | 126 | 112 | 110 | `cls` |
| Bortle 8 | 1480 | 182 | 198 | 507 | `ha` |

Bortle 5–7 choose `dual`. At Bortle 9 no filter tops out at SNR 8.5 and Hα needs 265 subs.

**Limits.** One nebula's lines are good to a factor of three to five. Not modelled: master dark noise, hot pixels, amplifier glow, saturation, per-rig residual, per-dye moonlight colour. One sensor temperature offset for all smart telescopes; flat spectra across each band.

## Seeing estimate

`weather.Seeing`. Never changes a score; SUBS' blur and the SEEING column use it.

FWHM from the ECMWF IFS 0.25° forecast (Open-Meteo, 15 days): T, wind and height at the surface and 925–100 hPa.

**Cn² per layer**, adjacent levels at least 300 m apart ([Dewan et al. 1993][dewan]):

```math
C_n^2 = 2.8\,M^2\,L_0^{4/3} ,\qquad
M = -79\cdot10^{-6}\,\frac{P}{T^2}\Big(\frac{dT}{dz} + 9.8\ \text{K/km}\Big)
```

```math
L_0^{4/3} = 0.1^{4/3}\cdot 10^{Y} ,\qquad
Y = \begin{cases} 1.64 + 42\,S & \text{troposphere} \\ 0.506 + 50\,S & \text{stratosphere}\end{cases}
```

M uses P in hPa and T in K; S is the vector wind shear in 1/s.

**Tropopause**, WMO lapse-rate rule ([AMS][ams-tropopause]) from 500 hPa up: the lowest level whose layer cools at ≤ 2 K/km and whose next layer does too. Above it, stratospheric Y. None found: all troposphere.

**Sanity checks** ([hypsometric equation][ams-hypsometric], R/g = 29.3 m/K):

```math
\Delta z = 29.3\ \tfrac{\text{m}}{\text{K}}\cdot\bar T\cdot\ln\frac{p_1}{p_2}\quad(\pm 30\%) ,\qquad
p_\text{surface} \approx 1013.25\,e^{-z/H},\ H = 29.3\ \tfrac{\text{m}}{\text{K}}\cdot\bar T\quad(\pm 12\%)
```

A failing or implausible level is dropped. An hour needs the surface, at least three pressure levels and one at ≤ 300 hPa.

**Cn² to FWHM**, λ = 500 nm, zenith ([Fried 1966][fried]; [Roddier 1981][roddier]); outside 0.1–30″ or non-finite is unknown:

```math
r_0 = \Big(0.423\,k^2\!\int C_n^2\,dz\Big)^{-3/5},\quad k = \frac{2\pi}{\lambda} ,\qquad
\text{FWHM} = 0.98\,\frac{\lambda}{r_0}
```

**Calibration.** Dewan extended to the surface overestimates the boundary layer ([Cuevas et al. 2024][cuevas]) and coarse levels smooth the free-atmosphere shear:

```math
\int C_n^2\,dz \;\approx\; 0.2\,C_{n,\text{ground}}^2\,\Delta z_\text{ground} + 1.45\sum_\text{above} C_n^2\,\Delta z
```

Least-squares fit (`scripts/calibrate_seeing.py`) over 12,222 hours of ESO DIMM at Paranal and La Silla against Open-Meteo's archived IFS runs (May 2024 to September 2026):

| | bias | RMSE | r |
|---|---|---|---|
| uncalibrated | +0.21″ | 0.49″ | 0.32 |
| calibrated | +0.03″ | 0.33″ | 0.41 |

Published forecasts for Paranal: [Cuevas et al. 2024][cuevas] 0.30″, r 0.41; [Osborn & Sarazin 2018][osborn] 0.31″, r 0.30. Refit only after a model change.

## Night plan

`internal/plan`, with `-plan`

Blocks of `-plan` length, at most one target each, each target at most once. A block's value is the sum of the target's weights in it × frame score.

1. **Greedy**: blocks in time order, best unused target; mean altitude breaks ties.
2. **Local search** on the target × block matrix until nothing improves: swap two blocks, move a target to another block and refill, replace with a better unused target.

The reported block score is that sum divided by the block's length in minutes.

## Constellations

`internal/constellation`

Always recomputed, following [Roman (1987)][roman]. J2000 positions are precessed to B1875.0 (`T = (2405889.258550475 − 2451545)/36525`), the IAU boundaries' equinox ([VI/49][vi49]). A ray due north counts constant-declination edge crossings; odd is inside. Ursa Minor winds around the pole, so its count is inverted. Boundary table from [astrogo](https://github.com/TuSKan/astrogo).

## Local horizon

`internal/horizon`

(azimuth, altitude) points, linear in azimuth, wrapping at north; two points at one azimuth make a vertical step.

```math
h_\text{hor}(A) = h_\text{lo} + (h_\text{hi} - h_\text{lo})\,\frac{(A - A_\text{lo}) \bmod 360}{\text{span}}
```

**`scripts/terrain2horizon.py`**. AWS Terrain Tiles, bilinear, stepped from `--near` (100 m) to `--radius` (20 km), curvature less refraction (k = 0.13); z₀ the site's model elevation (or `--elevation`) + 2 m eye height:

```math
h_\text{hor}(A) = \max_d \arctan\frac{z(d) - z_0 - d^2 (1 - k) / 2R}{d}
```

OpenStreetMap buildings (and with `--no-canopy` trees and woods) within `--osm-radius` (300 m) via Overpass; `height` tag, else `building:levels` × 3 + 2 m, else 8 m building, 12 m tree, 20 m wood. Each crossing at distance d, flat roofs, an outline holding the site left out:

```math
h_\text{obs}(A) = \max \arctan\frac{z(d) - z_\text{site} + h - 2\ \text{m}}{d}
```

Trees by default from the Meta/WRI canopy height map ([Tolan et al. 2024][chm]): 1.19 m pixels, about 2.8 m MAE, 2009–2020 imagery, read by HTTP range from BigTIFF strips, marched from `--canopy-near` (3 m) to `--canopy-radius` (300 m), pixels of at least 1 m.

## Heuristics

The project's own judgment, not measurements:

| Heuristic | Value | Reasoning |
|---|---|---|
| Moon-distance rule | ≥ illumination % in degrees (filtered: × f_R/w², 0.3–1) | uptonight's rule; skipped with the Moon down; the floor is the halo and gradients near the Moon, which no scattering model here holds |
| A filter for the whole night | each choice scored as worn every minute, the one that gains most kept (the largest uncapped weight; with a camera, the fewest subs by the sub-exposure model over its own observable minutes) | a filter changed mid-target means a second set of flats and a split stack |
| Moon in the umbra | 10⁻⁴ of its light, flat; the Moon's disc uniformly bright, the Sun's limb darkening left out | a bright totality (Danjon L = 4); darker ones change nothing the score sees, moonlight 10 mag down being far under any sky; the Moon-distance limit scaled linearly with the light, as the rule is with the illumination |
| Bright-star reach (STAR) | V ≤ 4.0, stars and planets, within the frame's half-diagonal (without framing the Origin Mark II's); planets at mid-window | a warning, not a score: how far a halo or reflection spreads depends on the optics; Mars moves 0.3° in a night at most |
| Field rotation limit | the trail at the object's edge in one sub at most a star's FWHM | stars elongated past 1.4 read as trailed; smart telescopes' live stacks derotate between subs, not within one; a mount type per preset (`config.Scope.Mount`): all alt-az, the Draco and the ORBIS 60EDX with a field derotator, from the makers' and distributors' specifications |
| Altitude window | 30°–80° | uptonight's limits; ≥ 30° keeps the airmass ≤ 2 |
| Emission line ratios | one set per type ([Nebula filters](#nebula-filters)) | single objects scatter by factors of a few; no list has line fluxes |
| Hβ | Hα/2.86 for every type | case-B recombination; dust raises the ratio, so dusty H II regions' Hβ is overstated |
| Emission-line light from V | V/f_V (photon shares) less c = 1.16 mag of stars and continuum | fitted to Finkbeiner's Hα map over 13 nebulae of 30′ and more (interquartile 0.19–2.45 mag), almost all H II regions; for other types f_V carries the difference |
| Airglow with the solar cycle | one slope (0.006/sfu) and a 0.6 share of the natural 22.0 for every site, the monthly flux | measured swings run 0.3–1.0 mag by site and cycle; the airglow also changes through the night and the seasons by up to a few tenths, and DarkSkySites' handling of the natural sky is undocumented |
| Colour sensor response | each preset's red, green and blue curves digitized from its maker's chart (`scripts/qe_digitize.py`, about ±1 point), each line at its dye's response, each channel's continuum its mean over 400–700 nm; a camera given by `-sensor`/`-pixel` takes the IMX678's | vendor curves, possibly above the delivered sensor's, some reused across chips; the moonlight's colour is weighted as over the whole band for every channel, not by each dye |
| Optics throughput | refractor 0.88, prime-focus mirror 0.81, another telescope 0.85 | coated surfaces, aluminium and the sensor window by design; no maker publishes its scope's; `-throughput` sets it |
| Uncooled sensor temperature | the air + 10 °C, the air 10 °C without a forecast | Seestar's battery runs about 10 °C above the air in the same housing; no smart telescope publishes its sensor's; `-sensor-temp` sets a cooled one's |
| Dark current | the chip's at 25 °C from ZWO's chart, else the charted median 0.16 e⁻/s, doubling every 5 °C | the charts' slope between 15 and 30 °C; the IMX678 and smaller chips have no reliable chart |
| Stacking | equal weights with `-scope`, weighted by SNR² otherwise | live stacks average what they keep; desktop stackers weight |
| Stack ceiling | a residual of 0.2% of the sky (`-residual`) | flats and gradient removal match the sky to 0.1–0.5%; one value for every rig |
| Blur | seeing (else 2.5″) × X^0.6, diffraction and 0.68 of a pixel in quadrature; a uniform ellipse | amateur sites see 2–3″; tracking and focus left out |
| Rejected subs and gaps | 15% of subs, 1 s between subs | the Origin loses 10–20% to tracking, satellites and wind; the download and live stack take about a second; `-reject` sets the share |
| Camera presets | 45 chips and 21 smart telescopes, the maker's peak QE (often an estimate) and read noise at its deep-sky gain (the OS08B10's from OmniVision's Nyxel 2 chart and one community measurement) | another gain changes the read noise (`-read-noise` sets it); a version without a published peak QE is left out |
| Filter band shift | `-filter-std`: top-hat bands of the nominal width, one effective index (2.05) for every filter | real filters' edges slope and their indices differ by coating (Chroma's 5 nm Hα measures 0.39 at f/2 where the model gives 0.53); filters sold for fast optics are assumed to pass their lines whole |
| Filter band width | every band `-filter-nm` wide, a `-scope`'s built-in filter's per line | real tri- and quad-band filters mix widths (the L-eNhance's 24 nm over Hβ and [O III]); one width keeps the flag simple |
| OSM obstacle heights | building 8 m (else 3 m a level + 2 m), tree 12 m, wood 20 m | typical two-storey house, mature broadleaf, temperate forest canopy; OSM tags few heights |
| Canopy floor | trees under 1 m ignored | the canopy map reads a metre or so over open ground |
| Night-sky spectrum | flat per nm, Moon before scattering and glow | the lamp mix is unknown; the Moon's redness offsets the Sun's fall |
| Moonlight scattering | aerosol asymmetry g = 0.8, offset δ = 0.078 in V, rescaled from his albedo 0.74 to the minute's | Winkler's fit at Sutherland (τ = 0.14); clean air, so heavy haze is extrapolated |
| Broadband filter passbands | `cls` Astronomik's published edges, `lpro` read off Optolong's reference curve (±5 nm); continuum flat per nm over 400–700 nm | one entry per class; under the flat (LED) glow brands within a class score alike |
| Zodiacal reference direction | 135° from the Sun at ecliptic latitude 45° | a mid-latitude zenith through the night; the zenith brightness given or defaulted includes that much zodiacal light |
| Galactic light reference | 81 S10, the sky's median by solid angle | a zenith over a year's nights; the zenith brightness given or defaulted includes that much galactic light |
| Zodiacal and galactic light off the zenith | their excess E dimmed by extinction alone (the whole column: E is tabulated above the atmosphere), outside K&S's X_s | light from beyond the atmosphere is not brightened by a longer path through glowing air; the share the configured zenith includes stays in K&S's term, as they fitted it |
| Galactic light in V | blue^0.45 · red^0.55 | 550 nm between Pioneer's 440 and 640 nm bands by wavelength, in log brightness |
| Ozone in a filter band | the 550 nm value | within 0.02 mag/airmass at every nebula line |
| High cloud weight | ½ | thin cirrus dims rather than blocks |
| Dew ramp | spread 4 → 1 °C, × 1 → 0.7 | dew on optics ends sessions |
| Gust ramp | 20 → 40 km/h, × 1 → 0.5, gusts at most 3× the mean wind, not capped below 20 km/h | shaken frames get discarded; mountain grid points' gusts run 5–10× the wind |
| Rain gate | 0 → 0.1 mm/h, × 1 → 0 | nobody images in rain |
| Washed-out night | no 60 minutes in a row at Q ≥ 0.2 with zenith seeing < 4″ | a fifth of a clear sky is no imaging, nor are 4″ stars at the Origin's 1.2″/px, nor a half-hour gap in the cloud; the forecast table still shows the night, with the hour after the window when its last half hour reads it |
| Transparency ramp | 7Timer 1 → 8, × 1 → 0.5, only without CAMS aerosols | haze dims but does not stop imaging; CAMS's is in the extinction |
| Aerosol scale height | 1.5 km, from the IFS cell's ground up | Hayes & Latham (1975); CALIOP 1.5–2 km over land |
| SUBS levels for a faint object | Levels × √(S/B_ref) below a pristine sky's brightness | reported totals grow as 1/S, not a fixed SNR's 1/S² ([Sub-exposure estimate](#sub-exposure-estimate)) |
| Frame fill | full credit at 25–80% of the short side | small objects lack detail, tight ones lack margin |
| Sampling verdict | 2–3 pixels per star FWHM is well sampled | the usual rule of thumb; zenith seeing, no airmass; shown only |
| Mosaic | 10% overlap, score ÷ panels | panels need overlap to register; the time is shared |
| Forecast gaps | mean of the covered hours | neither optimistic nor pessimistic |
| Clear night alert (`astro-notify`) | a minute is clear at cloud ≤ 10%, seeing ≤ 2″ and under 0.05 mm of rain in its hour (cloud and seeing known, unknown rain counting as none, as in the score); a stretch, and a listed target within it, of 2 h, or the window when shorter, but not under 30 min of a longer limit | "no clouds, good seeing" made numbers: 10% is about one okta, 2″ the report's own colour boundary; the limits are flags, and the 30 min keeps a window of minutes from passing for a clear night |

## References

| Topic | Source |
|---|---|
| Sun low-precision formulas, obliquity | *The Astronomical Almanac*, [section C][aa-sun] |
| Moon position and distance, lunar eclipse geometry | [Astronomy Engine][astronomy-engine] v2.1.19 (MIT), after [Montenbruck & Pfleger][montenbruck], *Astronomy on the Personal Computer*; ΔT: [Espenak & Meeus][deltat] |
| Planets' positions (STAR) | E. M. Standish, [Approximate Positions of the Planets][jpl-approx], JPL, Table 1; magnitudes [Meeus][meeus] ch. 41 |
| Field rotation | the parallactic angle, [Meeus][meeus] ch. 14 |
| Bright stars (STAR) | Hoffleit & Warren (1991), *The Bright Star Catalogue*, 5th revised edition, CDS [V/50](https://cdsarc.cds.unistra.fr/viz-bin/cat/V/50) |
| Sidereal time | [Aoki et al. (1982)][aoki], A&A 105, 359; [Meeus][meeus] eq. 12.4 |
| Precession | [Lieske et al. (1977)][lieske], A&A 58, 1; [Meeus][meeus] eqs. 21.2–21.4 |
| Coordinate transformations, separations, illumination, parallax | [Meeus, *Astronomical Algorithms*][meeus], chs. 13, 17, 40, 48 |
| Twilight definitions | [USNO][usno-twilight] |
| Comet orbits and magnitudes | [Meeus][meeus] chs. 30, 33, 34; [MPC comet orbit format][mpc-format]; Gauss (1809), IAU 1976 constants |
| Airmass | [Pickering (2002)][pickering], DIO 12, 1 |
| Rayleigh extinction | [Hayes & Latham (1975)][hayes], ApJ 197, 593; [Bucholtz (1995)][bucholtz], Appl. Opt. 34, 2765 |
| Aerosols | [CAMS][cams] via Open-Meteo |
| Sky brightness and moonlight | [Krisciunas & Schaefer (1991)][ks], PASP 103, 1033 (dark sky, Moon's magnitude law); scattered moonlight: [Winkler (2022)][winkler], MNRAS 514, 208; aerosol asymmetry range: [Andrews et al. (2006)][andrews], JGR 111, D05S04; opposition surge after [thorsky][thorsky] |
| Zodiacal light | [Leinert et al. (1998)][leinert], A&AS 127, 1, Table 17 (transcribed as in [astrogo](https://github.com/TuSKan/astrogo)); the natural-sky reference of [Falchi et al. (2016)][falchi], Sci. Adv. 2, e1600377 |
| Galactic light | Pioneer 10 background starlight, Toller (1981), PhD thesis, SUNY Albany, as [Leinert et al. (1998)][leinert], A&AS 127, 1, Tables 35 and 36 give it; the galactic frame: [Blaauw et al. (1960)][blaauw], MNRAS 121, 123 |
| Filter bands | V band: [Bessell (1990)][bessell], PASP 102, 1181; Ångström exponent: [Ångström (1929)][angstrom]; Moon colour: [Thejll et al. (2014)][moon-colour], A&A 563, A38 |
| Broadband filter passbands | [Astronomik CLS][astronomik-cls] (published edges); [Optolong L-Pro][optolong-lpro] (blocked lines, reference curve) |
| Emission-line light from V, large emission nebulae's Hα flux | Hα map: [Finkbeiner (2003)][finkbeiner], ApJS 146, 407, as LAMBDA's HEALPix file; SHASSA's filter: Gaustad et al. (2001), PASP 113, 1326; V passband: [Bessell (1990)][bessell] |
| Filters and the sensor | standard narrowband filters at f/2 to f/5: [Chroma Technology's f/# data][chroma]; the presets' QE charts and specifications: ZWO's camera manuals (ASI678MC V1.0, ASI2600MC/533MC/585MC/294MC Pro 2026, ASI662MC, ASI664MC, ASI676MC, ASI485MC, ASI482MC, ASI224MC, ASI2400MC, ASI6200, ASI461MM, ASI432MM, ASI174MM, ASI183, ASI462) and Player One's (Neptune-C II, Zeus 455C) |
| Airglow and the solar cycle | the component scaling: [Noll et al. (2012)][noll], A&A 543, A92; Paranal's V against the flux: [Patat (2008)][patat], A&A 481, 575; the airglow's share of a dark sky: [Benn & Ellison (1998)][benn], New Astron. Rev. 42, 503; the 22.0 natural sky at solar minimum: [Falchi et al. (2016)][falchi], Sci. Adv. 2, e1600377; the flux: NOAA SWPC's solar cycle indices and 30-day 10.7 cm flux |
| Emission line ratios | SNR criterion: [Mathewson & Clarke (1973)][mathewson], ApJ 180, 725; Magellanic Cloud PN [O III]/Hβ: [Stanghellini et al. (2003)][stanghellini], ApJ 596, 997; the criterion against H II regions: [Kopsacheili et al. (2020)][kopsacheili], MNRAS 491, 889; the remnants' [N II]: [Fesen, Blair & Kirshner (1985)][fbk], ApJ 292, 29; H II regions: [Madsen, Reynolds & Haffner (2006)][madsen], ApJ 652, 401; planetaries' [N II] and [S II]: [Riesgo & López (2006)][riesgo], RMxAA; Wolf-Rayet shells: [Mesa-Delgado et al. (2014)][mesa], ApJ 785, 100; case-B Hα/Hβ: [Osterbrock & Ferland (2006)][agn2], *Astrophysics of Gaseous Nebulae and AGN*, 2nd ed. |
| Per-filter Moon avoidance (comparison) | [Telescopius][telescopius]; the BAIT Lorentzian as in ACP |
| Bortle scale | [Bortle (2001)][bortle], *Sky & Telescope* |
| Surface brightness | [OpenNGC][openngc]; Gary Imm's Deep Sky Compendium; [SkEye][skeye] |
| Optical turbulence | [Dewan et al. (1993)][dewan], PL-TR-93-2043 |
| Tropopause, hypsometric equation | AMS Glossary: [tropopause][ams-tropopause], [hypsometric equation][ams-hypsometric] |
| Fried parameter, seeing FWHM | [Fried (1966)][fried], JOSA 56, 1372; [Roddier (1981)][roddier], Progress in Optics 19, 281 |
| Seeing calibration | [Cuevas et al. (2024)][cuevas], MNRAS 529, 2208; [Osborn & Sarazin (2018)][osborn], MNRAS 480, 1278 |
| Constellation boundaries | [Roman (1987)][roman], PASP 99, 695; CDS [VI/49][vi49]; [astrogo](https://github.com/TuSKan/astrogo) |
| Observability rules, target lists | [uptonight][uptonight] |

[astronomy-engine]: https://github.com/cosinekitty/astronomy
[jpl-approx]: https://ssd.jpl.nasa.gov/planets/approx_pos.html
[montenbruck]: https://link.springer.com/book/10.1007/978-3-540-67221-0
[deltat]: https://eclipse.gsfc.nasa.gov/SEhelp/deltatpoly2004.html
[aa-sun]: https://aa.usno.navy.mil/faq/sun_approx
[usno-twilight]: https://aa.usno.navy.mil/faq/RST_defs
[meeus]: https://ui.adsabs.harvard.edu/abs/1998aalg.book.....M
[aoki]: https://ui.adsabs.harvard.edu/abs/1982A%26A...105..359A
[lieske]: https://ui.adsabs.harvard.edu/abs/1977A%26A....58....1L
[mpc-format]: https://minorplanetcenter.net/iau/info/CometOrbitFormat.html
[pickering]: http://www.dioi.org/vols/wc0.pdf
[hayes]: https://doi.org/10.1086/153548
[bucholtz]: https://doi.org/10.1364/AO.34.002765
[cams]: https://atmosphere.copernicus.eu/
[ks]: https://doi.org/10.1086/132921
[leinert]: https://doi.org/10.1051/aas:1998105
[winkler]: https://doi.org/10.1093/mnras/stac1387
[andrews]: https://doi.org/10.1029/2004JD005734
[blaauw]: https://doi.org/10.1093/mnras/121.2.123
[falchi]: https://doi.org/10.1126/sciadv.1600377
[benn]: https://arxiv.org/abs/astro-ph/9909153
[patat]: https://arxiv.org/abs/0801.2270
[noll]: https://arxiv.org/abs/1205.2003
[thorsky]: https://github.com/jrthorstensen/thorsky
[cobs]: https://cobs.si/
[bortle]: https://skyandtelescope.org/astronomy-resources/light-pollution-and-astronomy-the-bortle-dark-sky-scale/
[openngc]: https://github.com/mattiaverga/OpenNGC
[skeye]: https://skeye.rocks/apps/skeye/book/explanations/visibilitymeasures
[7timer]: https://www.7timer.info/
[dewan]: https://apps.dtic.mil/sti/pdfs/ADA279399.pdf
[ams-tropopause]: https://glossary.ametsoc.org/wiki/Tropopause
[ams-hypsometric]: https://glossary.ametsoc.org/wiki/Hypsometric_equation
[fried]: https://doi.org/10.1364/JOSA.56.001372
[roddier]: https://doi.org/10.1016/S0079-6638(08)70204-X
[cuevas]: https://doi.org/10.1093/mnras/stae630
[osborn]: https://doi.org/10.1093/mnras/sty1898
[uptonight]: https://github.com/mawinkler/uptonight
[bessell]: https://doi.org/10.1086/132749
[angstrom]: https://doi.org/10.1080/20014422.1929.11880498
[moon-colour]: https://arxiv.org/abs/1401.1994
[mathewson]: https://doi.org/10.1086/151999
[madsen]: https://arxiv.org/abs/astro-ph/0609558
[riesgo]: https://arxiv.org/abs/astro-ph/0602201
[fbk]: https://doi.org/10.1086/163130
[mesa]: https://arxiv.org/abs/1402.6181
[chm]: https://doi.org/10.1016/j.rse.2023.113888
[agn2]: https://ui.adsabs.harvard.edu/abs/2006agna.book.....O
[finkbeiner]: https://doi.org/10.1086/374411
[stanghellini]: https://arxiv.org/abs/astro-ph/0307351
[kopsacheili]: https://arxiv.org/abs/1909.05879
[telescopius]: https://telescopius.com/
[astronomik-cls]: https://www.astroshop.eu/broadband-filters/astronomik-filters-2-cls-ccd-filter/p,16748
[optolong-lpro]: https://optolong.com/cms/document/detail/id/13.html
[chroma]: https://www.chroma.com/resources/technical-library/narrowband-astronomy-filter-f-number-data/
[roman]: https://doi.org/10.1086/132034
[vi49]: https://vizier.cfa.harvard.edu/viz-bin/VizieR?-source=VI/49
