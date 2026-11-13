// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

// Package dss holds what links to a target's DSS2 picture share: the survey,
// the field that frames an object and the Aladin Lite address, and Cutout,
// the picture resampled from a HiPS mirror's tiles when CDS does not answer.
package dss

import "fmt"

// Survey is the HiPS survey every picture of a target comes from, escaped
// for a query string.
const Survey = "CDS%2FP%2FDSS2%2Fcolor"

// FOV is the field in degrees to show an object of size arcminutes in: the
// short side half again the object, within 0.1-5°, 0.5° for an unknown size.
func FOV(size float64) float64 {
	if size == 0 {
		return 0.5
	}

	return min(5, max(0.1, size*1.5/60))
}

// Aladin is the Aladin Lite link at a J2000 position in degrees, for an
// object of size arcminutes.
func Aladin(ra, dec, size float64) string {
	return fmt.Sprintf("https://aladin.cds.unistra.fr/AladinLite/?target=%.4f%%20%.4f&fov=%.2f&survey=%s", ra, dec, FOV(size), Survey)
}
