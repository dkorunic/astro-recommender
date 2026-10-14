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
| B | sky brightness at the target (light pollution + zodiacal and galactic light + moonlight) | [Atmosphere and sky](#atmosphere-and-sky) |
| S | the object's own surface brightness | [Object surface brightness](#object-surface-brightness) |
| k | fraction of sky glow that counts against the target | [Scoring a target](#scoring-a-target) |
| w, f | through a `-filter`: the target's share of light in its bands, and the bands' share of each part of the sky (replacing k) | [Nebula filters](#nebula-filters), [Broadband filters](#broadband-filters) |
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

The three parts are kept apart (`atmos.RayleighExtinction`, `OzoneExtinction`, `AerosolExtinction`) because they scale differently with wavelength, which the [filter bands](#nebula-filters) use. A fixed `-extinction` is split the same way: ozone first, Rayleigh up to its sea-level 0.1066, the rest aerosol (`atmos.SplitExtinction`).

Past the CAMS range (about 5 days), a typical AOD of 0.1 is used. The elevation comes from the CAMS response, else from the weather forecast's, whose range is longer. When neither has it, or when `-extinction` is given, that fixed value (default 0.2) is used instead. A smoky or dusty night therefore penalises low targets and spreads moonlight more widely.

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
| mag/arcsec² | 21.9 | 21.6 | 21.4 | 20.85 | 19.75 | 18.8 | 18.25 | 17.75 | 17.3 |

These are the midpoints of the SQM ranges usually quoted for [Bortle's (2001)][bortle] classes.

**Towards the horizon** the sky brightens because the line of sight passes through more glowing air, and dims through extinction. K&S model this with a scattering airmass X_s at zenith distance Z = 90° − h:

```math
X_s(h) = \big(1 - 0.96\sin^2 Z\big)^{-1/2} ,\qquad
B_{\text{dark}}(h) = B_{\text{zen}}\cdot 10^{-0.4\,\kappa\,(X_s-1)}\cdot X_s
```

(The zodiacal and galactic light of the target's direction then add a term of their own, below.)

**Zodiacal light.** The natural sky is not the same in every direction: sunlight scattered by interplanetary dust brightens it along the ecliptic, most towards the Sun, and least at the ecliptic poles. [Leinert et al. (1998)][leinert] Table 17 gives its brightness Z(Δλ, β) at 500 nm by the ecliptic longitude from the Sun Δλ and the ecliptic latitude β, from 77 (in 10⁻⁸ W m⁻² sr⁻¹ µm⁻¹) at the pole to 259 on the ecliptic 90° from the Sun; 1 S10 unit is 1.28 of those and 27.78 mag/arcsec² in V, so the pole reads 23.3 mag/arcsec², the ecliptic at 90° 22.0, and the gegenschein opposite the Sun 22.2. The zenith brightness above already includes the zodiacal light of a typical zenith: a mid-latitude site's zenith runs over β = 45° ± 23.4° and Δλ from 90° at dusk to 180° at midnight, so the value is taken to include Z at the reference direction (Δλ, β) = (135°, 45°), which reads 90, and is corrected per target by the difference (`atmos.ZodiacalExcess`; the heliocentric ±7% and seasonal ±10% factors are left out):

```math
B_{\text{zen}}(\text{target}) = \max\big(B_{\text{zen}} + Z(\Delta\lambda,\beta) - Z(135^\circ, 45^\circ),\ Z_{\text{pole}}\big)
```

in nanoLamberts, bilinearly interpolated in the table, with the pole value past β = 75° and the blank cells near the Sun (which no night-time direction reaches) filled from the row's first tabulated latitude. The target's ecliptic coordinates come from its mid-window position of date (a comet's too) and the Sun's longitude at mid-window. On a pristine sky this makes the ecliptic 90° from the Sun 0.5 mag/arcsec² brighter than the reference and the pole 0.06 darker: a faint galaxy in Virgo or Leo in the evening sees a brighter sky than one in Ursa Major, and its sky-limited weight is 15–25% lower. Under light pollution the same nanoLamberts hardly show. The floor is the pole's own light: no configured sky gets darker than that (with the galactic light below, than the pole's light plus the darkest galactic light).

**Galactic light.** The Milky Way's unresolved stars, and the starlight its dust scatters (the diffuse galactic light, 20–30% of it), are the natural sky's other large variation. Pioneer 10 mapped them from beyond 2.8 AU, where the zodiacal light has gone: [Leinert et al. (1998)][leinert] Tables 35 and 36 (from Toller 1981) give that background starlight G(l, b) in blue (440 nm) and red (640 nm), every 10° of galactic longitude l by galactic latitude b (±80° in steps of 4° near the plane, 10° beyond), in S10⊙ units with the stars brighter than V 6.5 removed, as an imaging camera resolves them. A source of solar colour reads the same in both, so V (550 nm) is taken between them by wavelength in log brightness, G_V = G_blue^0.45 · G_red^0.55 (red reads 1.3 times blue at high latitude, the galactic light being redder than the Sun). Pioneer's spin axis kept it off 28 cells around the Sun (l 180–240°, b 16–50°); they are filled along each row's latitude, log-linearly between the nearest measured cells, where the light falls off smoothly towards the anticentre. G_V runs from 29 S10 at the galactic poles (23.4 mag/arcsec²) to 900 in the Sagittarius star clouds (20.4), against 205 for a whole 22.0 mag/arcsec² sky. The zenith brightness is taken to include the table's median over the sky by solid angle, 81 S10, what a zenith sees over a year's nights, and is corrected per target by the difference (`atmos.GalacticExcess`), the zodiacal light's beside it:

```math
B_{\text{zen}}(\text{target}) = \max\big(B_{\text{zen}} + Z(\Delta\lambda,\beta) - Z(135^\circ, 45^\circ) + G_V(l,b) - G_{\text{ref}},\ Z_{\text{pole}} + G_{\min}\big)
```

bilinearly interpolated, the ±80° rows going to their mean at the poles. The galactic coordinates come from the target's mid-window position (`astro.Galactic`, the IAU 1958 frame in J2000, within 0.005″ of SOFA's). On a pristine sky the galactic poles are 0.3 mag/arcsec² darker than the reference, the plane 0.5–1.1 brighter, the Sagittarius clouds 1.7 and the LMC 0.4: a faint galaxy at high galactic latitude sees the darkest natural sky, and a nebula in the Cassiopeia Milky Way about a magnitude brighter one. Under light pollution, again, it hardly shows. Pioneer's ~2° beam smooths the star clouds, and its red band carries some Hα, so the brightest H II regions' cells read high. The floor adds the darkest galactic light to the zodiacal pole's.

**Off the zenith** the two are not brightened as the rest of the dark sky is. K&S's X_s stands for the longer path through glowing air (the airglow, and the light pollution scattered in it), while the zodiacal and galactic light come from beyond the atmosphere, which only dims them. So their excess over what the configured zenith includes, E = B_zen(target) − B_zen, is added outside the dark-sky term and dimmed by extinction alone, at the Pickering airmass X of the target's own light:

```math
B_{\text{dark}}(h) = B_{\text{zen}}\cdot 10^{-0.4\,\kappa\,(X_s-1)}\cdot X_s + E\cdot 10^{-0.4\,\kappa\,(X-1)}
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

ρ is the Moon–target angle. The last factor turns per-steradian into per-arcsec². δ = 0.078 is Winkler's V-band offset: it carries the particles' albedo and the light scattered more than once. g and δ are his fit at Sutherland (SAAO), where the model reproduces the moonlit sky mostly within 0.05 mag. Against K&S, the full Moon 60° up puts 18.6 mag/arcsec² at a target 60° up and 90° away (K&S: 18.45). The sky is 0.7–1 mag brighter within about 10° of the Moon (the aerosol forward peak), a little brighter under a Moon below about 12° (0.06 mag at 10°, 0.15 at 5°), and about 0.15 mag darker at moderate separations. Aerosols range over 0.5 < g < 0.8 ([Andrews et al. 2006][andrews]), so under heavy haze (κ ≳ 0.3) a narrower-than-real forward peak likely makes the sky far from the Moon somewhat too dark; there it also gets darker with more haze, where K&S's grew brighter. The total is

```math
B = B_{\text{dark}} + B_{\text{moon}}
```

The table's SKY column is this B (with the zodiacal and galactic light) converted back to mag/arcsec² and averaged over the observable minutes. The part of B_moon that does not depend on the target (F*, the Moon's airmass and beam transmission, and the Rayleigh share) is computed once per grid minute (`atmos.MoonLight`).

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

Before any minute is evaluated, a target is dropped when its major axis is outside `-size-min`…`-size-max` (an unknown size counts as 0; comets are exempt), when it is outside the `-ra`/`-dec` region, when `-skip` names it, or when it is of no `-type` class (`catalog.Target.Is`: a galaxy group counts as a galaxy, a cluster with nebulosity as both cluster and nebula, `emission` is the emission-line targets below). The region is a box of ±`-tol` degrees on each axis around the J2000 position, with RA counted at 15° per hour and wrapping at 24h, not scaled by cos δ.

### Is this minute observable?

A minute counts when all of these hold (the rules come from [uptonight][uptonight]):

1. `-alt-min ≤ h ≤ -alt-max`, 30°–80° by default. Being above 30° also guarantees an airmass ≤ 2.
2. h is above the local horizon profile at the target's azimuth, when `-horizon` is given.
3. **The Moon-distance rule**: while the Moon is above the horizon, the target is at least `k × (illumination in %)` degrees from it. For example, a 60%-lit Moon keeps a broadband target 60° away and a cluster 30° away; through a filter, an emission-line target's limit shrinks by how much moonlight the filter's bands pass against its own light in them ([Nebula filters](#nebula-filters)): 11° for an H II region through a 7 nm dual-band filter, 5° through Hα alone. Unlike uptonight, the rule is skipped while the Moon is down.

The longest unbroken stretch of observable minutes is reported as OBSERVABLE. A target whose longest stretch is shorter than `-min-run` is dropped.

### The sky-glow fraction k

Not every target suffers equally from a bright sky, so a fraction k of the sky glow is counted against it (`skyK`):

| Target | k | Why |
|---|---|---|
| galaxies, reflection and dark nebulae, everything else | 1 | faint broadband light, fully exposed to the sky |
| open and globular clusters (without nebulosity) | 0.5 | bright point sources stand out of the glow |

k scales both the Moon-distance rule and the sky term below. A `-filter` replaces it with the bands' share of each part of the sky: a line filter for emission-line targets ([Nebula filters](#nebula-filters)), a broadband one for any target, keeping k as a factor ([Broadband filters](#broadband-filters)).

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

### Nebula filters

`-filter dual|tri|quad|ha|oiii|sii` (`scoring.passes`, `atmos.SkyParts`, `atmos.BandSky`, `catalog.Target.Lines`) scores the emission-line targets through a filter: emission, planetary and Wolf-Rayet nebulae, supernova remnants, and a few named objects whose catalogue type hides their Hα, such as IC 1396. A dual-band filter passes Hα and [O III] onto one frame, a tri-band one Hβ too, a quad-band one Hβ and [S II] too; the others pass one line. Every band is the same width, so a commercial multi-band filter with one wide band (the L-eNhance's 24 nm over Hβ and [O III]) is only approximated. Each band is `-filter-nm` wide (7 nm by default).

**The sky in a band.** The sky splits into three parts with different spectra:

- the **glow** (airglow and light pollution, with the zodiacal and galactic light: the dark-sky term B_dark),
- moonlight scattered by **molecules** (Rayleigh, the χ term of p(ρ)), going as λ⁻⁴,
- moonlight scattered by **aerosols** (Mie, the Henyey–Greenstein term), going as λ^−1.3 ([Ångström][angstrom]).

The model gives them in the V band, centred at 551 nm and 88 nm wide ([Bessell 1990][bessell]). A band Δλ wide at λ passes the fractions

```math
f_{\text{glow}} = \frac{\Delta\lambda}{88},\qquad
f_{\text{R}} = \frac{\Delta\lambda}{88}\Big(\frac{\lambda}{551}\Big)^{-4},\qquad
f_{\text{M}} = \frac{\Delta\lambda}{88}\Big(\frac{\lambda}{551}\Big)^{-1.3}
```

so per nm the [O III] band (500.7 nm) gets 2.9 times the Rayleigh moonlight of the Hα one (656.3 nm). Moonlight is taken as flat per nm before scattering: the Moon is 0.25 mag redder than the Sun in B−V ([Thejll et al. 2014][moon-colour]), which roughly offsets the Sun's own fall from 500 to 670 nm. The glow is taken as flat too: its spectrum depends on the site's lamps (sodium, white LEDs), which nothing here knows; flat is white LEDs, the case [broadband filters](#broadband-filters) gain least in. A multi-band filter's fractions are the sums over its bands.

**The target's light in the bands.** No catalogue gives line fluxes, so each type gets typical ratios to Hα (`catalog.Target.Lines`), normalised to shares:

| Type | Hα | [O III] 500.7 | [S II] 671.6/673.1 | Basis |
|---|---|---|---|---|
| H II region (and every other emission-line target) | 1 | 0.3 | 0.12 | [S II]/Hα ≈ 0.12 on average; [O III] weak outside bright cores |
| planetary nebula | 1 | 2.8 | 0.05 | [O III]/Hβ 6–9 (Magellanic Cloud averages), Hα = 2.86 Hβ |
| supernova remnant | 1 | 0.5 | 0.6 | [S II]/Hα > 0.4 defines them ([Mathewson & Clarke 1973][mathewson]); [O III] varies |
| Wolf-Rayet nebula | 1 | 1 | 0.2 | [O III]-bright shells |

Hβ (486.1 nm) is Hα/2.86 for every type, the case-B recombination ratio at 10⁴ K ([Osterbrock & Ferland 2006][agn2]; dust only raises it). It is added on top of the shares rather than normalised into them, so w can exceed 1 through `tri` or `quad`; the reference light is Hα + [O III] + [S II], and adding Hβ left the other filters' scores unchanged.

The share in the filter's bands is w (an H II region: 0.70 through Hα, 0.92 through dual-band, 1.16 tri-band, 1.25 quad-band; a planetary: 0.73 through [O III]). [N II] 654.8/658.3, inside most Hα bands, is left out.

Each extra band also passes its share of the sky, so the multi-band filters score close to `dual`: sky-limited under the glow alone (w²/f_glow, against 1 for broadband) an H II region gets 0.42 through `dual`, 0.45 `tri`, 0.39 `quad`; a planetary 0.49, 0.39, 0.30; a remnant, for its [S II], 0.26, 0.26, 0.34. Their use is colour on a one-shot-colour camera, which no score here measures.

**The weight.** The target's signal through the filter is w·S, the sky the bands' share of each part. Against the same pristine-sky reference as broadband:

```math
W_{\text{filter}} = \min\!\Bigg(1,\ \sqrt{\frac{w^2\,(S + B_{\text{ref}})}{w\,S + f_{\text{glow}}B_{\text{glow}} + f_{\text{R}}B_{\text{R}} + f_{\text{M}}B_{\text{M}}}}\Bigg)
```

With w = 1 and every f equal to k this is the broadband weight.

**Extinction in the bands.** The extinction coefficient κ above is at 550 nm. Rayleigh scattering goes as λ⁻⁴ and aerosol extinction as λ^−1.3 like the moonlight they scatter, while the ozone term barely changes across the nebula lines (the Chappuis band peaks at 600 nm; within 0.02 mag/airmass), so a band at λ sees

```math
\kappa_\lambda = \kappa_{\text{R}}\Big(\frac{\lambda}{551}\Big)^{-4} + \kappa_{\text{O}_3} + \kappa_{\text{aer}}\Big(\frac{\lambda}{551}\Big)^{-1.3}
```

and a multi-band filter the mean over its lines weighted by the share of light each passes. At κ = 0.2 the Hα band's coefficient is 0.13 and the [O III] band's 0.26, so at airmass 2 an Hα target through its filter keeps 6% more light than the V coefficient says and an [O III] target 5% less. The filtered weight carries its own transmission `T(h, κ_λ)` and the broadband one `T(h, κ)`; each minute takes the better of the two products, so an imager would not shoot an H II region through [O III] if broadband did better. A filter whose weight runs within a few percent of broadband can now win on extinction alone (a planetary near the Moon through Hα, by a hair), which is as it should be.

**A list of filters** (`-filter dual,ha,oiii`, the ones you own) scores each target once per filter that passes some of its light (a line filter: emission-line targets; a [broadband](#broadband-filters) one: all), each run as above, and keeps the best score among the filters that mattered (the first in the list on a tie): those where some minute's `W_filter` beat `W_sky` or only the filter's wider Moon limit admitted the minute; that filter is the target's FILTER (`Result.Filter`). A target no filter helped is scored without one, FILTER `-`, so its weights are exactly the unfiltered ones (the filtered path's sky, the sum of its parts, matches the unfiltered one only to rounding). One filter per target rather than per minute: changing filters mid-target means a second set of flats and a split stack. A single filter therefore scores exactly as it did before lists.

**The Moon limit.** Away from the Moon its light is mostly Rayleigh-scattered, so the filter's sky-limited gain against moonlight is `f_R / w²`. That, at most 1, replaces k in the Moon-distance rule. At full Moon (100°) through 7 nm bands:

| Target | `ha` | `oiii` | `sii` | `dual` | `tri` | `quad` |
|---|---|---|---|---|---|---|
| H II region | 8° | 100° (no gain) | 100° | 19° | 21° | 21° |
| planetary nebula | 59° | 22° | 100° | 16° | 25° | 27° |
| supernova remnant | 17° | 100° | 44° | 31° | 37° | 24° |
| Wolf-Rayet nebula | 19° | 56° | 100° | 19° | 25° | 24° |

The filtered limits run well below [Telescopius][telescopius]' Lorentzian Moon avoidance (60° for Hα and [S II], 90° for [O III], 120° for broadband, at full Moon), which is a scheduling rule of thumb rather than a model; here the sky weight already prices the moonlight near the limit.

### Broadband filters

`-filter cls|lpro` (`config.Broadband`, `scoring.broadPasses`, `atmos.BroadSky`) are the light-pollution filters that pass wide passbands rather than single lines:

| Choice | Passbands (nm) | Stands for | f_glow | f_R | f_M |
|---|---|---|---|---|---|
| `cls` | 450–520, 640–690 | Astronomik CLS (published edges); Optolong CLS and IDAS LPS-D2 are close | 0.40 | 0.36 | 0.39 |
| `lpro` | 400–430, 440–540, 555–575, 625–700 | Optolong L-Pro; IDAS LPS-D1/P2/D3 and Baader's Moon & Skyglow are alike | 0.75 | 0.80 | 0.77 |

Optolong publishes only the lines the L-Pro blocks (Hg 435.8, 546.1, 577–578 nm; Na 589, 598, 616 nm), not its band edges, so `lpro`'s are read off its reference curve and are good to about ±5 nm. Brands within a class would score alike, so there is one entry per class.

The fractions are taken against an unfiltered camera's 400–700 nm (`atmos.CameraLo`–`CameraHi`, UV/IR cut), not the V band's 88 nm a line filter's width is a share of: a broadband filter passes more than 88 nm, and the unfiltered weight's V sky stands for the whole camera band. The glow's fraction is the passbands' share of the 300 nm; the moonlight's is weighted by its scattering, `∫ (λ/551)⁻⁴ dλ` over the passbands against the camera band (and λ⁻¹·³ for Mie), so cutting the blue cuts more Rayleigh moonlight than glow. The two filter families are thus measured against slightly different baselines, which flatters broadband against the line filters a little; moving the line filters onto the camera band too would change every filtered ranking.

The glow is still flat per nm: the lamps are taken to be white LEDs, a continuum, the conservative case. A broadband filter's gain against sodium or mercury lamps, which it was designed for, is not modelled; under LEDs it mostly cuts sky as a share of the band.

w is the target's share of light through the passbands, against the same unfiltered camera:
- an emission-line target: its lines that fall in a passband over all its lines, Hβ included (unlike the line filters' w, so passing every line is exactly 1, as unfiltered). Both filters pass Hβ, [O III], Hα and [S II], so w = 1 and the sky-limited gain under glow is 1/f_glow: 2.5 through `cls`, 1.33 through `lpro`, against an H II region's 5.3 (0.92²/0.16) through a 7 nm `dual`;
- anything else (galaxies, clusters, reflection nebulae, comets): its continuum, flat per nm, so w = f_glow, and w²/f_glow = f_glow < 1. The filter cuts the target as much as the sky and never wins, as imagers report of galaxies under LED skies. A cluster's filtered sky keeps its k = 0.5, so it loses alike.

The band's extinction is the mean of `(λ/551)⁻⁴` and `(λ/551)^-1.3` over the passbands for continuum, and the passed lines' as for line filters. The Moon limit is f_R/w², at most 1: 36° at full Moon through `cls` for any emission-line target, 80° through `lpro`, and no change for continuum.

### Putting it together

```math
w_i = Q_i \cdot \max\big(T(h_i,\kappa_i)\, W_{\text{sky},i},\ T(h_i,\kappa_{\lambda,i})\, W_{\text{filter},i}\big) ,\qquad
\text{SCORE} = F_{\text{frame}}\cdot\frac{1}{N}\sum_i w_i
```

(without a `-filter`, just `Q · T(h, κ) · W_sky`). Results are sorted by SCORE, and the mean altitude over the observable minutes breaks ties. Extinction does not also dim S. It enters once, as the separate factor T.

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

## Sub-exposure estimate

`scoring.Subs`, `catalog.Target.ExposureSB`, `internal/config`

With framing, each listed target gets the number of sub-exposures needed tonight for a modest, good and best image: a stacked SNR of 5, 10 and 20 per arcsec² at the object's mean surface brightness, through the `-filter` choice that scored it best. It shows in the SUBS column (the good level), in `-json` (`subs`) and in the web page's hover box.

**Camera.** The Origin's defaults, each overridden by its flag: aperture D = 152 mm with a d = 63 mm central obstruction (Celestron, 41% of the aperture); the IMX678's 0.83 peak QE and 2.15 e⁻ read noise (a vendor sheet for the same sensor, at 8.5 dB gain, the middle of its 0.6–3.5 e⁻; the Origin's ISO 1000 has no published gain); 15 s subs; a colour (RGGB) sensor unless `-mono`. `-aperture` alone describes another telescope, so its obstruction is then 0.

**Photon rate.** A V = 0 source gives 1005 photons s⁻¹ cm⁻² Å⁻¹ at 550 nm (Bessell 1998), 884 400 s⁻¹ cm⁻² over the V band's 88 nm. The sky model works in V-band nanoLamberts, so a brightness of x nL is 884 400 · x / NL(0) photons s⁻¹ cm⁻² arcsec⁻², and per pixel

```math
R = \frac{884\,400\;x}{\mathrm{NL}(0)}\;\frac{\pi}{4}\left(D^2 - d^2\right)\;QE_\mathrm{peak}\;q\;\Omega ,\qquad \Omega = \mathrm{scale}^2
```

The relative response q depends on what the light is:

- **The sky and continuum objects**, unfiltered or through `cls`/`lpro`, are spread flat per nm over the camera's 400–700 nm (× 300/88 against V) at q = 0.8 for a mono sensor or 0.34 for a colour one, whose every pixel's dye passes about a third of the band (the RGGB mean of Sony's colour curves). The sky's share through a broadband filter is `BroadSky`'s, as in the score.
- **Through a line filter** the sky model's shares are already the bands' own (`BandSky` against V), at each line's response (Hα 0.95, [O III] 0.85, [S II] 0.9, Hβ 0.8 of peak, read off Sony's IMX678 curve) times the Bayer share of pixels that see it (red Hα and [S II] ¼, green [O III] and Hβ ½; 1 for mono), weighted by the object's light per line.
- **An emission-line object's** light is its lines, which any band that passes them collects once: it uses the line response unfiltered and through broadband filters too, rather than the sky's flat spread, which would count it 3.4 times.

The object and sky per minute are the score's own (the sky with moonlight, zodiacal and galactic light, through a filter its bands' shares; the object dimmed by its band's extinction), with the surface brightness from `ExposureSB`.

**Per sub and stacked.** With rates S and B per pixel, a sub of t seconds and read noise RN,

```math
\mathrm{SNR}^2_\mathrm{sub} = \frac{(S t)^2}{S t + B t + RN^2}\;\frac{1}{\Omega}\quad\text{per arcsec}^2
```

Dark current is left out. An optimally weighted stack adds SNR², so a night half dark and half moonlit is the mean of its minutes' SNR², not the SNR of mean rates. Subs lost to the weather (the mean quality $\bar q$ of the score: cloud, transparency, rain, dew, gusts) must be shot again:

```math
N_L = \left\lceil \frac{L^2}{\overline{\mathrm{SNR}^2_\mathrm{sub}}\;\bar q} \right\rceil ,\qquad L = 5,\ 10,\ 20
```

capped at 10⁹. For a mosaic the counts are per panel, and the subs that fit tonight (the observable time, or the planned blocks with `-plan`, divided by the sub length) are shared among the panels.

**Surface brightness.** The object's own (`SurfaceBrightness`), else the median of its class over the embedded lists (`TestTypicalSB` recomputes them): galaxies 22.52, planetary nebulae 21.75, emission nebulae 22.82, reflection nebulae 22.03, other nebulae 20.91 mag/arcsec², marked `~`. Clusters (point sources), galaxy groups, comets and dark nebulae (unless imaged for the emission behind them, like the Horsehead) get none.

**Limits.**

- The V-band surface brightness of an emission nebula, measured or derived from an integrated magnitude that is often its cluster's, holds little of its Hα, and the class median comes from those values, so emission nebulae without a measurement come out pessimistic: hours rather than the hour Origin users report for the bright ones. Galaxies match (M 51 at the good level in about 35 minutes from a dark site).
- At the Origin's 15 s a colour sensor's Hα quarter through a 7 nm band is read-noise limited (well under 1 e⁻ of object and a few of sky per sub against RN² ≈ 4.6), so a line filter gains less in subs than in the score, which has no read noise and can name a filter that SUBS does not favour as much.
- No dark current, no frame rejection (tracking, satellites, wind: often 10–20% on the Origin), no saturation (the IMX678's 11.27 ke⁻ full well is far off at 15 s for these objects), flat spectra across each band, and one SNR target for every type.

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
| Moon-distance rule | ≥ illumination % in degrees, × k (filtered: × f_R/w²) | uptonight's rule; skipped with the Moon down |
| Altitude window | 30°–80° | uptonight's limits; ≥ 30° keeps the airmass ≤ 2 |
| Cluster sky fraction | k = 0.5 | point sources stand out of the glow |
| Emission line ratios | one set per type ([Nebula filters](#nebula-filters)) | single objects scatter by factors of a few; no list has line fluxes |
| Hβ | Hα/2.86 for every type | case-B recombination; dust raises the ratio, so dusty H II regions' Hβ is overstated |
| Filter band width | every band `-filter-nm` wide | real tri- and quad-band filters mix widths (the L-eNhance's 24 nm over Hβ and [O III]); one width keeps the flag simple |
| OSM obstacle heights | building 8 m (else 3 m a level + 2 m), tree 12 m, wood 20 m | typical two-storey house, mature broadleaf, temperate forest canopy; OSM tags few heights |
| Canopy floor | trees under 1 m ignored | the canopy map reads a metre or so over open ground |
| Night-sky spectrum | flat per nm, Moon before scattering and glow | the lamp mix is unknown; the Moon's redness offsets the Sun's fall |
| Moonlight scattering | aerosol asymmetry g = 0.8, offset δ = 0.078 in V | Winkler's fit at Sutherland (τ = 0.14); clean air, so heavy haze is extrapolated |
| Broadband filter passbands | `cls` Astronomik's published edges, `lpro` read off Optolong's reference curve (±5 nm); continuum flat per nm over 400–700 nm | one entry per class; under the flat (LED) glow brands within a class score alike |
| Zodiacal reference direction | 135° from the Sun at ecliptic latitude 45° | a mid-latitude zenith through the night; the zenith brightness given or defaulted includes that much zodiacal light |
| Galactic light reference | 81 S10, the sky's median by solid angle | a zenith over a year's nights; the zenith brightness given or defaulted includes that much galactic light |
| Zodiacal and galactic light off the zenith | their excess E dimmed by extinction alone, outside K&S's X_s | light from beyond the atmosphere is not brightened by a longer path through glowing air; the share the configured zenith includes stays in K&S's term, as they fitted it |
| Galactic light in V | blue^0.45 · red^0.55 | 550 nm between Pioneer's 440 and 640 nm bands by wavelength, in log brightness |
| Ozone in a filter band | the 550 nm value | within 0.02 mag/airmass at every nebula line |
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
| Sky brightness and moonlight | [Krisciunas & Schaefer (1991)][ks], PASP 103, 1033 (dark sky, Moon's magnitude law); scattered moonlight: [Winkler (2022)][winkler], MNRAS 514, 208; aerosol asymmetry range: [Andrews et al. (2006)][andrews], JGR 111, D05S04; opposition surge after [thorsky][thorsky] |
| Zodiacal light | [Leinert et al. (1998)][leinert], A&AS 127, 1, Table 17 (transcribed as in [astrogo](https://github.com/TuSKan/astrogo)); the natural-sky reference of [Falchi et al. (2016)][falchi], Sci. Adv. 2, e1600377 |
| Galactic light | Pioneer 10 background starlight, Toller (1981), PhD thesis, SUNY Albany, as [Leinert et al. (1998)][leinert], A&AS 127, 1, Tables 35 and 36 give it; the galactic frame: [Blaauw et al. (1960)][blaauw], MNRAS 121, 123 |
| Filter bands | V band: [Bessell (1990)][bessell], PASP 102, 1181; Ångström exponent: [Ångström (1929)][angstrom]; Moon colour: [Thejll et al. (2014)][moon-colour], A&A 563, A38 |
| Broadband filter passbands | [Astronomik CLS][astronomik-cls] (published edges); [Optolong L-Pro][optolong-lpro] (blocked lines, reference curve) |
| Emission line ratios | SNR criterion: [Mathewson & Clarke (1973)][mathewson], ApJ 180, 725; Magellanic Cloud PN [O III]/Hβ: [Stanghellini et al. (2003)][stanghellini], ApJ 596, 997; the criterion against H II regions: [Kopsacheili et al. (2020)][kopsacheili], MNRAS 491, 889; case-B Hα/Hβ: [Osterbrock & Ferland (2006)][agn2], *Astrophysics of Gaseous Nebulae and AGN*, 2nd ed. |
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
[bessell]: https://doi.org/10.1086/132749
[angstrom]: https://doi.org/10.1080/20014422.1929.11880498
[moon-colour]: https://arxiv.org/abs/1401.1994
[mathewson]: https://doi.org/10.1086/151999
[chm]: https://doi.org/10.1016/j.rse.2023.113888
[agn2]: https://ui.adsabs.harvard.edu/abs/2006agna.book.....O
[stanghellini]: https://arxiv.org/abs/astro-ph/0307351
[kopsacheili]: https://arxiv.org/abs/1909.05879
[telescopius]: https://telescopius.com/
[astronomik-cls]: https://www.astroshop.eu/broadband-filters/astronomik-filters-2-cls-ccd-filter/p,16748
[optolong-lpro]: https://optolong.com/cms/document/detail/id/13.html
[roman]: https://doi.org/10.1086/132034
[vi49]: https://vizier.cfa.harvard.edu/viz-bin/VizieR?-source=VI/49
