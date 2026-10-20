# Models and formulas

This document explains how astro-recommender turns a location and a date into a ranked list of deep sky objects. For each step it gives the formula, where it comes from, and the simplifications it makes. The [README](README.md) covers how to use the program. Its [How objects are ranked](README.md#how-objects-are-ranked) section is the short version of this document, and its [Accuracy](README.md#accuracy) section has the measured error of each astronomical formula against SOFA.

Everything here is implemented directly in Go from the cited sources; no astronomy library is used at run time. Package names in parentheses (`internal/astro`, …) point to the code.

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

The program samples the night on a **1-minute grid**, from dusk to dawn. It computes everything about the night that does not depend on the target once per grid minute (`scoring.Sky`):

- the local sidereal time,
- the Moon's position, altitude and the light it scatters,
- the extinction coefficient,
- a weather quality factor.

It then walks every target through the same grid (`scoring.scoreTarget`). Each minute it decides whether the target is *observable*, and if so how much that minute is worth:

```math
w_i \;=\; \underbrace{Q_i}_{\text{weather}} \;\times\; \underbrace{T(h_i,\kappa_i)}_{\text{extinction}} \;\times\; \underbrace{W_{\text{sky}}(S, B_i)}_{\text{sky glow vs. object}}
```

```math
\text{SCORE} \;=\; F_{\text{frame}} \cdot \frac{1}{N}\sum_{i\,\in\,\text{observable}} w_i ,
\qquad
\text{UP} \;=\; \frac{\#\,\text{observable minutes}}{N}
```

N is the number of minutes in the window. Each factor lies between 0 and 1, so a perfect target (up all night, clear, moonless, at the zenith, well framed) scores 1. The sections below derive each factor:

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

**Time base.** Every formula takes the number of days since J2000.0:

```math
n = \frac{t_{\text{Unix}}}{86400} - 10957.5 ,\qquad T = \frac{n}{36525}\ \text{(Julian centuries)}
```

The difference between UT and TT (ΔT, about 70 s) is ignored. In that time the Moon moves about 0.01°.

**Sidereal time.** Local mean sidereal time uses the linear terms of IAU 1982 GMST ([Aoki et al. 1982][aoki], [Meeus][meeus] eq. 12.4), plus the east longitude λ:

```math
\text{LST} = 280.46061837^\circ + 360.98564736629^\circ \cdot n + \lambda
```

**Sun.** The Sun's position comes from the *Astronomical Almanac* low-precision formulas ([section C][aa-sun]). With mean longitude L and mean anomaly g:

```math
L = 280.460^\circ + 0.9856474^\circ n ,\quad
g = 357.528^\circ + 0.9856003^\circ n ,\quad
\lambda_\odot = L + 1.915^\circ \sin g + 0.020^\circ \sin 2g
```

The latitude is 0, and the ecliptic is rotated to the equator by the obliquity `ε = 23.439° − 0.0000004° n` (the same rotation, inverted, gives a target's ecliptic coordinates for the zodiacal light, `astro.Ecliptic`; against SOFA's IAU 2006 ecliptic of date they are within 3.9″, the low-precision obliquity's drift by 2050, and the Sun's longitude within 0.0123°):

```math
\tan\alpha = \frac{\cos\varepsilon \cos\beta \sin\lambda - \sin\varepsilon \sin\beta}{\cos\beta\cos\lambda},\qquad
\sin\delta = \sin\varepsilon\cos\beta\sin\lambda + \cos\varepsilon\sin\beta
```

**Precession.** Catalogue positions are J2000. The sidereal time is of date, so the catalogue positions are moved to the mean equator and equinox of date with the IAU 1976 angles ([Lieske et al. 1977][lieske], [Meeus][meeus] eqs. 21.2–21.4):

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

Without precession, altitudes in 2026 would be up to 41′ off. C is clamped to ±1 because rounding near the pole can push it just past 1. Each target is precessed once, at the middle of the night. Nutation (< 20″) and refraction (about 2′ at 30°) are ignored. Both are far below the 30° altitude limit's needs.

**Altitude and azimuth** follow from the spherical triangle of hour angle `H = LST − α`, declination δ and latitude φ ([Meeus][meeus] eqs. 13.5–13.6):

```math
\sin h = \sin\varphi\sin\delta + \cos\varphi\cos\delta\cos H ,\qquad
A = \mathrm{atan2}\!\big(-\sin H\cos\delta,\ \sin\delta\cos\varphi - \cos\delta\sin\varphi\cos H\big)
```

The azimuth is measured from north through east. Thousands of targets are evaluated at hundreds of minutes, so the scoring loop computes this in a cheaper but equivalent form. `sin φ`, `cos φ`, `sin δ` and `cos δ` are computed once per target (`astro.Horizontal`). `cos H` comes from the difference formula `cos(LST − α) = cos LST cos α + sin LST sin α`, with the LST sines stored per grid minute. The altitude limits are compared as sines (`sin h` rises with h), so `arcsin` runs only for the minutes that pass. The azimuth is computed only when a horizon profile needs it.

**Angular separation.** Separations use the spherical law of cosines ([Meeus][meeus] eq. 17.1). In the scoring loop they are dot products of unit vectors, `u = (cos δ cos α, cos δ sin α, sin δ)`. The cosine is compared against the limit before any `arccos` runs.

**Galactic coordinates**, for the Milky Way's light, rotate the J2000 position into the IAU 1958 galactic frame ([Blaauw et al. 1960][blaauw]): the north galactic pole at α_P = 192.85948°, δ_P = 27.12825°, and the north celestial pole at galactic longitude l_NCP = 122.93192° (`astro.Galactic`):

```math
\sin b = \sin\delta\sin\delta_P + \cos\delta\cos\delta_P\cos(\alpha-\alpha_P) ,\qquad
l = l_{\text{NCP}} - \mathrm{atan2}\big(\cos\delta\sin(\alpha-\alpha_P),\ \sin\delta\cos\delta_P - \cos\delta\sin\delta_P\cos(\alpha-\alpha_P)\big)
```

The frame is FK5's; SOFA's ICRS-based `Icrs2g` agrees within 0.005″ (`TestSOFAGalactic`).

## The night window

`astro.Window`, `astro.Tonight`, `astro.ClipWindow`

The night is the time while the Sun's geometric centre is below −18° (astronomical twilight), or −12° with `-twilight nautical` ([USNO definitions][usno-twilight]). The program does not solve for the crossing times. It steps minute by minute for 24 hours from the site's **solar noon**, `12:00 UTC − λ/15 h`, taking the solar noon closest to local noon. Starting from solar noon rather than local midnight has three benefits:

- A time zone that does not match the site cannot cut the night in two.
- Zones across the date line (Samoa, Kiritimati) get their own night rather than the next one.
- Polar night is capped at 24 hours.

If the Sun is already below the limit at noon and rises above it soon after, that darkness is the end of the previous night and is skipped. If that darkness lasts past midnight (more than 12 hours), it is tonight's night: at the end of polar night the Sun can clear the limit only minutes before the next noon (85° N on 31 December). If the Sun never gets below the limit, there is no night (high-latitude summer). Without `-date`, a run during the night plans only what remains of it; a polar night in progress, which would end at the next noon however near, carries on into the next day's night when that one is dark from the same noon, up to 24 hours from now (`astro.ContinuePolar`). `-from`/`-to` can only narrow the window.

## The Moon

`internal/astro`

**Position.** The Moon's geocentric ecliptic coordinates use the *Astronomical Almanac* low-precision series ([section D][aa]). The longitude has six periodic terms and the latitude four:

```math
\begin{aligned}
\lambda &= 218.32^\circ + 481267.881^\circ T + 6.29\sin(135.0 + 477198.87T) - 1.27\sin(259.3 - 413335.36T) \\
&\quad + 0.66\sin(235.7 + 890534.22T) + 0.21\sin(269.9 + 954397.74T) \\
&\quad - 0.19\sin(357.5 + 35999.05T) - 0.11\sin(186.5 + 966404.03T) \\[4pt]
\beta &= 5.13^\circ \sin(93.3 + 483202.02T) + 0.28\sin(228.2 + 960400.89T) \\
&\quad - 0.28\sin(318.3 + 6003.15T) - 0.17\sin(217.6 - 407332.21T)
\end{aligned}
```

These are rotated to RA/Dec as for the Sun. The result is good to about 0.4°.

**Distance** comes from the horizontal parallax series. The result r is in Earth radii: 55.9–63.8, with a mean of 60.27:

```math
\pi = 0.9508^\circ + 0.0518\cos(134.9 + 477198.85T) + 0.0095\cos(259.2 - 413335.38T) + 0.0078\cos(235.7 + 890534.23T) + 0.0028\cos(269.9 + 954397.70T),
\qquad r = \frac{1}{\sin\pi}
```

**Topocentric shift.** The Moon is close enough that the observer's position moves it by up to 1°. The observer is treated as a unit vector on a spherical Earth, rotated by the local sidereal time, and subtracted from the Moon's geocentric vector of length r ([Meeus][meeus] ch. 40):

```math
\vec{m}_{\text{topo}} = r\,\hat{u}(\alpha,\delta) - \big(\cos\varphi\cos\text{LST},\ \cos\varphi\sin\text{LST},\ \sin\varphi\big)
```

The Moon's altitude and its separation from each target both use this topocentric position, as astroplan does.

**Illumination and phase** come from the Sun–Moon elongation ψ ([Meeus][meeus] ch. 48):

```math
\text{illuminated fraction} = \frac{1-\cos\psi}{2} ,\qquad \text{phase angle } \alpha_\text{phase} \approx 180^\circ - \psi \quad (0 = \text{full})
```

The illumination, phase angle and distance are taken once, at the middle of the window, and drive both the Moon-distance rule and the moonlight model below. Against SOFA (`TestSOFA`) the distance is within 0.19 Earth radii (0.3%, 0.6% in the moonlight's (60.27/r)²), the phase angle within 0.35° of the true angle at the Moon and the illumination within 0.27 percentage points of the true illuminated fraction (1 + cos i)/2.

**Rise and set.** The header's "rises" and "sets" (and the JSON `moon.up` spans) are the minutes the Moon's topocentric centre crosses −50′: the upper limb on the refracted horizon, 34′ of refraction plus 16′ of semi-diameter, as the almanacs define moonrise and moonset ([USNO][usno-twilight]). The moonlight model and the Moon-distance rule keep the geometric horizon (centre above 0°); the few minutes between change nothing there.

## Comets

`internal/comets`

Orbital elements come from the Minor Planet Center's daily `CometEls.txt` ([format][mpc-format]): perihelion time T, perihelion distance q, eccentricity e, argument of perihelion ω, node Ω, inclination i (J2000 ecliptic), and the magnitude parameters H and K. Each comet is propagated as an unperturbed two-body orbit. The Gaussian gravitational constant is k = 0.01720209895 rad/day, and Δt is the time from perihelion in days.

**True anomaly ν and heliocentric distance r**, by orbit type:

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

**Heliocentric ecliptic position**, with argument of latitude `u = ω + ν` ([Meeus][meeus] ch. 33):

```math
x = r(\cos\Omega\cos u - \sin\Omega\sin u\cos i) ,\quad
y = r(\sin\Omega\cos u + \cos\Omega\sin u\cos i) ,\quad
z = r\sin u\sin i
```

**Earth's position** is the Sun formula reversed:

```math
R = 1.00014 - 0.01671\cos g - 0.00014\cos 2g ,\qquad (X_\oplus, Y_\oplus) = -R\,(\cos\lambda_\odot, \sin\lambda_\odot)
```

λ☉ is a longitude *of date*, while MPC elements are J2000. The general precession, 1.3969713° per century, is therefore subtracted from it. Without that correction a comet 0.5 AU away lands about 0.7° off.

**Geocentric direction.** The vector from Earth to the comet is rotated from the ecliptic to the equator by the J2000 obliquity, 23.4392911°. That gives the J2000 RA/Dec and the geocentric distance Δ, which is then precessed to date like a catalogue position. Light-time is ignored, and the perihelion time (TT) is treated as UT. Positions were checked against JPL Horizons.

**Brightness** follows the MPC total-magnitude law, evaluated at the middle of the window. Comets fainter than `-comet-mag` are dropped:

```math
m = H + 5\log_{10}\Delta + 2.5\,K\log_{10} r
```

MPC fits H and K once over an apparition, and weeks later they run 1–4 magnitudes off what observers see (October 2026: 10P/Tempel 14.7 against an observed 10.2–11.8, 161P 9.7 against 10.6–11.3, 220P 14.9 against 12.4–13.5). So the [COBS][cobs] observations of the 30 days before the night move the law by their median offset from it at each observation (`comets.FetchObserved`, `comet.offset`), when a comet has at least three; the description then says `(COBS)`, and the three read 10.5, 11.4 and 12.9. COBS keys them by the MPC designation, `10P` or the packed provisional `K22E020`, which the elements' first twelve columns carry; a numbered fragment keeps its letters (`73Pbt`, COBS's `component`), so it does not take its parent's observations. COBS answers 2500 records a page, newest first, and every page is read (a busy month, autumn 2025, held 3236); a page that fails leaves the elements' magnitudes rather than a part of the month. COBS sends no CORS headers, so the browser keeps the elements' magnitudes.

A comet has a separate position for every grid minute (`Target.Track`). It has no catalogue size or surface brightness, so the size limits and framing do not apply to it, and it is scored as sky-limited.

## Atmosphere and sky

`internal/atmos`

### Airmass and extinction

**Airmass** uses [Pickering (2002)][pickering], which stays accurate down to the horizon (h in degrees):

```math
X(h) = \frac{1}{\sin\!\big(h + 244/(165 + 47\,h^{1.1})\big)}
```

**Transmission**, relative to the same object at the zenith, for an extinction coefficient κ in magnitudes per airmass:

```math
T(h,\kappa) = 10^{-0.4\,\kappa\,(X(h)-1)}
```

With κ = 0.2, T is about 0.83 at 30° altitude.

**The extinction coefficient** at 550 nm (about V, and the wavelength of the CAMS aerosol optical depth) is the sum of three terms:

```math
\kappa = \underbrace{0.1066\,e^{-z/7996\,\text{m}}}_{\text{Rayleigh}} + \underbrace{0.029}_{\text{ozone}} + \underbrace{1.086\cdot\text{AOD}_{550}}_{\text{aerosols}}
```

- **Rayleigh scattering by air molecules**: an optical depth of 0.098 at 0.55 µm ([Hayes & Latham 1975][hayes]; consistent with [Bucholtz 1995][bucholtz]), decreasing with site elevation z over an 8 km scale height.
- **Ozone**: the Chappuis band for a 300 DU column.
- **Aerosols**: the hourly AOD from the [CAMS][cams] forecast via Open-Meteo, thinned for the height the site stands above the ground CAMS's column rests on (`atmos.AerosolAbove`): AOD × e^(−Δz/1.5 km), Hayes & Latham (1975)'s aerosol scale height (CALIOP's profiles give 1.5–2 km over polluted land, more over dust; Yu et al. 2010). CAMS's 0.4° cell averages the terrain, which the air-quality API does not report, so the ECMWF IFS 0.25° cell's (Open-Meteo with `elevation=nan`, `weather.GridElevation`) stands in: La Palma's 2334 m observatory sits in a sea-level cell, and its 0.24 becomes 0.05, κ 0.37 → 0.16 against the observatory's measured 0.13–0.15 (Mauna Kea 0.11, Paranal 0.13, as measured). A site below its cell keeps the AOD (a valley's haze is not less, and nothing says how much more), as does one whose cell is unknown. The factor 1.086 = 2.5 log₁₀ e converts optical depth to magnitudes.

The three parts are kept apart (`atmos.RayleighExtinction`, `OzoneExtinction`, `AerosolExtinction`) because they scale differently with wavelength, which the [filter bands](#nebula-filters) use. A fixed `-extinction` is split the same way: ozone first, Rayleigh up to its sea-level 0.1066, the rest aerosol (`atmos.SplitExtinction`); below ozone's 0.029 (`-extinction 0`) it is all ozone, which a band then keeps at the total (`Sky.ozone`), so no filter band is dimmed more than the unfiltered light.

Past the CAMS range (about 5 days), a typical AOD of 0.1 at sea level is used, thinned the same way for the site's elevation. The elevation comes from the CAMS response, else from the weather forecast's, whose range is longer. When neither has it, or when `-extinction` is given, that fixed value (default 0.2) is used instead. A smoky or dusty night therefore penalises low targets and spreads moonlight more widely.

### Sky brightness: Krisciunas & Schaefer (1991), moonlight after Winkler (2022)

The sky brightness B at the target is computed in nanoLamberts, a linear unit that can be added, with the [K&S][ks] conversion:

```math
B_{\text{nL}} = 34.08\, e^{\,20.7233 - 0.92104\,V} \qquad (V \text{ in mag/arcsec}^2)
```

**The site's dark-sky brightness at the zenith** comes from the first of these that is available:

1. `-sqm` (a meter reading),
2. the typical SQM value for `-bortle`,
3. a DarkSkySites lookup, run only when neither is given and an API key is available,
4. a pristine 22.0 mag/arcsec².

| Bortle | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 |
|---|---|---|---|---|---|---|---|---|---|
| band (mag/arcsec²) | ≥ 21.96 | 21.7–21.96 | 21.3–21.7 | 20.8–21.3 | 20.3–20.8 | 19.6–20.3 | 18.9–19.6 | 18.0–18.9 | < 18.0 |
| `-bortle` value | 21.98 | 21.83 | 21.5 | 21.05 | 20.55 | 19.95 | 19.25 | 18.45 | 17.5 |

The bands are DarkSkySites' map's (its classifier, October 2026), the service the lookup asks, so `-bortle N` and the header's `≈ Bortle N` mean the sky its map shows as class N; the values are the bands' middles (class 9 half a class under 18.0). [Bortle (2001)][bortle] defined his classes by eye, and SQM bands are later conventions that disagree by up to a class: Wikipedia's and lightpollutionmap.info's put class 6 at 18.94–19.50, DarkSkySites' own imaging calculator at 18.9 (a class brighter than its map's band throughout), and this table at 18.8 until October 2026. A measured `-sqm` beats any of them.

<a id="airglow"></a>**The solar cycle** (`atmos.AirglowExcess`, `internal/solar`). Airglow, the upper atmosphere's own light driven by the Sun's ultraviolet, is about two-thirds of a dark V zenith at solar minimum (145 of 220 S10 at the ecliptic and galactic poles, [Benn & Ellison 1998][benn]), and follows the 11-year cycle: V brightened 0.3 mag from solar minimum to maximum at Paranal ([Patat 2008][patat]), 0.4–0.5 at La Palma, 0.6 at Mauna Kea and 0.5 at CTIO. The ESO sky model ([Noll et al. 2012][noll]) scales each airglow component as 1 + m(S − 129), S the monthly mean 10.7 cm solar flux in sfu, m 0.0061 for the continuum and 0.0087 for [O I] 557.7 (about a fifth of V); with m = 0.006 the zenith gains

```math
\Delta B = 0.6\,B_{22.0}\left(\frac{1 + 0.006\,(S - 129)}{1 + 0.006\,(90 - 129)} - 1\right)
```

over the configured brightness: 0.6 is the airglow's share of the natural 22.0 at the reference direction (135°, 45°), where the zodiacal light is above the poles', and 90 sfu is the flux of that 22.0, solar minimum, as [Falchi et al. (2016)][falchi] take it (their atlas, from which light pollution maps derive, subtracts a natural sky scaled to each SQM reading's solar activity, so it holds artificial light only) and as Noll's model gives it. ΔB is added whatever the light pollution, whose natural part is that 22.0, and is clamped to 65–250 sfu (Noll's fit spans 95–228). At 180 sfu the zenith is 0.38 mag brighter, as Noll's model (22.0 to 21.6); at 107 sfu (October 2026) 0.08; at solar minimum (65) 0.14 darker. Under Bortle 6 and brighter the change is a few hundredths. S is NOAA SWPC's observed monthly mean for the night's UTC month, else (for a night within 30 days of NOAA's last daily value) the mean of the last 30 days, which Patat found the airglow follows best and which NOAA's monthly prediction overstated by 20 sfu in October 2026, else that prediction (to 2030); without one (`-no-solar`, a failed lookup, a date past 2030) the configured sky stands. An explicit `-sqm` is a measurement of a night's sky, airglow included, so it is not scaled. Time-of-night and seasonal airglow changes (up to a few tenths, by site) are not modelled.

**Towards the horizon** the sky brightens because the line of sight passes through more glowing air, and dims through extinction. K&S model this with a scattering airmass X_s at zenith distance Z = 90° − h:

```math
X_s(h) = \big(1 - 0.96\sin^2 Z\big)^{-1/2} ,\qquad
B_{\text{dark}}(h) = B_{\text{zen}}\cdot 10^{-0.4\,\kappa\,(X_s-1)}\cdot X_s
```

(The zodiacal and galactic light of the target's direction then add a term of their own, below.)

**Zodiacal light.** The natural sky is not the same in every direction: sunlight scattered by interplanetary dust brightens it along the ecliptic, most towards the Sun, and least at the ecliptic poles. [Leinert et al. (1998)][leinert] Table 17 gives its brightness Z(Δλ, β) at 500 nm by the ecliptic longitude from the Sun Δλ and the ecliptic latitude β, from 77 (in 10⁻⁸ W m⁻² sr⁻¹ µm⁻¹) at the pole to 259 on the ecliptic 90° from the Sun; 1 S10 unit is 1.28 of those and 27.78 mag/arcsec² in V, so the pole reads 23.3 mag/arcsec², the ecliptic at 90° 22.0, and the gegenschein opposite the Sun 22.1. The zenith brightness above already includes the zodiacal light of a typical zenith: a mid-latitude site's zenith runs over β = 45° ± 23.4° and Δλ from 90° at dusk to 180° at midnight, so the value is taken to include Z at the reference direction (Δλ, β) = (135°, 45°), which reads 90, and is corrected per target by the difference (`atmos.ZodiacalExcess`; the heliocentric ±7% and seasonal ±10% factors are left out):

```math
B_{\text{zen}}(\text{target}) = \max\big(B_{\text{zen}} + Z(\Delta\lambda,\beta) - Z(135^\circ, 45^\circ),\ Z_{\text{pole}}\big)
```

in nanoLamberts, bilinearly interpolated in the table, with the pole value past β = 75° and the blank cells near the Sun (which no night-time direction reaches) filled from the row's first tabulated latitude. The target's ecliptic coordinates come from its mid-window position of date (a comet's too) and the Sun's longitude at mid-window. On a pristine sky this makes the ecliptic 90° from the Sun 0.5 mag/arcsec² brighter than the reference and the pole 0.06 darker: a faint galaxy in Virgo or Leo in the evening sees a brighter sky than one in Ursa Major, and its sky-limited weight is 15–25% lower. Under light pollution the same nanoLamberts hardly show. The floor is the pole's own light: no configured sky gets darker than that (with the galactic light below, than the pole's light plus the darkest galactic light).

**Galactic light.** The Milky Way's unresolved stars, and the starlight its dust scatters (the diffuse galactic light, 20–30% of it), are the natural sky's other large variation. Pioneer 10 mapped them from beyond 2.8 AU, where the zodiacal light has gone: [Leinert et al. (1998)][leinert] Tables 35 and 36 (from Toller 1981) give that background starlight G(l, b) in blue (440 nm) and red (640 nm), every 10° of galactic longitude l by galactic latitude b (±80° in steps of 4° near the plane, 10° beyond), in S10⊙ units with the stars brighter than V 6.5 removed, as an imaging camera resolves them. A source of solar colour reads the same in both, so V (550 nm) is taken between them by wavelength in log brightness, G_V = G_blue^0.45 · G_red^0.55 (red reads 1.3 times blue at high latitude, the galactic light being redder than the Sun). Pioneer's spin axis kept it off 28 cells around the Sun (l 180–240°, b 16–50°); they are filled along each row's latitude, log-linearly between the nearest measured cells, where the light falls off smoothly towards the anticentre. G_V runs from 29 S10 at the galactic poles (24.1 mag/arcsec²) to 900 in the Sagittarius star clouds (20.4), against 205 for a whole 22.0 mag/arcsec² sky. The zenith brightness is taken to include the table's median over the sky by solid angle, 81 S10, what a zenith sees over a year's nights, and is corrected per target by the difference (`atmos.GalacticExcess`), the zodiacal light's beside it:

```math
B_{\text{zen}}(\text{target}) = \max\big(B_{\text{zen}} + Z(\Delta\lambda,\beta) - Z(135^\circ, 45^\circ) + G_V(l,b) - G_{\text{ref}},\ Z_{\text{pole}} + G_{\min}\big)
```

bilinearly interpolated, the ±80° rows going to their mean at the poles. The galactic coordinates come from the target's mid-window position (`astro.Galactic`, the IAU 1958 frame in J2000, within 0.005″ of SOFA's). On a pristine sky the galactic poles are 0.3 mag/arcsec² darker than the reference, the plane 0.5–1.1 brighter, the Sagittarius clouds 1.7 and the LMC 0.4: a faint galaxy at high galactic latitude sees the darkest natural sky, and a nebula in the Cassiopeia Milky Way about a magnitude brighter one. Under light pollution, again, it hardly shows. Pioneer's ~2° beam smooths the star clouds, and its red band carries some Hα, so the brightest H II regions' cells read high. The floor adds the darkest galactic light to the zodiacal pole's.

**Off the zenith** the two are not brightened as the rest of the dark sky is. K&S's X_s stands for the longer path through glowing air (the airglow, and the light pollution scattered in it), while the zodiacal and galactic light come from beyond the atmosphere, which only dims them. So their excess over what the configured zenith includes, E = B_zen(target) − B_zen, is added outside the dark-sky term and dimmed by extinction alone, the whole column at the Pickering airmass X of the target's own light (E is tabulated above the atmosphere, while B_zen is measured on the ground):

```math
B_{\text{dark}}(h) = B_{\text{zen}}\cdot 10^{-0.4\,\kappa\,(X_s-1)}\cdot X_s + E\cdot 10^{-0.4\,\kappa\,X}
```

The share the configured zenith already includes stays in K&S's term, as their fit had it. At 30° altitude, where X_s is 1.9, scaling E like the glow would roughly double it: the Cassiopeia Milky Way's sky there would read 0.25 mag/arcsec² brighter on a pristine sky (0.07 at 60°), the galactic and ecliptic poles' deficit likewise. At the zenith the two agree, and the floor holds there.

**Scattered moonlight** is added while the Moon is above the horizon, after [Winkler (2022)][winkler]'s revised simplified scattering model. The Moon's V flux outside the atmosphere comes from its magnitude, which depends on its phase angle α (degrees, 0 = full):

```math
m_{\text{moon}} = -12.73 + 0.026\,\alpha + 4\cdot10^{-9}\alpha^4 ,\qquad
F^* = 10^{-0.4\,m_{\text{moon}}}\cdot \Big(\frac{60.27}{r}\Big)^2 \cdot
\begin{cases} 1.35 - 0.05\,\alpha & \alpha < 7^\circ \\ 1 & \text{otherwise}\end{cases}
```

- The magnitude law is the one K&S use (from Allen), at the Moon's mean distance; `(60.27/r)²` scales it to the actual distance, which varies by ±13%.
- The last factor is the *opposition surge*, the extra brightening near full Moon that the magnitude law leaves out. It is modelled as in Thorstensen's skycalc ([thorsky][thorsky]).

That beam is scattered once in a uniform layer of optical depth τ = 0.4 ln 10 · κ = 0.921 κ. Each height is reached through only the air above it, and the scattered light is dimmed again on its way down. K&S instead dimmed the beam by the whole column before it scatters, which makes the moonlit sky too dark, most of all under a low Moon. Integrated down the line of sight, this gives Winkler's gradation function, with X_s(h) the scattering airmass above for the target and the Moon:

```math
\Gamma = X\,\frac{e^{-\tau X} - e^{-\tau X_{\text{moon}}}}{X_{\text{moon}} - X} \qquad \big(\to \tau X e^{-\tau X}\ \text{as}\ X_{\text{moon}} \to X\big)
```

The light is scattered towards the target by a normalised phase function (sr⁻¹). It mixes molecules (Rayleigh, [Bucholtz 1995][bucholtz], χ = 0.0148, K&S's 1.06 + cos²ρ shape) and aerosols (Henyey–Greenstein, asymmetry g = 0.8), weighted by their shares of the scattering optical depth. Those shares are the minute's own Rayleigh and aerosol extinction (κ_R, κ_A), so hazier air weighs the forward-peaked aerosol term more:

```math
p(\rho) = \frac{\kappa_R}{\kappa_R+\kappa_A}\,\frac{3(1-\chi)}{16\pi(1+2\chi)}\Big(\frac{1+3\chi}{1-\chi} + \cos^2\rho\Big) + \frac{\kappa_A}{\kappa_R+\kappa_A}\,\frac{1}{4\pi}\,\frac{1-g^2}{(1 + g^2 - 2g\cos\rho)^{3/2}}
```

```math
B_{\text{moon}} = 10^{\delta}\, F^*\, p(\rho)\, \Gamma \,/\, 4.2545\cdot10^{10}
```

ρ is the Moon–target angle. The last factor turns per-steradian into per-arcsec². δ = 0.078 is Winkler's V-band offset: it carries the particles' albedo and the light scattered more than once. g and δ are his fit at Sutherland (SAAO), where the model reproduces the moonlit sky mostly within 0.05 mag. His albedo there, scattering over all extinction, is τ_s/τ = (0.082 + 0.020)/0.138 = 0.74 (his Table 4), which δ holds; the minute's own is (κ_R + κ_A)/κ, ozone absorbing the rest, so B_moon is scaled by it over 0.74 (0.86 at κ = 0.2, 0.1–0.26 mag brighter than δ alone at the coefficients the model makes). Against K&S, the full Moon 60° up puts 18.48 mag/arcsec² at a target 60° up and 90° away (K&S: 18.45). The sky is 0.8–1.1 mag brighter within about 10° of the Moon (the aerosol forward peak), a little brighter under a Moon below about 12°, and about as bright as K&S's at moderate separations. Aerosols range over 0.5 < g < 0.8 ([Andrews et al. 2006][andrews]), so under heavy haze (κ ≳ 0.3) a narrower-than-real forward peak likely makes the sky far from the Moon somewhat too dark; there it also gets darker with more haze, where K&S's grew brighter. The total is

```math
B = B_{\text{dark}} + B_{\text{moon}}
```

The table's SKY column is this B (with the zodiacal and galactic light) converted back to mag/arcsec² and averaged over the observable minutes. The part of B_moon that does not depend on the target (F*, the Moon's airmass and beam transmission, and the Rayleigh share) is computed once per grid minute (`atmos.MoonLight`).

## Object surface brightness

`catalog.Target.SurfaceBrightness`

What sky glow does to an extended object depends on how bright the object is **per square arcsecond**, not on its total magnitude ([SkEye's explanation][skeye]). Each target's V surface brightness S is taken from the first rule that applies:

0. **Measured line flux.** A planetary nebula with an integrated Hα flux (`haflux`: Frew, Bojičić & Parker 2013, J/MNRAS/431/2, 1258 planetaries measured on SHASSA and VTSS plus 49 from the literature; observed, [N II] removed, not dereddened; `scripts/haflux.py` matches them by position, within max(1′, a quarter of the size), to 1499 entries over 14 lists) gets the V surface brightness of its lines (`catalog.Target.FluxSB`): the Hα flux over its Hα share gives every line's flux by the type's ratios, each line's photons through V (Bessell's curve at the line, `LineV`) over the ellipse of the axes, against a V = 0 source's photons. Exact but for the ratios. A faint planetary's catalogued magnitude is often its central star's: Abell 7 derived 30.2 from its 16.0, the flux gives 26.0; EGB 6 30.0 against 27.3. For the well-measured ones the two agree within about 0.6 mag (NGC 7293 21.9 against 21.3, M 57 19.2 against 18.3). Frew's tables miss many northern ones (M 27, M 97, NGC 7662, Jones-Emberson 1), which keep the rules below.
1. **Measured V.** The Compendium's V mean surface brightness (`surfbr`). Exact.
2. **Measured B, moved to V.** [OpenNGC][openngc]'s B mean surface brightness within the 25 mag/arcsec² isophote (`bsurfbr`), minus a colour. The colour is the object's own `B − V` when it lies in −0.3…1.5 (OpenNGC pairs inconsistent magnitudes for about 100 objects). Otherwise it is a typical colour for the kind of object, and the value is marked `~`:

   | Kind | typical B − V |
   |---|---|
   | galaxies, galaxy groups | 0.8 (median offset of Compendium V vs OpenNGC B over 723 shared galaxies) |
   | planetary nebulae | 0.7 |
   | other emission-line objects | 0 |
   | everything else | 0.5 |

   `TestTypicalBV` recomputes them as the median B − V of the lists' objects with both magnitudes (galaxies 0.77, planetaries 0.80 within the bounds above, the other emission-line objects 0.07, the rest 0.43); a planetary's [O III] sits in V, out of B, so the 0 it once had made a B-only planetary up to 0.9 mag too bright.

3. **Derived from magnitude and size.** For single galaxies and nebulae only. Marked `~`. The integrated V magnitude m (or B minus the typical colour) is spread over a uniform ellipse of the major and minor axes (in arcminutes; a disc of the major axis when the minor is unknown):

   ```math
   S = m + 2.5\log_{10}\!\Big(\frac{\pi}{4}\,(60\cdot\text{major})(60\cdot\text{minor})\Big)
   ```

   Without a minor axis an elongated galaxy is narrower than the disc, so the result leans faint.
4. **Unknown** (S = 0) in every other case:
   - Star clusters, even with a measured value: their light sits in points a surface brightness would spread, an image of one is judged by its faintest members, and MWSC sizes are the core only.
   - Galaxy groups: their magnitude is one member's, but their size is the whole group's.
   - Clusters with nebulosity: their magnitude is the stars', not the glow's.
   - Nebulae whose catalogued magnitude is a star's or a cluster's (`starMag`): reflection, Wolf-Rayet and variable nebulae by type (the star lighting them: NGC 7023 7.1, vdB 150 8.4, WR 134 8.1), and by name in any list's spelling (`starMagNames`) the emission nebulae that carry their cluster's or star's (the Heart and Soul 6.5, which spread over 90–150′ made the Heart unreachable, the Wizard, the Cocoon, IC 1318's γ Cyg 2.2), the Compendium's 18.0 for unknown, and planetaries carrying the central star's (Sh2-174, Sh2-188, Jones-Emberson 1, Abell 35, Sh2-216).
   - Comets.
   - Any object without data.

   An unknown S scores the object as sky-limited, the most pessimistic case.

The object's type decides its kind, and `catalog.Target.kind()` is the single place that maps the lists' many type spellings to galaxy, group, cluster, cluster+nebula, nebula or other.

## Weather

`internal/weather`, `scoring.BuildSky`

All the weather inputs combine, minute by minute, into one quality factor Q between 0 and 1. Most terms use a linear **ramp** that is 1 on the good side of `good`, falls to `floor` at `bad`, and stays there beyond it:

```math
\text{ramp}(v;\ \text{good},\text{bad},\text{floor}) = 1 - \text{clamp}_{[0,1]}\!\Big(\frac{v-\text{good}}{\text{bad}-\text{good}}\Big)(1-\text{floor})
```

**Cloud** (Open-Meteo, hourly). The low, mid and high layers are treated as randomly overlapping. Thin high cirrus is counted at half:

```math
C = 1 - (1 - c_\text{low})(1 - c_\text{mid})(1 - c_\text{high}/2)
```

The full factor:

```math
Q = (1 - C)\cdot
\underbrace{\text{ramp}(T - T_d;\,4,1,0.7)}_{\text{dew risk}}\cdot
\underbrace{\text{ramp}(\text{gust};\,20,40,0.5)}_{\text{km/h}}\cdot
\underbrace{\text{ramp}(\text{precip};\,0,0.1,0)}_{\text{mm/h}}\cdot
\underbrace{\text{ramp}(\text{transp};\,1,8,0.5)}_{\text{7Timer}}
```

- **Dew risk**: the spread between temperature and dew point falling from 4 °C to 1 °C costs up to 30%.
- **Gusts**: from 20 to 40 km/h cost up to half, counted at most 3× the hour's mean wind (never capped below 20 km/h, where it would change nothing): Open-Meteo's hourly gust runs a median 2.5× the 10 m wind at lowland and Paranal points but 4.8× at La Palma's and 10.6× at Mauna Kea's, where the grid's gust parameterisation, not the summit's air, makes 30–50 km/h gusts in a 5–10 km/h breeze.
- **Rain**: the hour's precipitation ramps Q down to 0 at 0.1 mm, whatever the cloud layers say; an unknown amount does nothing. Open-Meteo reports gusts and precipitation over the *preceding* hour, so each hour reads the next entry. Cloud, temperature, dew point, wind and the seeing estimate are the values at the hour, so a minute takes the nearest hour's (a front arriving at 21:00 counts from 20:30, not 21:00), gusts and rain its own hour's.
- **Transparency**: [7Timer!][7timer]'s ASTRO product (1 best, 8 worst; 3-hourly, about 3 days ahead) scales Q from 1 down to 0.5, only for hours without a CAMS aerosol forecast (a fixed `-extinction`, a failed fetch): its class is mostly the same haze, which the extinction already charges, and counting both cost a clear La Palma night some 40% more subs.

When a forecast covers only part of the window, the uncovered minutes get the **mean of the covered ones**. A perfect sky would favour targets that are up only then. With no forecast at all, Q = 1. Weather only weighs the score. UP and the observable runs stay purely geometric.

The ramps are heuristics: the thresholds are judgment, not measurement (see [Heuristics](#heuristics)).

## Scoring a target

`internal/scoring`

### Which targets are scored

Before any minute is evaluated, a target is dropped when its major axis is outside `-size-min`…`-size-max` (an unknown size counts as 0; comets are exempt), when it is outside the `-ra`/`-dec` region, when `-skip` names it, or when it is of no `-type` class (`catalog.Target.Is`: a galaxy group counts as a galaxy, a cluster with nebulosity as both cluster and nebula, `emission` is the emission-line targets below). The region is a box of ±`-tol` degrees on each axis around the J2000 position, with RA counted at 15° per hour and wrapping at 24h, not scaled by cos δ.

### Is this minute observable?

A minute counts when all of these hold (the rules come from [uptonight][uptonight]):

1. `-alt-min ≤ h ≤ -alt-max`, 30°–80° by default, within 0°–90°. Being above 30° also guarantees an airmass ≤ 2.
2. h is above the local horizon profile at the target's azimuth, when `-horizon` is given.
3. **The Moon-distance rule**: while the Moon is above the horizon, the target is at least `illumination in %` degrees from it. For example, a 60%-lit Moon keeps a broadband target 60° away; through a filter, an emission-line target's limit shrinks by how much moonlight the filter's bands pass against its own light in them ([Nebula filters](#nebula-filters)): 18° for an H II region through a 7 nm dual-band filter or Hα alone, the floor (0.3 of the limit) that the Moon's halo sets. Unlike uptonight, the rule is skipped while the Moon is down.

The longest unbroken stretch of observable minutes is reported as OBSERVABLE. A target whose longest stretch is shorter than `-min-run` is dropped.

### Star clusters

Every target takes the whole sky against it. Clusters once took half (k = 0.5, bright point sources standing out of the glow), which put the Double Cluster, NGC 457 and NGC 7789 at #1–3 under any sky, ahead of M 33 and the Heart at Bortle 5. Their bright stars do show on any sky, and so decide nothing between nights; what a cluster image shows tonight is its faint members and background, at the limiting magnitude, sky-limited like any faint light. So a cluster scores as a target of unknown surface brightness, and the Moon-distance rule holds it as far as any other. A `-filter` replaces the sky with the bands' share of each part: a line filter for emission-line targets ([Nebula filters](#nebula-filters)), a broadband one for any target ([Broadband filters](#broadband-filters)).

### The sky-glow weight

In a fixed exposure, the signal-to-noise ratio of a surface of brightness S over a sky B goes as

```math
\text{SNR} \propto \frac{S\,t}{\sqrt{(S + B)\,t}}
```

The weight compares tonight's SNR with the SNR the same object would have under a pristine 22.0 mag/arcsec² sky, B_ref. All quantities are in nanoLamberts:

```math
W_{\text{sky}} = \min\!\Bigg(1,\ \sqrt{\frac{S + B_{\text{ref}}}{S + B}}\Bigg)
```

- **An object much brighter than the sky** (S ≫ B) gets W ≈ 1. It loses nothing to moonlight.
- **A faint object** (S ≪ B) is *sky-limited*: `W ≈ √(B_ref / B)`. Every target with unknown surface brightness (S = 0), and every star cluster, falls in this case.

A faint galaxy 30° from a bright Moon therefore scores much lower than one 120° away, while a filtered emission nebula keeps most of its score.

### Nebula filters

`-filter dual|tri|quad|ha|oiii|sii` (`scoring.passes`, `atmos.SkyParts`, `atmos.BroadSky`, `catalog.Target.Lines`) scores the emission-line targets through a filter: emission, planetary and Wolf-Rayet nebulae, supernova remnants, and a few named objects whose catalogue type hides their Hα, such as IC 1396. A dual-band filter passes Hα and [O III] onto one frame, a tri-band one Hβ too, a quad-band one Hβ and [S II] too; the others pass one line. Every band is the same width, so a commercial multi-band filter with one wide band (the L-eNhance's 24 nm over Hβ and [O III]) is only approximated. Each band is `-filter-nm` wide (7 nm by default). Bands wide enough to overlap — past 14.6 nm Hβ's meets [O III]'s, past 16.1 Hα's meets [S II]'s, and the flag allows 30 — are clipped at the midpoint of the two lines (`scoring.lineBands`), each side keeping its own line's response, so the shared wavelengths are charged their sky once, as a physical filter's merged bands pass them once; the lines' light is counted once per line regardless.

<a id="filters-in-a-fast-beam"></a>**Filters in a fast beam** (`-filter-std`, `scoring.beam`). An interference filter tilted by θ passes its band at λ·√(1 − sin²θ/n²), bluer, n being its effective index. Behind a telescope of focal ratio N each ray meets the filter at tan θ = r/2N, r from the central obstruction's edge to 1 across the pupil, so a standard filter (centred for collimated light) passes a line only from the rays whose shifted band still holds it. Taking the band as a top-hat [lo, hi] of the nominal width, a line at λ is in while n²(1 − (λ/lo)²) ≤ sin²θ ≤ n²(1 − (λ/hi)²), and its share is the pupil area, uniform in r², between the two:

```math
t = \frac{\min(1, r^2_{\text{hi}}) - \max(\varepsilon^2, r^2_{\text{lo}})}{1-\varepsilon^2},\qquad r^2 = \frac{\sin^2\theta}{1-\sin^2\theta}\,(2N)^2
```

with ε the obstruction over the aperture. n = 2.05 (`filterN`) fits [Chroma's measured curves][chroma] of its 3, 5 and 8 nm Hα, Hβ, [O III] and [S II] filters at f/2 to f/5 (55 curves, rms 0.06 in the line's transmission, no bias; `scripts/filter_shift.py`). Each line's share w then counts t of it; the sky stays the band's, since the shift moves the band without narrowing it (Chroma's integrated passbands hold within a few per cent to f/2). The beam is the Origin Mark II's f/2.2 with its 77 mm obstruction, which leaves only the steeper rays (or `-scope`'s, or `-focal`/`-aperture`/`-obstruction`); without the flag t is 1 inside the band and 0 outside, the filters taken as made for the beam. At f/2.2:

| Band | Hα 656.3 | [N II] 654.8 | [N II] 658.3 | [O III] 500.7 |
|---|---|---|---|---|
| 3 nm | 0.16 | 0.69 | 0 | 0.33 |
| 7 nm | 0.87 | 1 | 0.16 | 1 |

(behind the original Origin's 63 mm: 0.25, 0.73, 0, 0.40 and 0.88, 1, 0.25, 1)

so a standard 7 nm Hα filter costs the Origin enough to lose to no filter on an H II region (IC 1795 at Bortle 6 on 14 October 2026: 351 subs unfiltered, where a filter made for the beam needs 237), and a 3 nm one loses further. Broadband filters' edges shift too, but by a few nm against passbands tens of nm wide, and are left alone.

**The sky in a band.** The sky splits into three parts with different spectra:

- the **glow** (airglow and light pollution, with the zodiacal and galactic light: the dark-sky term B_dark),
- moonlight scattered by **molecules** (Rayleigh, the χ term of p(ρ)), going as λ⁻⁴,
- moonlight scattered by **aerosols** (Mie, the Henyey–Greenstein term), going as λ^−1.3 ([Ångström][angstrom]).

The model gives them in the V band ([Bessell 1990][bessell]), and the unfiltered weight's V sky stands for the whole camera band, 400–700 nm (`atmos.CameraLo`–`CameraHi`, UV/IR cut). A band Δλ wide at λ passes its share of that band, as a [broadband filter's](#broadband-filters) passbands do (`atmos.BroadSky` over λ ± Δλ/2):

```math
f_{\text{glow}} = \frac{\Delta\lambda}{300},\qquad
f_{\text{R}} = \frac{\Delta\lambda}{300}\,\frac{(\lambda/551)^{-4}}{\langle(\lambda/551)^{-4}\rangle_{400-700}},\qquad
f_{\text{M}} = \frac{\Delta\lambda}{300}\,\frac{(\lambda/551)^{-1.3}}{\langle(\lambda/551)^{-1.3}\rangle_{400-700}}
```

so per nm the [O III] band (500.7 nm) gets 2.9 times the Rayleigh moonlight of the Hα one (656.3 nm). Moonlight is taken as flat per nm before scattering: the Moon is 0.25 mag redder than the Sun in B−V ([Thejll et al. 2014][moon-colour]), which roughly offsets the Sun's own fall from 500 to 670 nm. The glow is taken as flat too: its spectrum depends on the site's lamps (sodium, white LEDs), which nothing here knows; flat is white LEDs, the case [broadband filters](#broadband-filters) gain least in. A multi-band filter's fractions are the sums over its bands. (The bands were once shares of V's 88 nm, which charged a line filter 3.4 times the sky per nm a broadband one is charged against the same unfiltered sky.)

**The target's light in the bands.** No catalogue gives line fluxes, so each type gets typical ratios to Hα (`catalog.Target.Lines`), with Hβ (486.1 nm) at Hα/2.86 for every type, the case-B recombination ratio at 10⁴ K ([Osterbrock & Ferland 2006][agn2]; dust only raises it), [N II] split 1:3 over 654.8 and 658.3 nm and [O III] 495.9 nm a third of 500.7 (each doublet's fixed ratio), all normalised to shares that sum to 1, the whole of what an unfiltered camera collects:

| Type | Hα | [O III] 500.7 | [S II] 671.6+673.1 | [N II] 654.8+658.3 | Basis | shares Hα, [O III] (both lines), [S II], Hβ, [N II] |
|---|---|---|---|---|---|---|
| H II region (and every other emission-line target) | 1 | 0.18 | 0.19 | 0.36 | the average of 13 classical O-star H II regions in WHAM's 1° beam ([Madsen, Reynolds & Haffner 2006][madsen]): [N II] 658.3 0.27 and [S II] 671.6 0.11, one line of each doublet, so the doublets are 0.27 × 4/3 and 0.11 × (1 + 1/1.45), 671.6/673.1 at low density; [O III] scatters most | 0.47, 0.11, 0.09, 0.16, 0.17 |
| planetary nebula | 1 | 2.8 | 0.05 | 1 | [O III]/Hβ 6–9 (Magellanic Cloud averages), Hα = 2.86 Hβ; over 613 planetaries log Hα/[S II] peaks at 1.2 and log Hα/[N II] at 0 ([Riesgo & López 2006][riesgo]) | 0.16, 0.61, 0.01, 0.06, 0.16 |
| supernova remnant | 1 | 0.5 | 0.6 | 0.8 | [S II]/Hα > 0.4 ([Mathewson & Clarke 1973][mathewson]) and [N II] 658.3/Hα > 0.5 ([Fesen, Blair & Kirshner 1985][fbk]) define them; [O III] varies | 0.29, 0.20, 0.18, 0.10, 0.23 |
| Wolf-Rayet nebula | 1 | 1 | 0.2 | 0.6 | NGC 6888's dereddened slits ([Mesa-Delgado et al. 2014][mesa]): [O III] about Hα, the N-enriched ejecta's [N II] 0.4–0.9 of it | 0.29, 0.38, 0.06, 0.10, 0.17 |

The share in the filter's bands is w: every line a band reaches, the filter's own and the others in it, so a 7 nm Hα band (652.8–659.8 nm) passes both [N II] lines, a 3 nm one only 654.8 at its edge, and a 30 nm [O III] band Hβ (an H II region: 0.64 through Hα, 0.72 through dual-band, 0.88 tri-band, 0.97 quad-band, all but [O III] 495.9; a planetary: 0.46 through [O III], 0.78 through dual-band). [N II] is why a remnant gains most from Hα and a planetary from dual-band.

Each extra band also passes its share of the sky, so the multi-band filters score close to `dual`. Sky-limited under the glow alone (w²/f_glow, against 1 unfiltered) through 7 nm bands:

| Target | `ha` | `oiii` | `sii` | `dual` | `tri` | `quad` |
|---|---|---|---|---|---|---|
| H II region | 17.3 | 0.3 | 0.3 | 11.1 | 11.1 | 10.1 |
| planetary nebula | 4.6 | 8.9 | 0.0 | 13.1 | 10.1 | 7.7 |
| supernova remnant | 11.9 | 0.9 | 1.3 | 9.7 | 8.6 | 9.7 |
| Wolf-Rayet nebula | 9.0 | 3.5 | 0.1 | 11.9 | 10.2 | 8.8 |

The multi-band filters' use beyond that is colour on a one-shot-colour camera, which no score here measures.

**The target's light against the camera band.** An emission line is collected once whatever the band, while the camera band holds `atmos.CameraBand` = 300/88 V bands of sky, so against the V sky that stands for the camera's an emission-line target is 3.4 times fainter; a continuum is held by the band as the sky is, and its S is unchanged. Its light S itself is its lines, which its V surface brightness measures only in part ([below](#emission-line-light)).

**The weight.** The target's signal through the filter is w·S, the sky the bands' share of each part. Against the same pristine-sky reference as broadband:

```math
W_{\text{filter}} = \min\!\Bigg(1,\ \sqrt{\frac{w^2\,(S + B_{\text{ref}})}{w\,S + f_{\text{glow}}B_{\text{glow}} + f_{\text{R}}B_{\text{R}} + f_{\text{M}}B_{\text{M}}}}\Bigg)
```

With w = 1 and every f equal to 1 this is the broadband weight.

**Extinction in the bands.** The extinction coefficient κ above is at 550 nm. Rayleigh scattering goes as λ⁻⁴ and aerosol extinction as λ^−1.3 like the moonlight they scatter, while the ozone term barely changes across the nebula lines (the Chappuis band peaks at 600 nm; within 0.02 mag/airmass), so a band at λ sees

```math
\kappa_\lambda = \kappa_{\text{R}}\Big(\frac{\lambda}{551}\Big)^{-4} + \kappa_{\text{O}_3} + \kappa_{\text{aer}}\Big(\frac{\lambda}{551}\Big)^{-1.3}
```

and a multi-band filter the mean over its lines weighted by the share of light each passes. At κ = 0.2 the Hα band's coefficient is 0.13 and the [O III] band's 0.26, so at airmass 2 an Hα target through its filter keeps 6% more light than the V coefficient says and an [O III] target 5% less. Without a filter an emission-line target is dimmed at its lines' own coefficient too (the mean over all its lines, weighted by their shares: Hα and [N II] dominate an H II region's), so a filter cannot win on extinction alone, as it could when the unfiltered target was dimmed at V's.

**A list of filters** (`-filter dual,ha,oiii`, the ones you own) scores each target without a filter and through each filter that passes some of its light (a line filter: emission-line targets; a [broadband](#broadband-filters) one: all), each **worn all night**, as it is shot: changing filters mid-target means a second set of flats and a split stack, and a filter that costs a bright nebula under a dark sky (object-limited, it loses 1 − w of the light) costs it every minute. The choice that gains most wins, no filter on a tie (then the first in the list); that filter is the target's FILTER (`Result.Filter`), `none` when no filter beats going without, whose weights are then exactly the unfiltered ones (the filtered sky, the sum of its parts, matches the unfiltered one only to rounding). Gains most is not the highest SCORE: under a dark sky every helpful line filter's weight reaches the cap of 1, which would leave the band's extinction to choose (Hα's, the lowest, even for a planetary, whose light is half [O III]). Without a camera it is the most uncapped weight, Σ Q·T·√(SNR² ratio); with framing, which describes one, and a surface brightness to estimate subs from (`ExposureSB`), the fewest subs the [sub-exposure model](#sub-exposure-estimate) needs for the good level over the choice's own observable minutes, read noise, the colour channels, the stacking (`-stack`) and the residual ceiling (`-residual`) included: the per-sub SNR² m and the ceiling C give L²/N = m(1 − L²/C), so FILTER is exactly the choice SUBS needs fewest subs through (a level past every ceiling ranks the highest ceiling first). The total Σ Q·SNR²_sub would let a choice win on minutes alone — a narrowband filter's Moon limit (below) admits moonlit minutes no filter keeps, and near the Moon a bright nebula then showed Hα in FILTER with more subs in SUBS than no filter needs over its few Moon-down minutes. The Origin's 10 s colour subs thus pick `cls` under a dark sky, where a 7 nm band's sky and dark current only match the read noise, dual-band over Hα (two channels see it) under a suburban one, and Hα under a city sky. All the choices are scored in one pass over the grid, which shares the geometry (altitude, horizon, the Moon's separation) and the sky's parts.

**The Moon limit.** Away from the Moon its light is mostly Rayleigh-scattered, so the filter's sky-limited gain against moonlight is `f_R / w²`. That, at most 1 and at least 0.3, scales the Moon-distance rule. The floor holds every narrowband filter that would beat the Moon to within a few degrees at some 30° from a full Moon: near it the halo and the gradients across the frame, which no λ⁻⁴ scattering model holds, limit narrowband imagers too. At full Moon (100°) through 7 nm bands:

| Target | `ha` | `oiii` | `sii` | `dual` | `tri` | `quad` |
|---|---|---|---|---|---|---|
| H II region | 30° | 100° (no gain) | 100° (no gain) | 30° | 30° | 30° |
| planetary nebula | 30° | 30° | 100° | 30° | 30° | 30° |
| supernova remnant | 30° | 100° (no gain) | 30° | 30° | 30° | 30° |
| Wolf-Rayet nebula | 30° | 32° | 100° | 30° | 30° | 30° |

The filtered limits run below [Telescopius][telescopius]' Lorentzian Moon avoidance (60° for Hα and [S II], 90° for [O III], 120° for broadband, at full Moon), which is a scheduling rule of thumb rather than a model; here the sky weight already prices the moonlight near the limit.

### Broadband filters

`-filter cls|lpro` (`config.Broadband`, `scoring.broadPasses`, `atmos.BroadSky`) are the light-pollution filters that pass wide passbands rather than single lines:

| Choice | Passbands (nm) | Stands for | f_glow | f_R | f_M |
|---|---|---|---|---|---|
| `cls` | 450–520, 640–690 | Astronomik CLS (published edges); Optolong CLS and IDAS LPS-D2 are close | 0.40 | 0.36 | 0.39 |
| `lpro` | 400–430, 440–540, 555–575, 625–700 | Optolong L-Pro; IDAS LPS-D1/P2/D3 and Baader's Moon & Skyglow are alike | 0.75 | 0.80 | 0.77 |

Optolong publishes only the lines the L-Pro blocks (Hg 435.8, 546.1, 577–578 nm; Na 589, 598, 616 nm), not its band edges, so `lpro`'s are read off its reference curve and are good to about ±5 nm. Brands within a class would score alike, so there is one entry per class.

The fractions are taken against the same unfiltered camera band as the line filters' bands: the glow's is the passbands' share of the 300 nm; the moonlight's is weighted by its scattering, `∫ (λ/551)⁻⁴ dλ` over the passbands against the camera band (and λ⁻¹·³ for Mie), so cutting the blue cuts more Rayleigh moonlight than glow.

The glow is still flat per nm: the lamps are taken to be white LEDs, a continuum, the conservative case. A broadband filter's gain against sodium or mercury lamps, which it was designed for, is not modelled; under LEDs it mostly cuts sky as a share of the band.

w is the target's share of light through the passbands, against the same unfiltered camera:
- an emission-line target: its lines that fall in a passband. Both filters pass Hβ, [O III], Hα and [S II], so w = 1 and the sky-limited gain under glow is 1/f_glow: 2.5 through `cls`, 1.33 through `lpro`, against an H II region's 11.6 through a 7 nm `dual`;
- anything else (galaxies, clusters, reflection nebulae, comets): its continuum, flat per nm, so w = f_glow, and w²/f_glow = f_glow < 1. The filter cuts the target as much as the sky and never wins, as imagers report of galaxies under LED skies.

The band's extinction is the mean of `(λ/551)⁻⁴` and `(λ/551)^-1.3` over the passbands for continuum, and the passed lines' as for line filters. The Moon limit is f_R/w², between 0.3 and 1: 36° at full Moon through `cls` for any emission-line target, 80° through `lpro`, and no change for continuum.

### Putting it together

```math
w_i = Q_i \cdot T(h_i,\kappa_{\lambda,i})\, W_i ,\qquad
\text{SCORE} = F_{\text{frame}}\cdot\frac{1}{N}\sum_i w_i\ \ \text{for the choice that gains most}
```

each choice (no filter, `W_sky`, and each passing filter, `W_filter`) summed over its own observable minutes, with κ_λ the band's coefficient, or without a filter V's for a continuum and the lines' for an emission-line target. Results are sorted by SCORE, and the mean altitude over the observable minutes breaks ties. Extinction does not also dim S. It enters once, as the separate factor T.

<a id="emission-line-light"></a>**Emission-line light from V** (`scoring.lightNL`). A catalogued V surface brightness, measured or derived from an integrated magnitude, measures an emission nebula's lines only as far as the V passband reaches them, and also counts the stars and continuum in it. The lines' light is

```math
S_{\text{lines}} = \frac{S_V}{f_V}\,10^{-0.4\,c},\qquad f_V = \sum_\ell s_\ell\,T_V(\lambda_\ell)
```

with s_ℓ the line shares above and T_V Bessell's V curve at each line (`catalog.LineV`: Hα 0.032, [O III] 0.481 and 0.337 at 495.9 nm, [S II] 0.016, Hβ 0.111, [N II] 0.035 and 0.028): V straddles [O III] and misses Hα. c = 0.99 mag (`vContinuum`) is fitted by `scripts/calibrate_lines.py`: the median, over the 15 emission nebulae of at least 30′ (not a planetary with a flux, which needs none) in GaryImm, Messier, OpenNGC, OpenIC, Sharpless and Caldwell (exact duplicates once; the Hα map's beam is 6′), of S_lines' Hα share against [Finkbeiner (2003)][finkbeiner]'s full-sky Hα map (NASA SkyView's "H-Alpha Comp", in Rayleighs), averaged over each disc less the median of a ring from 1.25 to 2 radii (the Milky Way's diffuse Hα). Nebulae whose catalogued magnitude is a star's or cluster's ([above](#object-surface-brightness)) derive none, so they are not in it. Its interquartile range is 0.22–1.92 mag: one nebula's lines are good to a factor of about three to five. The map measures Hα alone, so the fit holds each nebula's Hα where it was before [N II] was counted (c was 0.93 then) and the [N II] comes on top. The lines are then 1.63 mag brighter than V for an H II region (taking V as all of the light, as before, ran 1.1–1.5 mag faint on every subset of the sample), 1.34 for a remnant, 0.78 for a Wolf-Rayet nebula and 0.36 for a planetary, whose [O III] V holds. A surface brightness from a measured flux holds no stars, so `lightNL` undoes it exactly (in photons) without c. The SB column still shows the V surface brightness.

## Framing and mosaics

`scoring.frame`, `internal/config`

Framing is turned on by `-scope` ([smart telescopes](#smart-telescopes)), `-fov`, `-scale`, or `-focal` with `-sensor`/`-pixel` (or `-camera`'s, [presets](#camera-presets)); unset frame values are the scope's, else the Origin Mark II's.

**Field of view and pixel scale** from the focal length f (mm), a sensor side d (mm) and the pixel size p (µm):

```math
\text{FOV}_{\text{side}} = 2\arctan\frac{d}{2f}\ \ \text{(in arcmin)} ,\qquad
\text{scale} = 206.265\,\frac{p}{f}\ \ \text{arcsec/px}
```

The Celestron Origin's IMX678 sensor has 3856 × 2180 pixels of 2 µm, at 335 mm. That gives 1.32° × 0.75° at 1.23″/px.

**Size limits.** The smallest object worth framing is `-min-px` pixels across (200 by default): `size_min = min-px × scale / 60` arcminutes. The largest is the frame's short side. The catalogue size is the major axis, and the object's position angle in the frame is unknown, so the major axis has to fit even in the worst orientation. With `-mosaic` the limit is the largest grid's short side, and with `-rotate` its long side. Explicit `-size-min`/`-size-max` win over these defaults.

**Fill and frame score.** For a grid of c × r panels overlapping by 10% per side, the frame covers

```math
W = \text{FOV}_\text{long}\,\big(c - 0.1(c-1)\big) ,\qquad H = \text{FOV}_\text{short}\,\big(r - 0.1(r-1)\big)
```

The fill is `major / min(W, H)`. With `-rotate` the camera can be turned to put the major axis along whichever side fits better, and a known minor axis then counts:

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

- Full credit goes to objects filling 25–80% of the frame, 11–36′ on the Origin.
- An object as large as the frame (fill ≥ 1) scores 0 and is dropped.
- The night's time is shared among a mosaic's panels, so the score is divided by the panel count.
- `-mosaic N` tries every grid of at most N panels, and the fewest panels win a tie.
- Objects of unknown size get F = 1.

## Sub-exposure estimate

`scoring.Subs`, `catalog.Target.ExposureSB`, `internal/config`

With framing, each listed target gets the number of sub-exposures needed tonight for a modest, good and best image: a stacked SNR of 5, 10 and 20 per arcsec² at the object's mean surface brightness for an object at least as bright as a pristine 22.0 sky (in the score's terms: an emission-line target's lines against the camera band), and √(S/B_ref) of them for a fainter one, through its FILTER: with framing the `-filter` choice is made by this model, the one needing fewest subs tonight ([Nebula filters](#nebula-filters)). It shows in the SUBS column (the good level), in `-json` (`subs`) and in the web page's hover box.

**Camera.** The Origin Mark II's defaults (`-scope`'s, [below](#smart-telescopes)), each overridden by its flag: aperture D = 152 mm with a d = 77 mm central obstruction (Celestron, 50.7% of the aperture; the original Origin's is 63 mm); the IMX678's 0.83 peak QE and 0.97 e⁻ read noise at its ISO 1000, an upper bound measured from the 9-frame bias masters the Origin saves: a σ between neighbouring pixels of 27.6–29.0 ADU over two sessions (28.3–29.4 over nine with another estimator), at 0.2793/25 e⁻/ADU (it stores each ADC count as 25 ADU, its EGAIN being per count), bounds one frame's at 3·29.0·0.0112 = 0.97 e⁻ (measured with astro-analyzer, October 2026; one master's 1.1 e⁻ and a vendor sheet's 2.15 e⁻ at 8.5 dB gain before). The original Origin's IMX178 the same way at its ISO 200: σ 30.4 ADU in every session, at the photon-transfer gain of 0.0165 e⁻/ADU (its EGAIN header reads 4.5 times that), 3·30.4·0.0165 = 1.5 e⁻, against the IMX178 preset's 1.7 (ZWO at gain 100), which Stellina keeps; Celestron's 10 s subs; a colour (RGGB) sensor unless `-mono`. `-aperture` alone describes another telescope, so its obstruction is then 0 and its throughput 0.85. Framing without `-scope` (`-camera`, `-focal`, `-fov`, `-scale`) describes another telescope too, whose optics the Origin's would only guess, and whose sensor the Origin's IMX678 would only guess, so without `-aperture` and `-camera` it has no estimate (the Frame line names what is missing, `SUBS needs -aperture and -camera`); `-qe` and `-read-noise` then only override the preset's. A mono camera (`-mono`) has no colour response to guess, so `-qe` and `-read-noise` stand in for `-camera` there, its dark current then the typical 0.16 e⁻/s.

**Throughput** (`-throughput`). The QE is the bare sensor's; the optics in front of it pass less than all the light. A refractor (every smart telescope but the Origins and Unistellar's) passes 0.88: an apochromat and its flattener, about ten coated surfaces at 0.5% each, and the sensor's IR-cut window, 93% in ZWO's ASI676MC chart. A mirror at prime focus (the Origin's RASA, Unistellar's Newtonians) passes 0.81: aluminium at about 0.92, its corrector and the window. Another telescope (`-aperture`) takes 0.85, between them. These are by design, not measured per scope.

**Dark current** (`-dark`, `-sensor-temp`). Smart telescopes are not cooled, so their sensors make dark current at the night's temperature. Each preset carries its chip's at a sensor temperature of 25 °C, read off ZWO's dark-current charts: IMX571 0.065, IMX533 0.063, IMX676 0.060, IMX585 0.25, IMX294 (and the IMX492, its die) 0.37, IMX178 0.95 e⁻/s per pixel. Chips without a chart, the IMX678 among them (ZWO's CN manual labels its −20 °C point 0.0078 e⁻/s against an axis reading 0.12), take the median of those, 0.16 (`typicalDark`, which `TestTypicalDark` recomputes); the MN34230's chart (0.40 at 25 °C) is left out, as it would move that median to 0.25. The CCDs run ten times higher and stay out of the median: the KAF-16803's 3 e⁻/s is onsemi's table at 25 °C, the KAF-16200's 1.7 Moravian's 0.08 at 0 °C doubling every 5.7 °C, extrapolated; the Sony CCDs, uncharted, take 0.16, which a cooled camera's `-sensor-temp` makes small either way. The smart telescopes' own chips are the gap: neither ZWO's manuals (the IMX678's, IMX462's and IMX662's, English and Chinese, October 2026) nor Player One's listings nor a third party publish a dark current for them, and ZWO says only in words that the IMX462 (Seestar S50, Vespera) has amplifier glow, which is not modelled; dark frames from those telescopes would settle both. The current doubles every 5 °C, as the IMX585's and IMX676's charts do between 15 and 30 °C (it flattens below 0 °C, where it no longer matters). An uncooled sensor runs 10 °C above the air: no smart telescope publishes its sensor's temperature, and Seestar says its battery, in the same housing, runs about 10 °C above the air. The air is the forecast's per hour (the covered hours' mean where it has none, 10 °C without a forecast); `-sensor-temp` holds a cooled camera's sensor at its setpoint instead. At the Origin's 10 s under a 10 °C night (the sensor at 20 °C, 0.08 e⁻/s) that is 0.8 e⁻ a sub, against RN² ≈ 1.2 and, through a 7 nm Hα band under a dark sky, a third of an e⁻ of sky on the red pixels.

<a id="smart-telescopes"></a>**Smart telescopes** (`-scope`, `config.Scopes`). Twenty-one, from Celestron, ZWO, DwarfLab, Vaonis, Unistellar and Sky-Watcher (October 2026; the ORBIS 60EDX from its trade-show specifications, not yet on sale), each with its aperture, focal length and central obstruction, the frames its camera saves (pixel size after any binning) and its usual sub length; its sensor, when one of the camera presets below, gives the QE, read noise and colour response. `-scope origin` is the Mark II with the IMX678 behind a 77 mm obstruction; `-scope origin-mk1` the original with the IMX178 behind 63 mm. A scope whose sensor has no preset (OmniVision's, the IMX347, the IMX415) or whose central obstruction is unpublished (Unistellar's Newtonians, the Draco) frames and scores as any other, but has no camera model, so SUBS is left out unless `-qe` and `-read-noise` (and `-obstruction`) are given. `TestScopes` checks each scope's frame, from its pixels and focal length, against the maker's stated field: within 3%, except the eVscope's (rounded to the minute) and the Vespera 3 and Pro 2, whose stated 245 mm Vaonis contradicts with the Vespera II's 250 mm field and 2.39″/px.

<a id="camera-presets"></a>**Camera presets** (`-camera`, `config.Cameras`). Forty-five sensor chips, the ones in the current ZWO, Player One and ToupTek deep-sky, planetary and guide cameras, QHY's IMX568, IMX530, IMX250 and GSENSE4040 (front- and back-illuminated, two presets) cameras, the MN34230 (ZWO's ASI1600 manual), Moravian's IMX252 and the KAF-16200 and KAF-16803 CCDs (onsemi's absolute charts), Omegon's IMX287 and Starlight Xpress's Sony CCDs, the original Origin's IMX178 and the Seestar S50 Pro's OmniVision OS08B10 (the one not from a camera maker: its response and 80% peak from OmniVision's absolute QE chart for Nyxel 2, which a ZWO user group slide attributes to the chip, digitized from the slide's 971 px copy with its pale gridlines darkened, a second copy agreeing to 0.03; its read noise Cuiv's 0.84 e⁻ measured at gain 150), each with its pixel size and output resolution (giving `-sensor` and `-pixel` with `-focal`), the maker's peak QE for its colour and mono versions, and the read noise at the maker's deep-sky gain, where the high conversion gain mode switches on (read off ZWO's and ToupTek's charts to about ±0.2 e⁻, ToupTek's at gain 100 of its separately switched HCG mode; the IMX174, IMX183, IMX249 and AR0130 have no such mode and are quoted at the gain given, QHY's IMX568, IMX530 and IMX250 too, where the read noise levels off on QHY's own 0–350 gain scale, the MN34230 at its unity gain 139 and the IMX252 at Moravian's 24 dB, a CCD, without a gain setting, at its maker's typical read noise). A version is left out where no astro camera ships it or its peak QE is unpublished (the IMX183's and IMX174's colour); the IMX462's colour peak (81%, the green at 510 nm) and its `Response` are ZWO's ASI462MC/ASI662MC comparison's absolute chart, whose ASI662MC curve reproduces the manual's STARVIS 2 one to 0.007 (the ASI462 manual's relative chart peaks at 810 nm, and scaled to the same 81% it put the visible 10–35% low), the IMX178's (70%) Sony's datasheet as rk.edu.pl quotes it, ZWO's manual saying TBD, and the IMX568's (mono 67%, colour 53%) FLIR's EMVA 1288 report for its Dragonfly S DR-U3-50S4M/C at 523 nm, the colour version's green, since QHY charts it only relative to its peak (FLIR's red and blue, 63% and 86% of its green, against the chart's 88% and 78%: the chart, Sony's, is kept for the shape), and likewise the IMX252's (mono 64%, colour 60%, FLIR's Blackfly S), Moravian quoting none; the IMX287's 71% is Altair's (FLIR measures 62%), its read noise Altair's 1.41 e⁻ at no stated gain. The KAF-16200's colour peak is onsemi's 40% (its green). The IMX185 has no preset: ZWO's ASI185 manual and QHY's listing say TBD. A mono version carries its own `MonoResponse`, from ZWO's manual where ZWO sells it (one curve in its ASI2600MM, ASI533MM and ASI461MM manuals, Hα 0.64 of the peak; the IMX183's 0.62, the IMX432's 0.83, the IMX585's 0.88, the IMX662's 0.88, the IMX676's 0.87, the IMX178's 0.62), else its maker's (`qe_digitize.py --mono`, a single black or coloured curve; `--xpx` for a chart with tick marks instead of vertical gridlines, `--light-grid` for Sony's white gridlines on a tint): the IMX174's front-illuminated pixels at Hα 0.63 of the peak, the IMX249's 0.63 (Sony's curve for both chips, per watt, reads ZWO's IMX174 QE chart to 0.007), the ICX694's 0.84 (Starlight Xpress's curve, its "65% at Hα" against the 77% peak; Sony's relative chart reads 0.91), the IMX492's 0.66 (ZWO's and ToupTek's charts agree), the MN34230's 0.76 (ZWO's ASI1600 manual), the KAF-16200's 0.80 and KAF-16803's 0.77 (onsemi), the ICX825's 0.96 (Atik's coarse comparison chart; Starlight Xpress publishes none), the ICX829's 0.93 (Sony's overview, `--grid-max 0.5`), the GSENSE4040's 0.94 front- and back-illuminated (QHY's absolute charts), the IMX287's 0.85 (Sony's chart via The Imaging Source); the ICX695 takes the ICX694's curve (none published, the same EXview II family) and the IMX252 the IMX250's (Sony charts them as one), QHY's IMX568 0.68 (Sony's chart, per watt, within 0.01 of QHY's absolute IMX530 chart), IMX530 0.69 and IMX250's front-illuminated pixels 0.84 (QHY's absolute charts, a pale pink curve on a pale grid: `--sat-min 0.1 --grid-max 0.9` with `--box` and `--xpx` at the frame, the peaks giving the IMX530's 88% and IMX250's 70%); the uncharted (the ASI6200 manual has no QE chart, the ASI678 and ASI462 manuals and ToupTek's ATR428 page only the colour one) take a relative's: the IMX455 the IMX571's 3.76 µm curve, the IMX462 its STARVIS predecessor IMX290's (0.79), the IMX428 the IMX250's (front-illuminated Pregius, as the IMX432, 0.83), and the IMX678 `sonyMono`, the IMX585's STARVIS 2 curve (Hα 0.88, the continuum 0.95), which the IMX662's and IMX676's charts match within 0.03; `-mono` without `-camera` takes it too. **Sony's charts are per watt.** Sony's "relative response" charts, which ZWO, ToupTek, QHY, Player One and The Imaging Source reproduce, plot the signal per unit of radiant power, not per photon: Allied Vision's "Sony IMXnnn absolute QE" curves (IMX174, IMX264, IMX273, IMX287, IMX304) equal Sony's curve ÷ λ, renormalised, to about 1%, as do ZWO's own QE charts where its manuals print both (the IMX585, ±1.5%; its 3.76 µm mono chips, ±3%), and FLIR's EMVA 1288 colour measurements of twelve Sony chips match the ÷ λ ratios (blue/green, untouched by its IR-cut filter, at or above them; as charted red/green came out 1.3–1.45× too high). Read as charted, a Sony chart overstates Hα against [O III] by 656/501 = 1.31×, so `qe_digitize.py --per-watt` divides every curve by λ before the peak and the line values are taken; its peak moves to the green (511–534 nm). It applies to the Sony charts only (the per-watt comments in `config/cameras.go`); the maker's QE charts (the IMX585's, IMX462's, the 3.76 µm mono curve, the IMX174's, QHY's IMX530, IMX250 and GSENSE4040 ones) and onsemi's, OmniVision's and the CCDs' absolute charts are per photon already. The IMX678's, IMX485's and SmartSens SC2210's charts are labelled QE with no Sony chart to check them against, and Panasonic's MN34230 one does not say; they are read as charted. Every colour version carries its own `Response`, digitized by `scripts/qe_digitize.py` from the maker's QE chart: the gridlines found as the grey rows and columns spanning the plot (an evenly spaced grid fitted to them, so one hidden under the curves does not skew the scale), each curve's pixels told apart by hue within the plot (not the legend's swatches), every value relative to the chart's highest point over its whole range, which for a chip peaking in the near infrared (the IMX464, the IMX715) lies past 700 nm. It reproduces the IMX678 values read by hand to within 0.007. Makers reuse charts, and borrow them: the IMX290's is ZWO's ASI290 manual's (ToupTek's G3M290C page has none), with ToupTek's 81% peak, ZWO's saying TBD; the IMX662's and IMX664's are one (STARVIS 2, also in the ASI585MC's CN manual), the IMX482's is the IMX485's; where two of a chip's charts disagree (the ASI585MC Pro manual's against that CN one, green at Hα 0.16 against 0.37), the newer is taken. At equal QE, read noise and pixels, with no dark current and lossless optics, the response alone moves an emission nebula's SUBS by up to 2× across the presets (IC 1795 on 14 October 2026 from 45.8° N 16° E at Bortle 4, behind an 80 mm f/5 with an 11.1 × 6.3 mm sensor of 2.9 µm pixels, QE 0.8, 1 e⁻: 25 subs of 120 s for the good level unfiltered on the IMX662, 28 on the IMX676, 49 on the IMX410; 14–27 through dual-band). The Origin keeps its own read noise; `-camera imx678` is ZWO's ASI678MC (with `-mono` its ASI678MM, the same 83% peak QE) at gain 182, 0.9 e⁻. The values were checked in October 2026 against ZWO's product pages, manuals and gain charts and Testar's listings; ZWO's current ASI6200 chart gives the IMX455 1.35 e⁻ at gain 100 (its v1.4 manual 1.5).

**Photon rate.** A V = 0 source gives 1005 photons s⁻¹ cm⁻² Å⁻¹ at 550 nm (Bessell 1998), 884 400 s⁻¹ cm⁻² over the V band's 88 nm. The sky model works in V-band nanoLamberts, so a brightness of x nL is 884 400 · x / NL(0) photons s⁻¹ cm⁻² arcsec⁻², and per pixel

```math
R = \frac{884\,400\;x}{\mathrm{NL}(0)}\;\frac{\pi}{4}\left(D^2 - d^2\right)\;\tau\;QE_\mathrm{peak}\;q\;\Omega ,\qquad \Omega = \mathrm{scale}^2
```

with τ the throughput. The relative response q depends on what the light is:

- **The sky and continuum objects**, unfiltered or through `cls`/`lpro`, are spread over the camera's 400–700 nm (× 300/88 against V) at q = 0.8 for a mono sensor, or for a colour one each channel's own mean over the band from its chart (`Response.Broad`; the IMX678's red 0.40, green 0.57, blue 0.39 of peak, an RGGB mean of 0.48: the dyes overlap, so each passes more than a third of the band). The moonlight's colour is weighted as over the whole band for every channel, not by each dye's curve. The glow and a continuum are flat per nm; the moonlight scatters as it does in the score, so the camera band's Rayleigh part per nm is the λ⁻⁴ mean over 400–700 nm, 1.30 of V's (its Mie part 1.04). The sky's share through a broadband filter is `BroadSky`'s, as in the score.
- **Through a line filter** each band passes its own share of the sky (`BandSky`, against V, so absolute) to every channel at that dye's response to the band's line, and the object's lines likewise, weighted by its light in each. A mono sensor takes its chip's `MonoResponse`, by default `sonyMono` (the IMX585's: Hα 0.88, [O III] and Hβ 0.99, [S II] 0.86 of peak); a colour one its chip's `Response` ([presets](#camera-presets)), by default the IMX678's from ZWO's ASI678MC curve (manual V1.0, §3.3), where every dye passes some of every line:

| Line | red | green | blue |
|---|---|---|---|
| Hβ 486.1 | 0.04 | 0.94 | 0.79 |
| [O III] 500.7 (+ 495.9) | 0.05 | 0.97 | 0.53 |
| Hα 656.3, [N II] | 0.77 | 0.34 | 0.11 |
| [S II] 671.6/673.1 | 0.72 | 0.39 | 0.12 |

so through Hα the green pixels, half the array, collect almost as much as the red quarter, and through [O III] the blue quarter adds half of green's response.
- **An emission-line object's** light is its lines ([from its V](#emission-line-light)), which any band that passes them collects once: it uses the line response unfiltered and through broadband filters too, rather than the sky's flat spread, which would count it 3.4 times. Unfiltered it is dimmed at its lines' extinction, as in the score.

The object and sky per minute are the score's own (the sky with moonlight, zodiacal and galactic light, through a filter its bands' shares; the object dimmed by its band's extinction), with the surface brightness from `ExposureSB`, as absolute photons: the object's lines are not moved against the camera band, which the sky's gain already spans. In absolute photons the object loses the zenith's extinction too, 10^(−0.4kX) rather than the score's 10^(−0.4k(X−1)): its surface brightness is catalogued above the atmosphere, while the sky's is measured on the ground (about 20% of the light at κ = 0.24, some 1.3–1.4× the subs). A filter is the score's choice, worn all night, so its observable minutes are the score's too.

**Per sub and stacked.** A colour sensor's red, green and blue pixels (`channels`: ¼, ½ and ¼ of the RGGB array) see different light, and an optimally weighted stack adds them by SNR², each over its own pixels; a mono sensor is one channel of every pixel. With rates S_c and B_c per pixel of channel c, its share f_c, the minute's dark current D per pixel, a sub of t seconds and read noise RN,

```math
\mathrm{SNR}^2_\mathrm{sub} = \sum_c f_c\,\frac{(S_c t)^2}{S_c t + B_c t + D t + RN^2}\;\frac{1}{\Omega}\quad\text{per arcsec}^2
```

Each channel adds by its own response, so one that sees little of the object adds little, its read noise included: through Hα the colour IMX678 needs 0.88²/Σ f_c q_c² = 3.7 times a mono sensor's subs read-noise limited, and 0.88/Σ f_c q_c = 2.3 times sky limited (`TestSubsBayerChannels`). An average pixel, (¼S)²/(¼S + ¼B + RN²), would charge every pixel's read noise against the line, and putting each line on one dye's channel only, as this model once did, missed the green pixels' Hα (0.34 of peak against red's 0.77) and the blue ones' [O III] (0.53): it counted about half the Hα signal and three-quarters of the [O III].

The master dark's own noise is left out. Subs lost to the weather (each minute's quality $q_i$ of the score: cloud, transparency, rain, dew, gusts) must be shot again, so the weather weighs each minute, and cloud during the target's best minutes costs more than the same cloud during its worst.

**How the subs are stacked** (`-stack`). An optimally weighted stack (`weighted`, the default without `-scope`: PixInsight's or Siril's weighting) adds each sub's SNR², so a night half dark and half moonlit is the mean of its minutes' SNR². A smart telescope's live stack averages the subs it keeps with equal weights (`mean`, the default with `-scope`; no maker documents a weighting): the stack's SNR² is then (ΣS)²/Σσ², and a noisy moonlit sub dilutes it rather than adding a little. Per sub the night allows, with σ²_{c,i} the denominator above,

```math
m = \frac{1}{n\,\Omega}\sum_i q_i \sum_c f_c \frac{S_{c,i}^2}{\sigma_{c,i}^2}\quad(\text{weighted}),\qquad m = \frac{1}{n\,\Omega}\sum_c f_c \frac{\left(\sum_i q_i S_{c,i}\right)^2}{\sum_i q_i\,\sigma_{c,i}^2}\quad(\text{mean})
```

over the n observable minutes. Equal weights cost little where the Moon limit has already dropped the worst minutes: M 51 from Bortle 4 on 6 May 2026, the Moon rising in the night, needs 347 subs averaged against 305 weighted.

**The stack's ceiling** (`-residual`). Flats never match the sky exactly, and gradient removal leaves some of it: a residual ε of the sky (0.2% by default) that averaging does not remove and that spans many pixels, so it caps the stacked SNR per arcsec² at S/(εB) however many subs, the channels adding by share:

```math
\mathrm{SNR}^2_\mathrm{max} = \sum_c f_c \left(\frac{\sum_i q_i S_{c,i}}{\varepsilon \sum_i q_i B_{c,i}}\right)^2 ,\qquad \frac{1}{L^2} = \frac{1}{N_L\,m} + \frac{1}{\mathrm{SNR}^2_\mathrm{max}} \;\Rightarrow\; N_L = \left\lceil \frac{L^2/m}{1 - L^2/\mathrm{SNR}^2_\mathrm{max}} \right\rceil ,\qquad L = 5,\ 10,\ 20
```

A level at or past the ceiling is never reached: SUBS shows `never`, JSON the ceiling as `snrLimit`. It is what keeps a faint galaxy from a city out of reach rather than a matter of patience, and it favours filters, which cut B: IC 1795 at Bortle 9 reaches SNR 8.6 at most unfiltered, where Hα needs 297 subs. The counts are capped at 10⁹, which a night the forecast loses entirely reaches too (past tonight's limit, rather than no estimate). FILTER is chosen by the same N for the good level, so it stays the fewest-subs choice (`TestScoreFilterCameraModes`).

**Seeing and the optics' blur.** A small object's light is spread over the star image, which dims its surface brightness. Each minute the FWHM is the seeing (the [upper-air estimate](#seeing-estimate), which matches DIMMs better, else 7Timer's class, else 2.5″), grown with the airmass as X^0.6, the optics' diffraction 1.03 λ/D at 550 nm (2.3″ at 50 mm, 0.8″ at 152 mm) and a pixel's own blur, 0.68 of its width (a square pixel's variance p²/12 as a Gaussian FWHM), in quadrature; a uniform ellipse of axes a × b keeps about ab/√((a² + F²)(b² + F²)) of its surface brightness, half for a disc as wide as F. Only objects under about an arcminute move: a 12″ planetary nebula needs more subs under 4″ seeing than under 1″ (`TestSubsResidualSeeing`), and nothing at the default `-min-px` does. For a mosaic the counts are per panel, and the subs that fit tonight are shared among the panels: the observable time, or with `-plan` the observable minutes of the planned blocks (which then also set the mean per-sub SNR², so a block the target sets in or spends low counts as it is), divided by the sub length plus a second between subs (the download and the live stack), less the share `-reject` discards (0.15 by default: tracking, satellites and wind cost the Origin 10–20%). The counts are the subs the stack keeps, so rejection only lowers what tonight allows.

**Surface brightness.** The object's own (`SurfaceBrightness`), else the median of its class over the embedded lists (`TestTypicalSB` recomputes them): galaxies 22.52, planetary nebulae 21.10 (most from their Hα flux), emission nebulae 21.90, other nebulae 20.73 mag/arcsec², marked `~`; reflection nebulae 22.0, a guess, since no list gives a reflection nebula's own magnitude and no photometric survey that a cutout service resamples measures them: Pan-STARRS through CDS's HiPS is not calibrated (its zero point scatters 2.1 mag over 25 galaxies), SkyView's SDSS reaches few of them, and Legacy Survey DR10 is calibrated but sky-subtracted, putting galaxies 1.0 mag faint with 1.3 mag scatter, and covers 10 of them (median 23.3 after the galaxies' offset, too few and too noisy to replace the guess). Clusters (point sources), galaxy groups, comets, and dark nebulae and molecular clouds (unless imaged for the emission behind them, like the Horsehead) get none: the median of the bright nebulae that have a measurement would make the faintest objects quick.

**Limits.**

- An emission nebula's lines come from its V surface brightness through the [fitted conversion](#emission-line-light), good to a factor of about three for one nebula; without a measurement the class median (21.90 for emission nebulae) stands in, which with the lines 1.63 mag brighter puts the typical one at about twenty minutes of subs unfiltered on the Origin under the default pristine sky (IC 1795: 117 subs of 10 s). Galaxies come out a little quicker than Origin users report: M 51 reaches the good level in 123 subs of 10 s (21 minutes of subs, 27 with the gaps and rejected subs) under a 22.0 sky on 15 April 2026 from 45.8° N, against reports of about 35 minutes from dark sites.
- **Fainter objects' levels.** A fixed SNR makes a sky-limited stack grow as 1/S², two magnitudes fainter forty times longer, but what imagers report stacking grows about as 1/S: they stretch a faint object harder and judge its brighter structure, where the catalogue's mean over the D25 ellipse is dominated by a faint outer disc. So below a pristine sky's brightness (B_ref, 22.0 mag/arcsec², in the score's terms) the levels are √(S/B_ref) of 5, 10 and 20 (`scoring.levelScale`; JSON's `snrPerSub` and `snrLimit` are against these levels). Against reported totals from 45.8° N under 21.3 mag/arcsec², new Moon, 10 s subs, dual-band for the emission nebulae (S50 users' at AstroBin and Cloudy Nights, 2026):

  | Target | Seestar S50 | Origin | reported |
  |---|---|---|---|
  | M 42 (2 panels) | 1.4 h a panel | 0.15 h | 0.5–2 h; Origin 10 min – 2 h |
  | M 51 | 3.8 h | 0.45 h | 1.5–3 h; Origin about 35 min |
  | M 81 | 7.4 h | 0.9 h | 3–5 h |
  | M 101 | 22 h (was 150–290 h at a fixed SNR) | 2.8 h | 1.6–9 h |
  | M 33 (2 panels) | 15 h a panel (was 104 h) | 1.9 h | 3–6 h |
  | NGC 7000 (2 panels) | 6.5 h a panel | 0.7 h | 2–5 h |
  | Veil, NGC 6992 | 5.1 h | 0.6 h | 1–3 h |
  | Heart, IC 1805 (8 panels) | 2.8 h a panel | 0.3 h | 2–9.5 h |
  | Bubble, NGC 7635 | 3.2 h | 0.4 h | 2–4 h |

  The Origin lands a little under (about 0.8×) what its users report, the S50 at 1–4×, more for the faint galaxies: the good level is a clean image, where many stop at a noisier one, and reports come from every kind of sky. Treat SUBS as a clean image's count, not a minimum.
- At the Origin's 10 s a colour sensor's pixels through a 7 nm Hα band are only about sky limited under a dark sky (about a third of an e⁻ of sky per sub on the red ones under 21.3 mag/arcsec², with 0.8 e⁻ of dark current, against RN² ≈ 1.2), where a broadband filter, collecting every line over 120 nm, is clearly so. With framing, FILTER is chosen by this model, so it is the choice needing fewest subs: IC 1795 from 45.8° N on 14 October 2026 (`-scope origin -no-weather`, one `-filter` at a time, the sensor at 20 °C) under the default 22.0 zenith takes 117 subs unfiltered, as many through Hα (no better), 115 through dual-band and 85 through `cls`, so `cls`; at Bortle 4 170 unfiltered, 143 through Hα, 120 through dual-band and 106 through `cls`, so `cls`; at Bortle 5 to 7 dual-band; at Bortle 8 1399 unfiltered (SNR 21 at most, the residual's ceiling), 204 through Hα, 210 through dual-band and 484 through `cls`, so Hα. The dark current falls on every pixel whatever the filter, so it weighs most where a filter has cut the sky: before it was modelled the same night chose dual-band under the default zenith.
- No master dark noise, no hot pixels or amplifier glow, one sensor temperature offset for every smart telescope, one residual for every flat and sky, no saturation (the IMX678's 11.27 ke⁻ full well is far off at 10 s for these objects), flat spectra across each band, and one SNR target for every type.

## Seeing estimate

`weather.Seeing`. This is **display only**: it never changes a score.

7Timer's seeing class covers only about three days. For hours beyond that, the SEEING column shows an estimate (`~1.3"`) computed from the ECMWF IFS 0.25° upper-air forecast (Open-Meteo, 15 days ahead). The forecast gives temperature T, wind and height z at the surface and at 925 … 100 hPa.

**Optical turbulence per layer.** Each pair of adjacent levels at least 300 m apart is a layer. Its refractive index structure constant follows the AFGL radiosonde model of [Dewan et al. (1993)][dewan]:

```math
C_n^2 = 2.8\,M^2\,L_0^{4/3} ,\qquad
M = -79\cdot10^{-6}\,\frac{P}{T^2}\Big(\frac{dT}{dz} + 9.8\ \text{K/km}\Big)
```

```math
L_0^{4/3} = 0.1^{4/3}\cdot 10^{Y} ,\qquad
Y = \begin{cases} 1.64 + 42\,S & \text{troposphere} \\ 0.506 + 50\,S & \text{stratosphere}\end{cases}
```

- M is the vertical gradient of the potential refractive index, with P in hPa and T in K. The dry adiabatic lapse rate is added because an adiabatic layer mixes without changing refraction.
- S is the magnitude of the *vector* wind shear, |Δ**v**| / Δz in 1/s. A change in wind direction counts as well as a change in speed.

**Tropopause.** The tropopause decides which form of Y applies. It uses the WMO lapse-rate rule ([AMS Glossary][ams-tropopause]), searched from 500 hPa upward: the lowest level whose layer cools at 2 K/km or less, and whose next layer does too. The second layer stands in for the rule's 2 km average. Everything from that level upward is stratospheric. If no level qualifies, the whole profile is tropospheric.

**Sanity checks on the forecast levels.** These use the hypsometric equation ([AMS Glossary][ams-hypsometric]) with R/g = 29.3 m/K:

```math
\Delta z = 29.3\ \tfrac{\text{m}}{\text{K}}\cdot\bar T\cdot\ln\frac{p_1}{p_2}\quad(\pm 30\%) ,\qquad
p_\text{surface} \approx 1013.25\,e^{-z/H},\ H = 29.3\ \tfrac{\text{m}}{\text{K}}\cdot\bar T\quad(\pm 12\%)
```

A level that fails these checks, or has implausible values, is dropped. An hour needs the surface plus at least three pressure levels, one of them at or above 300 hPa so the jet stream is included.

**From Cn² to seeing.** The turbulence is integrated over height, with the calibration factors described below. That gives the Fried parameter r0 and the FWHM of a star image at λ = 500 nm at the zenith ([Fried 1966][fried]; [Roddier 1981][roddier]):

```math
r_0 = \Big(0.423\,k^2\!\int C_n^2\,dz\Big)^{-3/5},\quad k = \frac{2\pi}{\lambda} ,\qquad
\text{FWHM} = 0.98\,\frac{\lambda}{r_0}
```

An estimate outside 0.1–30″, or not a finite number, counts as unknown.

**Calibration.** Two biases push the raw estimate in opposite directions:

- Dewan's model was fitted to free-atmosphere radiosonde layers, and extended to the surface it overestimates the boundary layer ([Cuevas et al. 2024][cuevas]).
- The forecast levels are 0.7–2 km apart, which smooths the shear higher up and so underestimates the free atmosphere.

The integral therefore weights the lowest layer by 0.2 and every layer above it by 1.45:

```math
\int C_n^2\,dz \;\approx\; 0.2\,C_{n,\text{ground}}^2\,\Delta z_\text{ground} + 1.45\sum_\text{above} C_n^2\,\Delta z
```

The two factors are a least-squares fit (`scripts/calibrate_seeing.py`) against 12,222 hours of ESO DIMM seeing at Paranal and La Silla, compared with Open-Meteo's archived IFS runs (May 2024 to September 2026):

| | bias | RMSE | r |
|---|---|---|---|
| uncalibrated | +0.21″ | 0.49″ | 0.32 |
| calibrated | +0.03″ | 0.33″ | 0.41 |

The calibrated figures are in the same range as published model forecasts for Paranal ([Cuevas et al. 2024][cuevas]: 0.30″, r 0.41; [Osborn & Sarazin 2018][osborn]: 0.31″, r 0.30). The factors describe the model, not the weather, so they are refit only after a change to the model.

## Night plan

`internal/plan`, with `-plan`

The plan splits the window into blocks of `-plan` length and assigns at most one target to each block, using each target at most once. A block's value for a target is the sum of that target's per-minute weights w_i inside the block, times its frame score, so the per-minute scoring is not repeated. The assignment runs in two steps:

1. **Greedy**: go through the blocks in time order, and give each one the best target not yet used. Mean altitude in the block breaks ties.
2. **Local search** over the target × block score matrix, repeated until nothing improves:
   - swap the targets of two blocks,
   - move a target into another block and refill the block it left with the best unused target,
   - replace a block's target with a better unused one.

The result is a local optimum, not necessarily the best possible plan. The reported block score is the sum divided by the block's length in minutes.

## Constellations

`internal/constellation`

The lists' constellation labels contain errors and spelling variants, so the constellation is always recomputed from the position. The method follows [Roman (1987)][roman]:

- The IAU boundaries (Delporte 1930) are defined at the B1875.0 equinox ([VI/49][vi49], Davenhall & Leggett 1989). Each J2000 position is therefore precessed to B1875.0 with the IAU 1976 formula above, at `T = (2405889.258550475 − 2451545)/36525`.
- The boundary edges are arcs of constant right ascension or constant declination. A ray cast due north from the point counts how many constant-declination edges it crosses. An odd count means the point is inside.
- Ursa Minor winds around the pole, so its count is inverted.

The boundary table and the test come from [astrogo](https://github.com/TuSKan/astrogo).

## Local horizon

`internal/horizon`

A `-horizon` file lists (azimuth, altitude) points. The horizon altitude between two points is interpolated linearly in azimuth, wrapping across north:

```math
h_\text{hor}(A) = h_\text{lo} + (h_\text{hi} - h_\text{lo})\,\frac{(A - A_\text{lo}) \bmod 360}{\text{span}}
```

Two points at the same azimuth make a vertical step. A minute counts only when the target is above both `-alt-min` and the horizon.

`scripts/terrain2horizon.py` writes such a file from a digital elevation model (AWS Terrain Tiles, Terrarium encoding, bilinear between pixels). Along each azimuth it steps one pixel at a time from `--near` (100 m) to `--radius` (20 km) and keeps the steepest angle to the ground at distance d. The ground there is lowered by Earth's curvature, less the standard terrestrial refraction (k = 0.13):

```math
h_\text{hor}(A) = \max_d \arctan\frac{z(d) - z_0 - d^2 (1 - k) / 2R}{d}
```

z₀ is the site's model elevation (or `--elevation`) plus a 2 m eye height. A ridge must rise about 580 m per km to reach the default 30° floor, so the terrain matters mostly with a lower `-alt-min` in the mountains. Trees and buildings are not in the elevation model.

By default (`--no-osm` turns it off) the script adds OpenStreetMap's buildings (outlines), and with `--no-canopy` its single trees (3 m crowns, tree rows as trees every 6 m) and woods (outlines), within `--osm-radius` (300 m), fetched from the Overpass API. Each has its `height` tag, else `building:levels` × 3 m + 2 m of roof, else a default: 8 m for a building, 12 m for a tree, 20 m for a wood. Along each azimuth every crossing of an outline or crown at distance d gives

```math
h_\text{obs}(A) = \max \arctan\frac{z(d) - z_\text{site} + h - 2\ \text{m}}{d}
```

with the obstacle standing on the terrain model's ground z(d), measured from the model's ground at the site, so an `--elevation` override moves the eye only. The horizon is the higher of the terrain's and the obstacles'. An outline holding the site (the house you stand by, a wood you stand in) is left out, and so are inner rings, since a ray's steepest crossing is the outer edge it meets first. The roof is flat at h, so a pitched roof's ridge seen end-on is overstated. An obstacle blocks the default 30° floor when its top is more than 0.58 d above the eye: an 8 m house within 10 m, a 20 m wood edge within 30 m.

By default (`--no-canopy` turns it off) the trees come from the Meta/WRI canopy height map instead of OSM ([Tolan et al. 2024][chm]): 1.19 m Web Mercator pixels (0.84 m at 45°), heights in whole metres, estimated from 2009–2020 Maxar imagery with a mean absolute error of about 2.8 m against airborne lidar. The script reads only the rows around the site from the Cloud-Optimized GeoTIFF tiles (zoom-9 quadkeys, 65536 px square, BigTIFF, Deflate with the horizontal predictor, one row a strip) by HTTP range requests, and marches each azimuth every half pixel from `--canopy-near` (3 m, past a tree you stand under) to `--canopy-radius` (300 m), with the same angle as above for any pixel at least 1 m tall (lower is the map's noise over open ground). OSM then gives the buildings only. Near Zagreb's Maksimir wood, 91% of the pixels inside OSM's wood outlines are at least 5 m tall against 17% outside, and a mirrored or 60 m shifted grid agrees far worse, which checks its registration.

## Heuristics

The models above come from the literature. The following are this project's own judgment, chosen to rank nights sensibly rather than to measure anything:

| Heuristic | Value | Reasoning |
|---|---|---|
| Moon-distance rule | ≥ illumination % in degrees (filtered: × f_R/w², 0.3–1) | uptonight's rule; skipped with the Moon down; the floor is the halo and gradients near the Moon, which no scattering model here holds |
| A filter for the whole night | each choice scored as worn every minute, the one that gains most kept (the most uncapped weight; with a camera, the best mean per-sub SNR² of the sub-exposure model over its own observable minutes, the fewest-subs choice) | a filter changed mid-target means a second set of flats and a split stack |
| Altitude window | 30°–80° | uptonight's limits; ≥ 30° keeps the airmass ≤ 2 |
| Emission line ratios | one set per type ([Nebula filters](#nebula-filters)) | single objects scatter by factors of a few; no list has line fluxes |
| Hβ | Hα/2.86 for every type | case-B recombination; dust raises the ratio, so dusty H II regions' Hβ is overstated |
| Emission-line light from V | V/f_V less c = 0.99 mag of stars and continuum | fitted to Finkbeiner's Hα map over 15 nebulae of 30′ and more (interquartile 0.22–1.92 mag), almost all H II regions; for other types f_V carries the difference |
| Airglow with the solar cycle | one slope (0.006/sfu) and a 0.6 share of the natural 22.0 for every site, the monthly flux | measured swings run 0.3–1.0 mag by site and cycle; the airglow also changes through the night and the seasons by up to a few tenths, and DarkSkySites' handling of the natural sky is undocumented |
| Colour sensor response | each preset's red, green and blue curves digitized from its maker's chart (`scripts/qe_digitize.py`, about ±1 point), each line at its dye's response, each channel's continuum its mean over 400–700 nm; a camera given by `-sensor`/`-pixel` takes the IMX678's | vendor curves, possibly above the delivered sensor's, some reused across chips; the moonlight's colour is weighted as over the whole band for every channel, not by each dye |
| Optics throughput | refractor 0.88, prime-focus mirror 0.81, another telescope 0.85 | coated surfaces, aluminium and the sensor window by design; no maker publishes its scope's; `-throughput` sets it |
| Uncooled sensor temperature | the air + 10 °C, the air 10 °C without a forecast | Seestar's battery runs about 10 °C above the air in the same housing; no smart telescope publishes its sensor's; `-sensor-temp` sets a cooled one's |
| Dark current | the chip's at 25 °C from ZWO's chart, else the charted median 0.16 e⁻/s, doubling every 5 °C | the charts' slope between 15 and 30 °C; the IMX678 and smaller chips have no reliable chart |
| Stacking | equal weights with `-scope`, weighted by SNR² otherwise | live stacks average what they keep; desktop stackers weight |
| Stack ceiling | a residual of 0.2% of the sky (`-residual`) | flats and gradient removal match the sky to 0.1–0.5%; one value for every rig |
| Blur | seeing (else 2.5″) × X^0.6, diffraction and 0.68 of a pixel in quadrature; a uniform ellipse | amateur sites see 2–3″; tracking and focus left out |
| Rejected subs and gaps | 15% of subs, 1 s between subs | the Origin loses 10–20% to tracking, satellites and wind; the download and live stack take about a second; `-reject` sets the share |
| Camera presets | 22 chips and 21 smart telescopes, the maker's peak QE (often an estimate) and read noise at its deep-sky gain (the OS08B10's from OmniVision's Nyxel 2 chart and one community measurement) | another gain changes the read noise (`-read-noise` sets it); a version without a published peak QE is left out |
| Filter band shift | `-filter-std`: top-hat bands of the nominal width, one effective index (2.05) for every filter | real filters' edges slope and their indices differ by coating (Chroma's 5 nm Hα measures 0.39 at f/2 where the model gives 0.53); filters sold for fast optics are assumed to pass their lines whole |
| Filter band width | every band `-filter-nm` wide | real tri- and quad-band filters mix widths (the L-eNhance's 24 nm over Hβ and [O III]); one width keeps the flag simple |
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
| Transparency ramp | 7Timer 1 → 8, × 1 → 0.5, only without CAMS aerosols | haze dims but does not stop imaging; CAMS's is in the extinction |
| Aerosol scale height | 1.5 km, from the IFS cell's ground up | Hayes & Latham (1975); CALIOP 1.5–2 km over land |
| SUBS levels for a faint object | Levels × √(S/B_ref) below a pristine sky's brightness | reported totals grow as 1/S, not a fixed SNR's 1/S² ([Sub-exposure estimate](#sub-exposure-estimate)) |
| Frame fill | full credit at 25–80% of the short side | small objects lack detail, tight ones lack margin |
| Mosaic | 10% overlap, score ÷ panels | panels need overlap to register; the time is shared |
| Forecast gaps | mean of the covered hours | neither optimistic nor pessimistic |

Treat SCORE as a way to order tonight's options, not as an absolute measure of quality.

## References

| Topic | Source |
|---|---|
| Sun, Moon low-precision formulas, obliquity | *The Astronomical Almanac*, [sections C][aa-sun] and [D][aa] |
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
| Emission-line light from V | Hα map: [Finkbeiner (2003)][finkbeiner], ApJS 146, 407, through NASA SkyView; V passband: [Bessell (1990)][bessell] |
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

[aa]: https://aa.usno.navy.mil/publications/asa
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
