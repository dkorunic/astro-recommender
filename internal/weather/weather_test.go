// SPDX-FileCopyrightText: 2026 Dinko Korunic <dinko.korunic@gmail.com>
// SPDX-License-Identifier: MIT

package weather

import (
	"testing"
	"time"
)

func TestAstroKey(t *testing.T) {
	if k := AstroKey(time.Date(2026, 10, 5, 22, 0, 0, 0, time.UTC)); k != time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("astroKey(22:00) = %v, want 21:00", time.Unix(k, 0).UTC())
	}
}
