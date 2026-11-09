# scripts

Helpers that never run at build or run time. Run them from the repository root. Python 3, standard library only, except `qe_digitize.py` (Pillow, NumPy) and `demo_picture.py` (Pillow). Each docstring has the full details.

- [Your own horizon](#your-own-horizon)
- [Target list generators](#target-list-generators)
- [Calibration](#calibration)
- [Bright stars](#bright-stars)
- [Camera charts](#camera-charts)
- [Demo image](#demo-image)

Offline self-tests: `--selftest` on `terrain2horizon.py`, `imm2yaml.py`, `hamap.py` and `calibrate_seeing.py`.

## Your own horizon

`terrain2horizon.py LAT LON` writes a `-horizon` file: a ray every `--step` degrees to `--radius` km over AWS Terrain Tiles, plus OpenStreetMap buildings and the Meta/WRI 1 m canopy height map within 300 m. Downloads are cached under `~/.cache/astro-recommender/terrain`. Stderr ends with a panorama marking what sets each column; check it and edit the file where wrong. `-h` lists every option.

```sh
scripts/terrain2horizon.py 45.81 15.98 > zagreb-horizon.txt                          # terrain, buildings, canopy, every 2°
scripts/terrain2horizon.py 46.37 13.84 --height 12 --elevation 520 > bohinj-horizon.txt   # balcony, model ground too high
scripts/terrain2horizon.py 45.58 17.31 --no-canopy --wood-height 25 > site-horizon.txt    # OSM trees and woods instead
scripts/terrain2horizon.py 46.50 13.70 --no-osm --no-canopy --step 1 --radius 40 > peak-horizon.txt   # bare terrain
```

`*-horizon.txt` is gitignored.

## Target list generators

They write `internal/catalog/targets/*.yaml` to stdout. After changing any list:

```sh
task lists                                 # rebuild the embedded .gob files
go test ./internal/catalog
git diff --stat internal/catalog/targets   # only what you meant
```

Commit YAML and `.gob` together: `TestEmbeddedLists`, the GoReleaser hook and the Pages workflow fail on a stale gob.

### Order

The scripts read each other's output:

1. `imm2yaml.py`
2. `openngc2yaml.py` (reads GaryImmFull)
3. `fillmag.py` on Messier, then GaryImm, Herschel400, Pensack500
4. `mwsc2yaml.py`
5. `wiki2yaml.py` (reads MWSC)
6. the rest; `caldwell2yaml.py` and `vizier2yaml.py Arp` need OpenNGC for `fillmag.py --minor-only`
7. `haflux.py && hamap.py && galaxyv.py` (every list, in place), then `task lists`

Shared helpers, which renaming breaks: `imm2yaml`, `mwsc2yaml`, `wiki2yaml`, `sharpless2yaml`, `vizier2yaml`, `crosscheck`, `haflux`, `hamap`, `galaxyv`, `dewan`.

### Sources

| Script | Output | Source |
|---|---|---|
| `imm2yaml.py` | `GaryImmFull` (3145) | Compendium XLSX (<https://www.garyimm.com/compendium>); fixes known coordinate typos |
| `openngc2yaml.py` | `OpenNGC` (8373), `OpenIC` (5590) | OpenNGC `NGC.csv` at commit `75ca7ff0` |
| `fillmag.py` | a list, via stdout | fills `-9999` magnitudes, adds `surfbr`, `bsurfbr`, `minor`; `--minor-only` adds `minor` alone |
| `mwsc2yaml.py` | `MWSC` (3006) | CDS J/A+A/558/A53 |
| `wiki2yaml.py` | `Melotte` (245), `Collinder` (471) | Wikipedia cross-ID tables; positions from MWSC, OpenNGC/OpenIC, GaryImmFull, `EXTRA`; exits on an unmatched number |
| `lbnminor.py` | `LBN`, via stdout | minor axes and colour-index types, CDS VII/9 |
| `ldnsize.py` | `LDN`, via stdout | diameter of a disc of Lynds' area, CDS VII/7A |
| `sharpless2yaml.py` | `Sharpless` (313) | CDS VII/20, Wikipedia, Compendium |
| `rcw2yaml.py` | `RCW` (181) | CDS VII/216, Compendium |
| `snr2yaml.py` | `SNR` (98 optical of Green's 310) | CDS VII/297, Green's `snrs.G*.html`, Compendium |
| `hash2yaml.py` | `HASH` (3994) | HASH CSV export (registered users), optional Compendium |
| `pnnet2yaml.py` | `PNnet` (1016) | planetarynebulae.net CSV; embedded with the authors' permission |
| `caldwell2yaml.py` | `Caldwell` (109) | Compendium, Wikipedia; C 24 and C 35 from OpenNGC |
| `vizier2yaml.py NAME` | `Barnard`, `vdB`, `vdBH`, `Arp`, `HCG`, `Ced`, `ACO`, `Gum`, `CG`, `MCRN`, `Henize`, `DEM`, `Harris`, `Dias`, `DCld`, `AM` | CDS/VizieR tables, SIMBAD TAP (Gum, CG, Henize), Harris' file, Compendium |
| `haflux.py` | every list, in place | planetaries' `haflux`: Frew et al. 2013, CDS J/MNRAS/431/2 |
| `hamap.py` | every list, in place | large emission nebulae's `haflux`: Finkbeiner's Hα map (`TestHaMapSample`) |
| `galaxyv.py` | every list, in place | replaces galaxies' aperture V with B |
| `crosscheck.py` | report | names in two lists or more compared by position and size; `--simbad` against SIMBAD |

A Compendium row disagreeing with VizieR's position and not settled in `POSITION` stops `vizier2yaml.py`: check SIMBAD and add it. `sharpless2yaml.py`, `rcw2yaml.py` and `snr2yaml.py` warn on stderr for each row rejected as too far away; compare with the counts in their docstrings.

### Commands

```sh
X=IMM_Compendium_2026.xlsx
T=internal/catalog/targets
V='https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-out.all&-out.max=10000&-out.add=_RAJ,_DEJ&-oc.form=dec&-source='
W='https://en.wikipedia.org/w/index.php?action=raw&title='

scripts/imm2yaml.py $X > $T/GaryImmFull.yaml

curl -O https://raw.githubusercontent.com/mattiaverga/OpenNGC/75ca7ff0/database_files/NGC.csv
scripts/openngc2yaml.py NGC.csv NGC > $T/OpenNGC.yaml
scripts/openngc2yaml.py NGC.csv IC > $T/OpenIC.yaml

# fillmag, lbnminor and ldnsize read the list they rewrite: go through a temp file
for l in Messier GaryImm Herschel400 Pensack500; do
    scripts/fillmag.py $T/$l.yaml > /tmp/$l.yaml && mv /tmp/$l.yaml $T/$l.yaml
done

curl -O https://cdsarc.cds.unistra.fr/ftp/J/A+A/558/A53/catalog.dat
scripts/mwsc2yaml.py catalog.dat > $T/MWSC.yaml

curl -o mel.wiki "${W}Melotte_catalogue"; scripts/wiki2yaml.py Melotte mel.wiki > $T/Melotte.yaml
curl -o cr.wiki "${W}Collinder_catalogue"; scripts/wiki2yaml.py Collinder cr.wiki > $T/Collinder.yaml

curl -o lbn.dat https://cdsarc.cds.unistra.fr/ftp/cats/VII/9/catalog.dat
scripts/lbnminor.py lbn.dat $T/LBN.yaml > /tmp/lbn.yaml && mv /tmp/lbn.yaml $T/LBN.yaml
curl -o ldn.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/7A/ldn&-out.all&-out.max=3000&-oc.form=dec'
scripts/ldnsize.py ldn.tsv $T/LDN.yaml > /tmp/ldn.yaml && mv /tmp/ldn.yaml $T/LDN.yaml

curl -o sh2.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/20&-out.all&-out.max=400&-out.add=_RAJ,_DEJ&-oc.form=dec'
curl -o sh2.wiki "${W}Sharpless_catalog"
scripts/sharpless2yaml.py sh2.tsv sh2.wiki $X > $T/Sharpless.yaml
curl -o rcw.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/216/rcw&-out.all&-out.max=400&-out.add=_RAJ,_DEJ&-oc.form=dec'
scripts/rcw2yaml.py rcw.tsv $X > $T/RCW.yaml

# Green's listings: www.mrao.cam.ac.uk no longer resolves. Use the Wayback Machine's
# 2025 copies and gunzip the compressed ones, or their Optical: notes read as missing.
curl -o snr.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/297/snrs&-out.all&-out.max=1000&-oc.form=dec'
scripts/snr2yaml.py snr.tsv snrs/ $X > $T/SNR.yaml

scripts/hash2yaml.py hash.csv $X > $T/HASH.yaml
curl -o pnnet.csv https://planetarynebulae.net/datas/TABLE_NP_CSV_converti.csv
scripts/pnnet2yaml.py pnnet.csv > $T/PNnet.yaml

curl -o cald.wiki "${W}Caldwell_catalogue"
scripts/caldwell2yaml.py $X cald.wiki > $T/Caldwell.yaml
scripts/fillmag.py --minor-only $T/Caldwell.yaml > /tmp/c.yaml && mv /tmp/c.yaml $T/Caldwell.yaml

curl -o barnard.tsv "${V}VII/220A/barnard"; scripts/vizier2yaml.py Barnard barnard.tsv $X > $T/Barnard.yaml
curl -o vdb.tsv "${V}VII/21/catalog"; scripts/vizier2yaml.py vdB vdb.tsv $X > $T/vdB.yaml
curl -o vdbh.tsv "${V}VII/218/catalog"; scripts/vizier2yaml.py vdBH vdbh.tsv > $T/vdBH.yaml
curl -o hcg.tsv "${V}VII/213/groups"; scripts/vizier2yaml.py HCG hcg.tsv $X > $T/HCG.yaml
curl -o ced.tsv "${V}VII/231/catalog"; scripts/vizier2yaml.py Ced ced.tsv $X > $T/Ced.yaml
curl -o mcrn.tsv "${V}J/A+A/399/141/table1"; scripts/vizier2yaml.py MCRN mcrn.tsv $X > $T/MCRN.yaml
curl -o ocl.tsv "${V}B/ocl/clusters"; scripts/vizier2yaml.py Dias ocl.tsv $X > $T/Dias.yaml
curl -o dcld.tsv "${V}VII/191/table1"; scripts/vizier2yaml.py DCld dcld.tsv $X > $T/DCld.yaml
curl -o am.tsv "${V}VII/170/catalog"; scripts/vizier2yaml.py AM am.tsv $X > $T/AM.yaml

curl -o arpord.tsv "${V}VII/192/arpord"; curl -o arplist.tsv "${V}VII/192/arplist"
scripts/vizier2yaml.py Arp arpord.tsv arplist.tsv $X > /tmp/a.yaml
scripts/fillmag.py --minor-only /tmp/a.yaml > $T/Arp.yaml

for t in 3 4 5; do curl -o aco$t.tsv "${V}VII/110A/table$t"; done
scripts/vizier2yaml.py ACO aco3.tsv aco4.tsv aco5.tsv $X > $T/ACO.yaml

# simbad.tsv and henize.tsv are SIMBAD TAP queries, given in vizier2yaml.py's docstring
scripts/vizier2yaml.py Gum simbad.tsv $X > $T/Gum.yaml
scripts/vizier2yaml.py CG simbad.tsv $X > $T/CG.yaml
curl -o deml.tsv "${V}VII/232/lmc"; curl -o dems.tsv "${V}VII/232/smc"
scripts/vizier2yaml.py Henize henize.tsv deml.tsv dems.tsv $X > $T/Henize.yaml
scripts/vizier2yaml.py DEM deml.tsv dems.tsv $X > $T/DEM.yaml

curl -o mwgc.dat https://physics.mcmaster.ca/~harris/mwgc.dat   # 2010 edition; VizieR's VII/202 is older
scripts/vizier2yaml.py Harris mwgc.dat $X > $T/Harris.yaml

scripts/haflux.py && scripts/hamap.py && scripts/galaxyv.py && task lists

scripts/crosscheck.py --lists GaryImm --simbad   # any time; --sep ARCMIN, --ratio X
```

## Calibration

Each prints a constant to copy into the Go code by hand, and caches its downloads under `~/.cache/astro-recommender/`. Rerun only when the model it fits changes.

| Script | Fits | Against | Redo after |
|---|---|---|---|
| `calibrate_seeing.py` | `groundFactor`, `freeFactor` (`internal/weather`) | ESO DIMM, Open-Meteo's archived ECMWF IFS 0.25; fits even months, evaluates odd, then fits all | `Seeing`, `cn2`, `levels()`, the pressure levels |
| `calibrate_lines.py` | `vContinuum` (`internal/scoring`) | Finkbeiner's Hα map; sample from `TestCalibrationSample` | `Lines`, `LineV`, `SurfaceBrightness`, `hamap.measure`, the lists' nebulae |
| `filter_shift.py` | `filterN` (`internal/scoring`) | Chroma's measured curves at f/2–f/5 | `beam.through` |

`dewan.py` is the Python reference for `weather.Seeing`: it prints the numbers `TestSeeing`, `TestCn2Stratospheric` and `TestMutCn2` pin. Change it and the Go code together.

```sh
scripts/calibrate_seeing.py                 # Paranal, May 2024 to the last full month; about 2 minutes cold
scripts/calibrate_seeing.py --site lasilla  # cross-check; also --from 2025-01 --to 2025-12
scripts/calibrate_lines.py
scripts/filter_shift.py
scripts/dewan.py
```

## Bright stars

`bsc2go.py` writes `internal/stars/bsc.go`: the Yale Bright Star Catalogue (CDS V/50) to V 4.0, T CrB left out. Rerun only to change `MAX_MAG` or `SKIP`.

```sh
curl -o bsc.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=V/50/catalog&-out=HR,Name,Vmag,_RAJ2000,_DEJ2000&Vmag=%3C%3D4.0&-out.max=unlimited'
scripts/bsc2go.py bsc.tsv > internal/stars/bsc.go
```

## Camera charts

`qe_digitize.py` reads a maker's QE chart into `internal/config/cameras.go`'s `Response` or `MonoResponse`. Render the chart first, e.g. `gs -sDEVICE=png16m -r400`. `--per-watt` is for Sony's "relative response" charts.

```sh
scripts/qe_digitize.py chart.png --x 400 1000 [--box X0 Y0 X1 Y1] [--dark] [--per-watt] [--xpx PX0 PX1] [--light-grid] [--grid-max 0.9]
scripts/qe_digitize.py chart.png --x 400 1000 --mono black [--black-max 0.3] [--sat-min 0.1]
```

## Demo image

`demo.sh` re-renders `demo.jpg` from a headless frame of the terminal view: `freeze` draws it, `demo_picture.py` pastes the DSS2 picture over its half-blocks, macOS `sips` converts to JPEG, `jpegoptim` recompresses if installed. Needs network and Pillow. Arguments replace the default flags, so include `-lat` and `-lon`.

```sh
task demo
scripts/demo.sh -lat 45.8 -lon 16 -scope origin
```
