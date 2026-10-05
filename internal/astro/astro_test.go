// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package astro

import (
	"math"
	"testing"
	"time"
)

func TestSexagesimal(t *testing.T) {
	if v, _ := Sexagesimal("-00 30 00"); v != -0.5 {
		t.Errorf("sexagesimal(-00 30 00) = %v", v)
	}
	if v, _ := Sexagesimal("+13 14 48"); math.Abs(v-13.2467) > 1e-3 {
		t.Errorf("sexagesimal(+13 14 48) = %v", v)
	}
	if v, err := Sexagesimal("23 59 59.9"); err != nil || v >= 24 {
		t.Errorf("sexagesimal(23 59 59.9) = %v, %v", v, err)
	}
	for _, bad := range []string{"", "  ", "-", "1 2 3 4", "1 x", "10 70 00", "00 00 99", "10 -5 00", "1 60", "NaN", "Inf", "10 NaN 00"} {
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
	start, end, ok := Window(time.Date(2026, 10, 5, 0, 0, 0, 0, zg), 45.8, 16.0)
	start, end = start.In(zg), end.In(zg)
	if !ok || start.Hour() != 20 || end.Day() != 6 || end.Hour() != 5 {
		t.Errorf("window = %v - %v, %v", start, end, ok)
	}
	// Tromsø at midsummer never gets astronomically dark.
	if _, _, ok := Window(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), 69.65, 18.96); ok {
		t.Error("window found night in Tromsø midsummer")
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
}

func TestAltAz(t *testing.T) {
	// altAz: Polaris-ish (dec 89.9) is due north at the observer's latitude.
	if alt, az := AltAz(0, 89.99, time.Now(), 45, 16); math.Abs(alt-45) > 0.1 || (az > 1 && az < 359) {
		t.Errorf("pole altAz = %v, %v", alt, az)
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
}

// A site far from the -tz zone: Sydney planned with Europe/Zagreb. Searching
// from Zagreb noon (10:00 UTC, already night in Sydney) used to start the
// window late; from the site's solar noon it starts at real astronomical dusk,
// about 20:26 AEDT (09:26 UTC), on a whole minute.
func TestWindowForeignTimeZone(t *testing.T) {
	zg, _ := time.LoadLocation("Europe/Zagreb")
	start, end, ok := Window(time.Date(2026, 10, 5, 0, 0, 0, 0, zg), -33.87, 151.21)
	if !ok || start.UTC().Hour() != 9 || start.Second() != 0 || end.Sub(start) < 8*time.Hour {
		t.Errorf("Sydney window = %v - %v, %v", start.UTC(), end.UTC(), ok)
	}
}

// Tonight is the night in progress after midnight, else the coming one.
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
		{time.Date(2026, 10, 5, 22, 0, 0, 0, zg), 5, false}, // evening, same date
	} {
		day, inProgress := Tonight(c.now, 45.8, 16.0)
		if day.Day() != c.day || inProgress != c.inProgress {
			t.Errorf("Tonight(%v) = %v, %v; want day %d, %v", c.now, day, inProgress, c.day, c.inProgress)
		}
	}
}
