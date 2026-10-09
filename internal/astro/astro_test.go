// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package astro

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestSexagesimal(t *testing.T) {
	if _, err := Sexagesimal("+-10 00 00"); !errors.Is(err, errSign) || !strings.Contains(err.Error(), `"+-10 00 00"`) {
		t.Errorf("sexagesimal(+-10 00 00) = %v, want errSign quoting the input", err)
	}
	// A second sign after whitespace, or in a later field, is one too: "-00"
	// parses as -0, which the range check cannot see.
	for _, bad := range []string{"+ -00 30 00", "- +10 00 00", "00 -00 30"} {
		if _, err := Sexagesimal(bad); !errors.Is(err, errSign) {
			t.Errorf("sexagesimal(%q) = %v, want errSign", bad, err)
		}
	}
	if _, err := Sexagesimal("-10 75 00"); !errors.Is(err, errRange) || !strings.Contains(err.Error(), `"-10 75 00"`) {
		t.Errorf("sexagesimal(-10 75 00) = %v, want errRange quoting the input with its sign", err)
	}
	if v, _ := Sexagesimal("-00 30 00"); v != -0.5 {
		t.Errorf("sexagesimal(-00 30 00) = %v", v)
	}
	if v, _ := Sexagesimal("+13 14 48"); math.Abs(v-13.2467) > 1e-3 {
		t.Errorf("sexagesimal(+13 14 48) = %v", v)
	}
	if v, err := Sexagesimal("23 59 59.9"); err != nil || v >= 24 {
		t.Errorf("sexagesimal(23 59 59.9) = %v, %v", v, err)
	}
	for _, bad := range []string{"", "  ", "-", "1 2 3 4", "1 x", "10 70 00", "00 00 99", "10 -5 00", "1 60", "NaN", "Inf", "10 NaN 00", "+-10 00 00", "--5 00 00", "-+5"} {
		if _, err := Sexagesimal(bad); err == nil {
			t.Errorf("sexagesimal(%q) accepted", bad)
		}
	}
}

func TestMoonIllumination(t *testing.T) {
	// Full moon 2026-09-26 16:49 UTC, new moon 2026-10-10 15:50 UTC.
	if k := MoonIllumination(time.Date(2026, 9, 26, 16, 49, 0, 0, time.UTC)); k < 0.99 {
		t.Errorf("full moon illumination = %v", k)
	}
	if k := MoonIllumination(time.Date(2026, 10, 10, 15, 50, 0, 0, time.UTC)); k > 0.01 {
		t.Errorf("new moon illumination = %v", k)
	}
}

func TestSunAltitude(t *testing.T) {
	// Equinox 2026-03-20: Sun near 90-lat at local solar noon on Greenwich.
	ra, dec := SunRADec(time.Date(2026, 3, 20, 12, 7, 0, 0, time.UTC))
	if alt := Altitude(ra, dec, time.Date(2026, 3, 20, 12, 7, 0, 0, time.UTC), 45, 0); math.Abs(alt-45) > 0.5 {
		t.Errorf("equinox noon altitude = %v", alt)
	}
}

func TestWindow(t *testing.T) {
	// Zagreb 2026-10-05: astronomical dusk ~20:08, dawn ~05:2x CEST.
	zg, _ := time.LoadLocation("Europe/Zagreb")
	start, end, ok := Window(time.Date(2026, 10, 5, 0, 0, 0, 0, zg), 45.8, 16.0, Astronomical)
	start, end = start.In(zg), end.In(zg)
	if !ok || start.Hour() != 20 || end.Day() != 6 || end.Hour() != 5 {
		t.Errorf("window = %v - %v, %v", start, end, ok)
	}
	// Nautical night (Sun below -12°) starts earlier and ends later; Zagreb
	// gets about 35 minutes more at each end in October.
	ns, ne, ok := Window(time.Date(2026, 10, 5, 0, 0, 0, 0, zg), 45.8, 16.0, Nautical)
	if d1, d2 := start.Sub(ns), ne.Sub(end); !ok || d1 < 25*time.Minute || d1 > 45*time.Minute || d2 < 25*time.Minute || d2 > 45*time.Minute {
		t.Errorf("nautical window = %v - %v, %v; want ~35 min wider than %v - %v", ns, ne, ok, start, end)
	}
	// Tromsø at midsummer never gets astronomically dark.
	if _, _, ok := Window(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), 69.65, 18.96, Astronomical); ok {
		t.Error("window found night in Tromsø midsummer")
	}
	// Reykjavík (64°N) at midsummer has no nautical night either.
	if _, _, ok := Window(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), 64.13, -21.9, Nautical); ok {
		t.Error("window found nautical night in Reykjavík midsummer")
	}
	// -from/-to clip within the night and never extend it.
	for _, c := range []struct {
		from, to, wantStart, wantEnd string
		ok                           bool
	}{
		{"22:00", "02:00", "22:00", "02:00", true},
		{"18:00", "07:00", start.Format("15:04"), end.Format("15:04"), true}, // clamped to the night
		{"", "01:30", start.Format("15:04"), "01:30", true},
		{"03:00", "02:00", "", "", false},
		{"25:00", "", "", "", false},
	} {
		s, e, err := ClipWindow(time.Date(2026, 10, 5, 0, 0, 0, 0, zg), zg, start, end, c.from, c.to)
		if (err == nil) != c.ok || (c.ok && (s.Format("15:04") != c.wantStart || e.Format("15:04") != c.wantEnd)) {
			t.Errorf("clipWindow(%q, %q) = %v - %v, %v", c.from, c.to, s, e, err)
		}
	}
	// A clock the zone skips (02:30 on the night Zagreb springs forward) is
	// an error, not silently 03:30; 03:30 itself is fine.
	dst := time.Date(2026, 3, 28, 0, 0, 0, 0, zg)
	dstStart, dstEnd := time.Date(2026, 3, 28, 19, 0, 0, 0, zg), time.Date(2026, 3, 29, 5, 0, 0, 0, zg)
	if _, _, err := ClipWindow(dst, zg, dstStart, dstEnd, "02:30", ""); err == nil {
		t.Error("clipWindow(02:30) on the spring-forward night accepted")
	}
	if s, _, err := ClipWindow(dst, zg, dstStart, dstEnd, "03:30", ""); err != nil || s.Format("15:04 MST") != "03:30 CEST" {
		t.Errorf("clipWindow(03:30) on the spring-forward night = %v, %v", s, err)
	}
	// A clock the zone repeats (02:30 on the night Zagreb falls back) is
	// read as the widest window: -from at CEST, -to at CET.
	fb := time.Date(2026, 10, 24, 0, 0, 0, 0, zg)
	fbStart, fbEnd := time.Date(2026, 10, 24, 19, 0, 0, 0, zg), time.Date(2026, 10, 25, 5, 0, 0, 0, zg)
	if s, _, err := ClipWindow(fb, zg, fbStart, fbEnd, "02:30", ""); err != nil || !s.Equal(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Errorf("clipWindow(-from 02:30) on the fall-back night = %v, %v; want 02:30 CEST", s, err)
	}
	if _, e, err := ClipWindow(fb, zg, fbStart, fbEnd, "", "02:30"); err != nil || !e.Equal(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Errorf("clipWindow(-to 02:30) on the fall-back night = %v, %v; want 02:30 CET", e, err)
	}
}

func TestAltAz(t *testing.T) {
	// altAz: Polaris-ish (dec 89.9) is due north at the observer's latitude.
	if alt, az := AltAz(0, 89.99, time.Now(), 45, 16); math.Abs(alt-45) > 0.1 || (az > 1 && az < 359) {
		t.Errorf("pole altAz = %v, %v", alt, az)
	}
	// At the zenith the sine rounds to 1.0000000000000002 for lat = dec = 10
	// at hour angle 0; Asin of that is NaN unless clamped.
	if sinAlt := NewHorizontal(10, 10).SinAlt(0); sinAlt != 1 {
		t.Errorf("zenith SinAlt = %v, want 1", sinAlt)
	}
}

func TestPrecess(t *testing.T) {
	// precess against SOFA iauPmat06 (mean of date) at 2026-10-05 22:00 UTC.
	pt := time.Date(2026, 10, 5, 22, 0, 0, 0, time.UTC)
	for _, c := range [][4]float64{{0, 0, 0.34288, 0.14897}, {83.8221, -5.3911, 84.15102, -5.37550}, {37.95, 89.26, 46.69231, 89.37044}} {
		ra, dec := Precess(c[0], c[1], pt)
		if d := Separation(ra, dec, c[2], c[3]) * 3600; d > 1 {
			t.Errorf("precess(%v, %v) = %v, %v: %.2f\" from SOFA", c[0], c[1], ra, dec, d)
		}
	}
	// A position within rounding of the precessed pole: the sine of the
	// declination comes out above 1 and must be clamped, not NaN.
	if _, dec := Precess(23.99087414049274*15, 89.88104100680276, time.Date(2021, 5, 15, 0, 0, 0, 0, time.UTC)); !(dec > 89.99 && dec <= 90) {
		t.Errorf("precess at the pole: dec %v, want 90", dec)
	}
}

// A site far from the -tz zone: Sydney planned with Europe/Zagreb. Searching
// from Zagreb noon (10:00 UTC, already night in Sydney) used to start the
// window late; from the site's solar noon it starts at real astronomical dusk,
// about 20:26 AEDT (09:26 UTC), on a whole minute.
// Zones across the date line from their longitude get the night of their own
// local date, not the next one; Auckland and Honolulu are the controls.
func TestWindowDateLine(t *testing.T) {
	for _, c := range []struct {
		zone     string
		lat, lon float64
	}{
		{"Pacific/Apia", -13.8, -171.8},
		{"Pacific/Tongatapu", -21.1, -175.2},
		{"Pacific/Kiritimati", 1.9, -157.4},
		{"Pacific/Chatham", -44, -176.5},
		{"Asia/Anadyr", 64.7, -177.5},
		{"Pacific/Auckland", -36.8, 174.8},
		{"Pacific/Honolulu", 21.3, -157.9},
	} {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Fatal(err)
		}
		start, _, ok := Window(time.Date(2026, 10, 5, 0, 0, 0, 0, loc), c.lat, c.lon, Astronomical)
		if start = start.In(loc); !ok || start.Day() != 5 || start.Hour() < 18 {
			t.Errorf("%s: dusk %v, want the evening of 2026-10-05", c.zone, start)
		}
	}
}

func TestWindowForeignTimeZone(t *testing.T) {
	zg, _ := time.LoadLocation("Europe/Zagreb")
	start, end, ok := Window(time.Date(2026, 10, 5, 0, 0, 0, 0, zg), -33.87, 151.21, Astronomical)
	if !ok || start.UTC().Hour() != 9 || start.Second() != 0 || end.Sub(start) < 8*time.Hour {
		t.Errorf("Sydney window = %v - %v, %v", start.UTC(), end.UTC(), ok)
	}
}

// Tonight is the night in progress (after dusk or after midnight), else the coming one.
func TestTonight(t *testing.T) {
	zg, _ := time.LoadLocation("Europe/Zagreb")
	for _, c := range []struct {
		now        time.Time
		day        int
		inProgress bool
	}{
		{time.Date(2026, 10, 6, 1, 30, 0, 0, zg), 5, true},  // middle of the night
		{time.Date(2026, 10, 6, 8, 0, 0, 0, zg), 6, false},  // after dawn
		{time.Date(2026, 10, 5, 15, 0, 0, 0, zg), 5, false}, // afternoon
		{time.Date(2026, 10, 5, 22, 0, 0, 0, zg), 5, true},  // evening, after dusk (20:08)
		{time.Date(2026, 10, 5, 20, 0, 0, 0, zg), 5, false}, // just before dusk
	} {
		day, inProgress := Tonight(c.now, 45.8, 16.0, Astronomical)
		if day.Day() != c.day || inProgress != c.inProgress {
			t.Errorf("Tonight(%v) = %v, %v; want day %d, %v", c.now, day, inProgress, c.day, c.inProgress)
		}
	}
	// Santiago de Compostela at midsummer: the night of the 20th starts at
	// 00:35 on the 21st, so at 00:10 it is the coming night, not the 21st's.
	mad, _ := time.LoadLocation("Europe/Madrid")
	if day, inProgress := Tonight(time.Date(2026, 6, 21, 0, 10, 0, 0, mad), 42.88, -8.54, Astronomical); day.Day() != 20 || inProgress {
		t.Errorf("Tonight(Santiago 00:10) = %v, %v; want day 20, false", day, inProgress)
	}
}
