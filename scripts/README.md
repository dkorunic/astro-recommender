# scripts

Helper scripts for astro-recommender. None of them run at build or run time: they generate the embedded target lists, a `-horizon` file, the seeing calibration constants and `demo.jpg`. Every Python script is Python 3 with the standard library only; run them from the repository root. Each script's docstring has the full details. The top-level [README](../README.md#target-lists) explains what each list contains and where its data comes from.

- [Your own horizon](#your-own-horizon)
- [Target list generators](#target-list-generators)
- [Seeing model](#seeing-model)
- [Demo image](#demo-image)

## Your own horizon

### `terrain2horizon.py`

Writes a `-horizon` file (azimuth/altitude pairs) for a site. It traces a ray every `--step` degrees out to `--radius` km over the free AWS Terrain Tiles, then adds OpenStreetMap buildings and the trees of the Meta/WRI 1 m canopy height map within 300 m. Downloads are cached under `~/.cache/astro-recommender/terrain` (about 7 MB of canopy data per site, once). Stderr ends with a text panorama of the horizon, with the letter of what sets each column (terrain, building, tree) under it. Check it and edit the file by hand where it is wrong.

```sh
# Default: terrain to 20 km, OSM buildings, canopy trees, every 2°
scripts/terrain2horizon.py 45.81 15.98 > zagreb-horizon.txt
go run . -lat 45.81 -lon 15.98 -horizon zagreb-horizon.txt

# A balcony 12 m above the ground on a valley floor whose model elevation is too high
scripts/terrain2horizon.py 46.37 13.84 --height 12 --elevation 520 > bohinj-horizon.txt

# Trees and woods from OSM instead of the canopy map, with taller woods
scripts/terrain2horizon.py 45.58 17.31 --no-canopy --wood-height 25 > site-horizon.txt

# Bare terrain only, finer and farther: a mountain site observing below 30°
scripts/terrain2horizon.py 46.50 13.70 --no-osm --no-canopy --step 1 --radius 40 > peak-horizon.txt
go run . -lat 46.50 -lon 13.70 -horizon peak-horizon.txt -alt-min 15

# Offline check of the PNG/TIFF decoders, ray marches and plot
scripts/terrain2horizon.py --selftest
```

`*-horizon.txt` is gitignored, so that naming keeps the generated files out of commits. `scripts/terrain2horizon.py -h` lists every option (`--osm-radius`, `--canopy-radius`, `--building-height`, `--tree-height`, `--near`, `--zoom`, `--cache`).

## Target list generators

These write the YAML lists in `internal/catalog/targets/` to stdout. The binary embeds the `.gob` files built from them, so after regenerating any list run:

```sh
task lists          # rebuild the .gob files (TestEmbeddedLists -update)
go test ./internal/catalog
git diff --stat internal/catalog/targets   # check that only what you meant has changed
```

Then commit the YAML and the `.gob` together. `TestEmbeddedLists`, the GoReleaser hook and the Pages workflow all fail on a stale gob.

Several sources are local files you download first. Most often that is Gary Imm's Deep Sky Compendium spreadsheet (`IMM_Compendium_2026.xlsx`, from <https://www.garyimm.com/compendium>), CDS/VizieR tables, or Wikipedia's raw wikitext. The `curl` lines below are the ones in each script's docstring. In the examples, `X` and `T` stand for:

```sh
X=IMM_Compendium_2026.xlsx
T=internal/catalog/targets
V='https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-out.all&-out.max=5000&-out.add=_RAJ,_DEJ&-oc.form=dec&-source='
```

### Order

The scripts read each other's output, so regenerate in this order:

1. `imm2yaml.py` → `GaryImmFull.yaml`
2. `openngc2yaml.py` → `OpenNGC.yaml`, `OpenIC.yaml` (reads GaryImmFull)
3. `fillmag.py` on `Messier.yaml`, then on `GaryImm.yaml`, `Herschel400.yaml`, `Pensack500.yaml`
4. `mwsc2yaml.py` → `MWSC.yaml`
5. `wiki2yaml.py` → `Melotte.yaml`, `Collinder.yaml` (read MWSC)
6. the rest in any order. `caldwell2yaml.py` and `vizier2yaml.py Arp` need OpenNGC, because `fillmag.py --minor-only` runs after them.

The scripts also import each other's helpers: `imm2yaml` (XLSX reading, `aliases`, `kind`, `sexagesimal`), `mwsc2yaml` (`entries`, `lookup`), `wiki2yaml` (wikitext regexes) and `sharpless2yaml` (`sep`). Renaming one of these breaks the scripts that import it.

### Shared sources

| Script | Output | Source |
|---|---|---|
| `imm2yaml.py` | `GaryImmFull.yaml` (3145) | Compendium XLSX: every object with Gary Imm's rating, magnitude and V surface brightness; fixes known coordinate typos |
| `openngc2yaml.py` | `OpenNGC.yaml` (8373), `OpenIC.yaml` (5590) | OpenNGC `NGC.csv`, pinned to commit `75ca7ff0` |
| `fillmag.py` | edits a list in place via stdout | fills `-9999` magnitudes and adds `surfbr`, `bsurfbr` and `minor` from Messier/OpenNGC/OpenIC/GaryImmFull; `--minor-only` adds just `minor` |
| `mwsc2yaml.py` | `MWSC.yaml` (3006) | Kharchenko et al. 2013 star clusters, CDS J/A+A/558/A53 |
| `lbnminor.py` | edits `LBN.yaml` via stdout | adds minor axes and Lynds' colour-index types (reflection, nebula, H II region) from Lynds, CDS VII/9 |
| `ldnsize.py` | edits `LDN.yaml` via stdout | replaces the sizes by the diameter of a disc of Lynds' area, CDS VII/7A |

```sh
scripts/imm2yaml.py $X > $T/GaryImmFull.yaml

curl -O https://raw.githubusercontent.com/mattiaverga/OpenNGC/75ca7ff0/database_files/NGC.csv
scripts/openngc2yaml.py NGC.csv NGC > $T/OpenNGC.yaml
scripts/openngc2yaml.py NGC.csv IC > $T/OpenIC.yaml

# fillmag, lbnminor and ldnsize rewrite a list that is also their input: go through a temp file
for l in Messier GaryImm Herschel400 Pensack500; do
    scripts/fillmag.py $T/$l.yaml > /tmp/$l.yaml && mv /tmp/$l.yaml $T/$l.yaml
done

curl -O https://cdsarc.cds.unistra.fr/ftp/J/A+A/558/A53/catalog.dat
scripts/mwsc2yaml.py catalog.dat > $T/MWSC.yaml

curl -o lbn.dat https://cdsarc.cds.unistra.fr/ftp/cats/VII/9/catalog.dat
scripts/lbnminor.py lbn.dat $T/LBN.yaml > /tmp/lbn.yaml && mv /tmp/lbn.yaml $T/LBN.yaml

curl -o ldn.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/7A/ldn&-out.all&-out.max=3000&-oc.form=dec'
scripts/ldnsize.py ldn.tsv $T/LDN.yaml > /tmp/ldn.yaml && mv /tmp/ldn.yaml $T/LDN.yaml
```

### Clusters

| Script | Output | Source |
|---|---|---|
| `wiki2yaml.py` | `Melotte.yaml` (245), `Collinder.yaml` (471) | Wikipedia's cross-ID tables; positions from MWSC, then OpenNGC/OpenIC, then GaryImmFull, then hard-coded `EXTRA` clusters. Exits listing any number it cannot match |

```sh
curl -o mel.wiki 'https://en.wikipedia.org/w/index.php?title=Melotte_catalogue&action=raw'
scripts/wiki2yaml.py Melotte mel.wiki > $T/Melotte.yaml
curl -o cr.wiki 'https://en.wikipedia.org/w/index.php?title=Collinder_catalogue&action=raw'
scripts/wiki2yaml.py Collinder cr.wiki > $T/Collinder.yaml
```

### Nebulae and planetaries

| Script | Output | Source |
|---|---|---|
| `sharpless2yaml.py` | `Sharpless.yaml` (313) | CDS VII/20 + Wikipedia + Compendium |
| `rcw2yaml.py` | `RCW.yaml` (181) | CDS VII/216 + Compendium |
| `snr2yaml.py` | `SNR.yaml` (98 optical of Green's 310) | CDS VII/297 + Green's detailed listings (`snrs.G*.html`) + Compendium |
| `hash2yaml.py` | `HASH.yaml` (3994) | HASH PN database CSV export (registered users), optional Compendium for V magnitudes |
| `pnnet2yaml.py` | `PNnet.yaml` (1016) | planetarynebulae.net's CSV export, plus two requests to the site's search for the sizes; embedded with the authors' permission |

```sh
curl -o sh2.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/20&-out.all&-out.max=400&-out.add=_RAJ,_DEJ&-oc.form=dec'
curl -o sh2.wiki 'https://en.wikipedia.org/w/index.php?title=Sharpless_catalog&action=raw'
scripts/sharpless2yaml.py sh2.tsv sh2.wiki $X > $T/Sharpless.yaml

curl -o rcw.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/216/rcw&-out.all&-out.max=400&-out.add=_RAJ,_DEJ&-oc.form=dec'
scripts/rcw2yaml.py rcw.tsv $X > $T/RCW.yaml

# Green's listings: www.mrao.cam.ac.uk no longer resolves; use the Wayback Machine's
# 2025 copies, and gunzip the ones saved compressed, or their Optical: notes read as missing
curl -o snr.tsv 'https://vizier.cds.unistra.fr/viz-bin/asu-tsv?-source=VII/297/snrs&-out.all&-out.max=1000&-oc.form=dec'
scripts/snr2yaml.py snr.tsv snrs/ $X > $T/SNR.yaml

scripts/hash2yaml.py hash.csv $X > $T/HASH.yaml
curl -o pnnet.csv https://planetarynebulae.net/datas/TABLE_NP_CSV_converti.csv
scripts/pnnet2yaml.py pnnet.csv > $T/PNnet.yaml   # also fetches the site's search, for the sizes
```

Sharpless, RCW and SNR print a warning on stderr for every Compendium row they reject as too far away. Compare those warnings with the counts in the script's docstring.

### VizieR and SIMBAD catalogues: `vizier2yaml.py`

One script, with the catalogue name as the first argument: Barnard, vdB, vdBH, Arp, HCG, Ced, ACO, Gum, CG, MCRN, Henize, DEM, Harris, Dias, DCld, AM. A Compendium row whose position disagrees with VizieR's and that `POSITION` does not settle stops the script; check it against SIMBAD and add it to `POSITION`.

```sh
curl -o barnard.tsv "${V}VII/220A/barnard"
scripts/vizier2yaml.py Barnard barnard.tsv $X > $T/Barnard.yaml

curl -o vdbh.tsv "${V}VII/218/catalog"
scripts/vizier2yaml.py vdBH vdbh.tsv > $T/vdBH.yaml          # no Compendium rows: VizieR alone

curl -o arpord.tsv "${V}VII/192/arpord"; curl -o arplist.tsv "${V}VII/192/arplist"
scripts/vizier2yaml.py Arp arpord.tsv arplist.tsv $X > $T/Arp.yaml
scripts/fillmag.py --minor-only $T/Arp.yaml > /tmp/a.yaml && mv /tmp/a.yaml $T/Arp.yaml

for t in 3 4 5; do curl -o aco$t.tsv "${V}VII/110A/table$t"; done
scripts/vizier2yaml.py ACO aco3.tsv aco4.tsv aco5.tsv $X > $T/ACO.yaml

# Gum and CG share one SIMBAD TAP query; see the script's docstring for it
scripts/vizier2yaml.py Gum simbad.tsv $X > $T/Gum.yaml
scripts/vizier2yaml.py CG simbad.tsv $X > $T/CG.yaml

# Henize's Magellanic nebulae from SIMBAD (another TAP query), sized from DEM's two tables
curl -o deml.tsv "${V}VII/232/lmc"; curl -o dems.tsv "${V}VII/232/smc"
scripts/vizier2yaml.py Henize henize.tsv deml.tsv dems.tsv $X > $T/Henize.yaml
scripts/vizier2yaml.py DEM deml.tsv dems.tsv $X > $T/DEM.yaml

# Harris' globular clusters, 2010 edition, from his site (VizieR's VII/202 is older)
curl -o mwgc.dat https://physics.mcmaster.ca/~harris/mwgc.dat
scripts/vizier2yaml.py Harris mwgc.dat $X > $T/Harris.yaml
```

The docstring has the `curl` line for every table (vdB VII/21, HCG VII/213, Ced VII/231, MCRN J/A+A/399/141, Dias B/ocl, DCld VII/191, AM VII/170) and the Henize query.

### Caldwell: `caldwell2yaml.py`

`Caldwell.yaml` (109) from the Compendium rows carrying `Caldwell N`, plus Wikipedia's table. C 24 and C 35 come from `OpenNGC.yaml`.

```sh
curl -o cald.wiki 'https://en.wikipedia.org/w/index.php?title=Caldwell_catalogue&action=raw'
scripts/caldwell2yaml.py $X cald.wiki > $T/Caldwell.yaml
scripts/fillmag.py --minor-only $T/Caldwell.yaml > /tmp/c.yaml && mv /tmp/c.yaml $T/Caldwell.yaml
```

## Seeing model

### `dewan.py`

The Python reference for `internal/weather`'s `Seeing`: Dewan et al. (1993) Cn² per layer, the WMO tropopause, Fried's r0 and the FWHM. Run it to print the numbers that `TestSeeing`, `TestCn2Stratospheric` and `TestMutCn2` pin. It must stay in step with the Go code, so change both together.

```sh
scripts/dewan.py
```

### `calibrate_seeing.py`

Refits `groundFactor` and `freeFactor` in `internal/weather` against ESO's DIMM seeing archive and Open-Meteo's archived ECMWF IFS 0.25 forecasts. It fits on the even months, evaluates on the odd ones, then fits on all of them, and prints both factors with bias, RMSE and correlation. Copy the result into `weather.go` by hand. A cold run takes about 2 minutes on the network; after that it reads from `~/.cache/astro-recommender/calibrate`. Redo it only when `Seeing`, `cn2`, `levels()` or the pressure levels change, not on a schedule.

```sh
scripts/calibrate_seeing.py                            # Paranal, May 2024 to the last full month
scripts/calibrate_seeing.py --site lasilla             # the second site, as a cross-check
scripts/calibrate_seeing.py --from 2025-01 --to 2025-12
scripts/calibrate_seeing.py --selftest
```

## Demo image

### `demo.sh`

Re-renders `demo.jpg` for the top-level README from a live coloured run. It needs [freeze](https://github.com/charmbracelet/freeze) and macOS `sips`, uses `jpegoptim` if installed, and needs network. Any arguments replace the default flag set, so pass `-lat` and `-lon` too.

```sh
task demo                                   # same as scripts/demo.sh
scripts/demo.sh -lat 45.8 -lon 16 -origin
```
