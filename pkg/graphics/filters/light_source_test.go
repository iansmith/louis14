package filters

import (
	"math"
	"testing"
)

const lightSourceTolerance = 1e-9

func approxEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < lightSourceTolerance
}

// TestDistantLightSource_ElevationAzimuth checks the direction-vector
// formula for `<feDistantLight>`:
//
//	Lx = cos(azimuth) * cos(elevation)
//	Ly = sin(azimuth) * cos(elevation)
//	Lz = sin(elevation)
//
// The three canonical cases isolate one component each. Visibility is
// always 1 for a distant light.
func TestDistantLightSource_ElevationAzimuth(t *testing.T) {
	cases := []struct {
		name                   string
		azimuth, elevation     float64
		wantLx, wantLy, wantLz float64
	}{
		// elevation=90 → light directly overhead → (0, 0, 1).
		{"overhead", 0, 90, 0, 0, 1},
		// elevation=0, azimuth=0 → light along +X axis → (1, 0, 0).
		{"east-horizon", 0, 0, 1, 0, 0},
		// elevation=0, azimuth=90 → light along +Y axis → (0, 1, 0).
		{"north-horizon", 90, 0, 0, 1, 0},
		// elevation=45, azimuth=0 → (cos45, 0, sin45).
		{"45deg-east", 0, 45, math.Cos(math.Pi / 4), 0, math.Sin(math.Pi / 4)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDistantLightSource(tc.azimuth, tc.elevation)
			// Surface coordinates are irrelevant — distant light.
			lx, ly, lz, vis := d.Direction(123, 456, 7.89)
			if !approxEq(lx, tc.wantLx) || !approxEq(ly, tc.wantLy) || !approxEq(lz, tc.wantLz) {
				t.Errorf("Direction(az=%g, el=%g) = (%g, %g, %g); want (%g, %g, %g)",
					tc.azimuth, tc.elevation, lx, ly, lz, tc.wantLx, tc.wantLy, tc.wantLz)
			}
			if vis != 1.0 {
				t.Errorf("Visibility = %g; want 1.0 (distant light is uniform)", vis)
			}
			// Sanity: unit length.
			mag := math.Sqrt(lx*lx + ly*ly + lz*lz)
			if !approxEq(mag, 1.0) {
				t.Errorf("Direction vector not unit length: |L| = %g", mag)
			}
		})
	}
}

// TestPointLightSource_DirectionUnit verifies the point-light direction
// is the unit vector from surface point to light position, and that
// degenerate (surface == light) returns a safe up vector instead of
// NaN.
func TestPointLightSource_DirectionUnit(t *testing.T) {
	// Light at (10, 0, 0); surface at origin → direction (1, 0, 0).
	p := NewPointLightSource(10, 0, 0)
	lx, ly, lz, vis := p.Direction(0, 0, 0)
	if !approxEq(lx, 1) || !approxEq(ly, 0) || !approxEq(lz, 0) || vis != 1.0 {
		t.Errorf("Direction(0,0,0) toward (10,0,0) = (%g,%g,%g,vis=%g); want (1,0,0,1)", lx, ly, lz, vis)
	}
	// Surface in front of light along z=5; light directly above at (0,0,10)
	// surface at (0,0,5) → direction (0,0,1).
	p2 := NewPointLightSource(0, 0, 10)
	lx, ly, lz, _ = p2.Direction(0, 0, 5)
	if !approxEq(lx, 0) || !approxEq(ly, 0) || !approxEq(lz, 1) {
		t.Errorf("Direction(0,0,5) toward (0,0,10) = (%g,%g,%g); want (0,0,1)", lx, ly, lz)
	}
	// Degenerate: surface coincides with light. Must not return NaN.
	p3 := NewPointLightSource(5, 5, 5)
	lx, ly, lz, _ = p3.Direction(5, 5, 5)
	if math.IsNaN(lx) || math.IsNaN(ly) || math.IsNaN(lz) {
		t.Errorf("Direction at light position returned NaN: (%g,%g,%g)", lx, ly, lz)
	}
}

// TestSpotLightSource_DirectionUnit verifies the spot light's direction
// computation (same shape as point light: surface → light position).
// Cone-narrowing is stubbed to visibility=1 for Phase 6 (Phase 6.1
// will replace).
func TestSpotLightSource_DirectionUnit(t *testing.T) {
	// Spot at (0, 0, 10) pointing at origin; surface at origin →
	// direction (0, 0, 1). Cone angles/exponents irrelevant for the
	// direction calc.
	s := NewSpotLightSource(0, 0, 10, 0, 0, 0, 1, 45)
	lx, ly, lz, vis := s.Direction(0, 0, 0)
	if !approxEq(lx, 0) || !approxEq(ly, 0) || !approxEq(lz, 1) {
		t.Errorf("Direction = (%g,%g,%g); want (0,0,1)", lx, ly, lz)
	}
	if vis != 1.0 {
		// Phase 6 stub: visibility must be 1 until 6.1 lands.
		t.Errorf("Visibility = %g; Phase 6 stubs cone-narrowing to 1", vis)
	}
}
