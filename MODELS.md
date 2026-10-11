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
w_i \;=\; \underbrace{Q_i}_{\text{weather}} \;\times\; \underbrace{T(h_i,\kappa_i)}_{\text{extinction}} \;\times\; \underbrace{W_{\text{sky}}(S, B_i, k)}_{\text{sky glow vs. object}}
```

```math
\text{SCORE} \;=\; F_{\text{frame}} \cdot \frac{1}{N}\sum_{i\,\in\,\text{observable}} w_i ,
\qquad
\text{FOTO} \;=\; \frac{\#\,\text{observable minutes}}{N}
```

N is the number of minutes in the window. Each factor lies between 0 and 1, so a perfect target (up all night, clear, moonless, at the zenith, well framed) scores 1. The sections below derive each factor:

| Symbol | Meaning | Section |
|---|---|---|
| h | target altitude | [Time and positions](#time-and-positions) |
| Q | weather quality (cloud, rain, dew, gusts, transparency) | [Weather](#weather) |
| κ | extinction coefficient, mag per airmass | [Atmosphere and sky](#atmosphere-and-sky) |
| T | fraction of light surviving extinction, relative to the zenith | [Atmosphere and sky](#atmosphere-and-sky) |
| B | sky brightness at the target (light pollution + moonlight) | [Atmosphere and sky](#atmosphere-and-sky) |
| S | the object's own surface brightness | [Object surface brightness](#object-surface-brightness) |
| k | fraction of sky glow that counts against the target | [Scoring a target](#scoring-a-target) |
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

The latitude is 0, and the ecliptic is rotated to the equator by the obliquity `ε = 23.439° − 0.0000004° n`:

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

## The night window

`astro.Window`, `astro.Tonight`, `astro.ClipWindow`

The night is the time while the Sun's geometric centre is below −18° (astronomical twilight), or −12° with `-twilight nautical` ([USNO definitions][usno-twilight]). The program does not solve for the crossing times. It steps minute by minute for 24 hours from the site's **solar noon**, `12:00 UTC − λ/15 h`, taking the solar noon closest to local noon. Starting from solar noon rather than local midnight has three benefits:

- A time zone that does not match the site cannot cut the night in two.
- Zones across the date line (Samoa, Kiritimati) get their own night rather than the next one.
- Polar night is capped at 24 hours.

If the Sun is already below the limit at noon and rises above it soon after, that darkness is the end of the previous night and is skipped. If the Sun never gets below the limit, there is no night (high-latitude summer). Without `-date`, a run during the night plans only what remains of it. `-from`/`-to` can only narrow the window.

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

The illumination, phase angle and distance are taken once, at the middle of the window, and drive both the Moon-distance rule and the moonlight model below.

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
- **Aerosols**: the hourly AOD from the [CAMS][cams] forecast via Open-Meteo. The factor 1.086 = 2.5 log₁₀ e converts optical depth to magnitudes.

Past the CAMS range (about 5 days), a typical AOD of 0.1 is used. The elevation comes from the CAMS response, else from the weather forecast's, whose range is longer. When neither has it, or when `-extinction` is given, that fixed value (default 0.2) is used instead. A smoky or dusty night therefore penalises low targets and spreads moonlight more widely.

### Sky brightness: Krisciunas & Schaefer (1991)

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
| mag/arcsec² | 21.9 | 21.6 | 21.4 | 20.85 | 19.75 | 18.8 | 18.25 | 17.75 | 17.3 |

These are the midpoints of the SQM ranges usually quoted for [Bortle's (2001)][bortle] classes.

**Towards the horizon** the sky brightens because the line of sight passes through more glowing air, and dims through extinction. K&S model this with a scattering airmass X_s at zenith distance Z = 90° − h:

```math
X_s(h) = \big(1 - 0.96\sin^2 Z\big)^{-1/2} ,\qquad
B_{\text{dark}}(h) = B_{\text{zen}}\cdot 10^{-0.4\,\kappa\,(X_s-1)}\cdot X_s
```

**Scattered moonlight** is added while the Moon is above the horizon. The Moon's brightness outside the atmosphere depends on its phase angle α (degrees, 0 = full):

```math
I^* = 10^{-0.4\,(3.84 + 0.026\,\alpha + 4\cdot10^{-9}\alpha^4)} \cdot \Big(\frac{60.27}{r}\Big)^2 \cdot
\begin{cases} 1.35 - 0.05\,\alpha & \alpha < 7^\circ \\ 1 & \text{otherwise}\end{cases}
```

- The `(60.27/r)²` term scales from K&S's mean distance to the Moon's actual distance, which varies by ±13%.
- The last factor is the *opposition surge*, the extra brightening near full Moon that K&S's I* leaves out. It is modelled as in Thorstensen's skycalc ([thorsky][thorsky]).

That light is dimmed on its way down through the Moon's own airmass, scattered towards the target by the scattering function f(ρ), and added along the target's line of sight:

```math
f(\rho) = 10^{5.36}\,(1.06 + \cos^2\rho) + 10^{\,6.15 - \rho/40}
```

```math
B_{\text{moon}} = f(\rho)\cdot I^*\cdot 10^{-0.4\,\kappa\,X_s(h_\text{moon})}\cdot\big(1 - 10^{-0.4\,\kappa\,X_s(h)}\big)
```

ρ is the Moon–target angle in degrees. In f(ρ), the first term is Rayleigh scattering by air molecules and the second is Mie scattering by aerosols, strongly forward-peaked. The total is

```math
B = B_{\text{dark}} + B_{\text{moon}}
```

The table's SKY column is this B converted back to mag/arcsec² and averaged over the observable minutes. The part of B_moon that does not depend on the target (I* times the Moon's extinction) is computed once per grid minute (`atmos.MoonLight`).

## Object surface brightness

`catalog.Target.SurfaceBrightness`

What sky glow does to an extended object depends on how bright the object is **per square arcsecond**, not on its total magnitude ([SkEye's explanation][skeye]). Each target's V surface brightness S is taken from the first rule that applies:

1. **Measured V.** The Compendium's V mean surface brightness (`surfbr`). Exact.
2. **Measured B, moved to V.** [OpenNGC][openngc]'s B mean surface brightness within the 25 mag/arcsec² isophote (`bsurfbr`), minus a colour. The colour is the object's own `B − V` when it lies in −0.3…1.5 (OpenNGC pairs inconsistent magnitudes for about 100 objects). Otherwise it is a typical colour for the kind of object, and the value is marked `~`:

   | Kind | typical B − V |
   |---|---|
   | galaxies, galaxy groups | 0.8 (median offset of Compendium V vs OpenNGC B over 723 shared galaxies) |
   | emission-line objects | 0 |
   | everything else | 0.5 |

3. **Derived from magnitude and size.** For single galaxies and nebulae only. Marked `~`. The integrated V magnitude m (or B minus the typical colour) is spread over a uniform disc with diameter equal to the major axis (size in arcminutes):

   ```math
   S = m + 2.5\log_{10}\!\Big(\frac{\pi}{4}\,(60\cdot\text{size})^2\Big)
   ```

   An elongated galaxy is narrower than that disc, so the result leans faint.
4. **Unknown** (S = 0) in every other case:
   - Star clusters, even with a measured value: their light sits in points, which the cluster factor k = 0.5 already accounts for, and MWSC sizes are the core only.
   - Galaxy groups: their magnitude is one member's, but their size is the whole group's.
   - Clusters with nebulosity: their magnitude is the stars', not the glow's.
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
- **Gusts**: from 20 to 40 km/h cost up to half.
- **Rain**: the hour's precipitation ramps Q down to 0 at 0.1 mm, whatever the cloud layers say; an unknown amount does nothing. Open-Meteo reports gusts and precipitation over the *preceding* hour, so each hour reads the next entry.
- **Transparency**: [7Timer!][7timer]'s ASTRO product (1 best, 8 worst; 3-hourly, about 3 days ahead) scales Q from 1 down to 0.5.

When a forecast covers only part of the window, the uncovered minutes get the **mean of the covered ones**. A perfect sky would favour targets that are up only then. With no forecast at all, Q = 1. Weather only weighs the score. FOTO and the observable runs stay purely geometric.

The ramps are heuristics: the thresholds are judgment, not measurement (see [Heuristics](#heuristics)).

## Scoring a target

`internal/scoring`

### Which targets are scored

Before any minute is evaluated, a target is dropped when its major axis is outside `-size-min`…`-size-max` (an unknown size counts as 0; comets are exempt), when it is outside the `-ra`/`-dec` region, or when `-skip` names it. The region is a box of ±`-tol` degrees on each axis around the J2000 position, with RA counted at 15° per hour and wrapping at 24h, not scaled by cos δ.

### Is this minute observable?

A minute counts when all of these hold (the rules come from [uptonight][uptonight]):

1. `-alt-min ≤ h ≤ -alt-max`, 30°–80° by default. Being above 30° also guarantees an airmass ≤ 2.
2. h is above the local horizon profile at the target's azimuth, when `-horizon` is given.
3. **The Moon-distance rule**: while the Moon is above the horizon, the target is at least `k × (illumination in %)` degrees from it. For example, a 60%-lit Moon keeps a broadband target 60° away and a filtered emission nebula (k = 0.25) 15° away. Unlike uptonight, the rule is skipped while the Moon is down.

The longest unbroken stretch of observable minutes is reported as OBSERVABLE. A target whose longest stretch is shorter than `-min-run` is dropped.

### The sky-glow fraction k

Not every target suffers equally from a bright sky, so a fraction k of the sky glow is counted against it (`skyK`):

| Target | k | Why |
|---|---|---|
| galaxies, reflection and dark nebulae, everything else | 1 | faint broadband light, fully exposed to the sky |
| open and globular clusters (without nebulosity) | 0.5 | bright point sources stand out of the glow |
| emission-line targets with `-filter` | `-filter-k` (0.25) | a narrowband filter passes the emission lines but blocks most of the sky |

The emission-line targets are emission, planetary and Wolf-Rayet nebulae, supernova remnants, and a few named objects whose catalogue type hides their Hα, such as IC 1396. k scales both the Moon-distance rule and the sky term below.

### The sky-glow weight

In a fixed exposure, the signal-to-noise ratio of a surface of brightness S over a sky B goes as

```math
\text{SNR} \propto \frac{S\,t}{\sqrt{(S + B)\,t}}
```

The weight compares tonight's SNR with the SNR the same object would have under a pristine 22.0 mag/arcsec² sky, B_ref. All quantities are in nanoLamberts:

```math
W_{\text{sky}} = \min\!\Bigg(1,\ \sqrt{\frac{S + B_{\text{ref}}}{S + k\,B}}\Bigg)
```

- **An object much brighter than the sky** (S ≫ B) gets W ≈ 1. It loses nothing to moonlight.
- **A faint object** (S ≪ B) is *sky-limited*: `W ≈ √(B_ref / (k B))`. Every target with unknown surface brightness (S = 0) falls in this case.

A faint galaxy 30° from a bright Moon therefore scores much lower than one 120° away, while a filtered emission nebula keeps most of its score.

### Putting it together

```math
w_i = Q_i \cdot T(h_i,\kappa_i)\cdot W_{\text{sky},i} ,\qquad
\text{SCORE} = F_{\text{frame}}\cdot\frac{1}{N}\sum_i w_i
```

Results are sorted by SCORE, and the mean altitude over the observable minutes breaks ties. Extinction does not also dim S. It enters once, as the separate factor T.

## Framing and mosaics

`scoring.frame`, `internal/config`

Framing is turned on by `-origin`, `-fov`, `-scale`, or `-focal` with `-sensor`/`-pixel`.

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

## Heuristics

The models above come from the literature. The following are this project's own judgment, chosen to rank nights sensibly rather than to measure anything:

| Heuristic | Value | Reasoning |
|---|---|---|
| Moon-distance rule | ≥ illumination % in degrees, × k | uptonight's rule; skipped with the Moon down |
| Altitude window | 30°–80° | uptonight's limits; ≥ 30° keeps the airmass ≤ 2 |
| Cluster sky fraction | k = 0.5 | point sources stand out of the glow |
| Filter sky fraction | k = 0.25 (`-filter-k`) | ~0.15 for ≤ 4 nm filters, ~0.4 for wide dual-band ones |
| High cloud weight | ½ | thin cirrus dims rather than blocks |
| Dew ramp | spread 4 → 1 °C, × 1 → 0.7 | dew on optics ends sessions |
| Gust ramp | 20 → 40 km/h, × 1 → 0.5 | shaken frames get discarded |
| Rain gate | 0 → 0.1 mm/h, × 1 → 0 | nobody images in rain |
| Transparency ramp | 7Timer 1 → 8, × 1 → 0.5 | haze dims but does not stop imaging |
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
| Sky brightness and moonlight | [Krisciunas & Schaefer (1991)][ks], PASP 103, 1033; opposition surge after [thorsky][thorsky] |
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
[thorsky]: https://github.com/jrthorstensen/thorsky
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
[roman]: https://doi.org/10.1086/132034
[vi49]: https://vizier.cfa.harvard.edu/viz-bin/VizieR?-source=VI/49
