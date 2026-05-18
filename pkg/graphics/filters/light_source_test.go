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

// TestDistantLightSource_ElevationAzimuthModulo verifies that azimuth
// and elevation values outside the canonical [0, 360) range are
// normalized before the trig conversion. Per SVG Filter Effects 1
// §"feDistantLight" the WPT `azimuth-and-elevation.html` reftest pairs
// (azimuth=-45, elevation=390) with the reference (azimuth=315,
// elevation=30) — same final direction once both pairs reduce mod 360.
// Mirrors Blink distant_light_source.cc's GetDirection (the angle
// math is implemented in terms of the raw degree values; the mod-360
// normalization is necessary to keep cos/sin numerically equivalent
// across these wrapped inputs).
func TestDistantLightSource_ElevationAzimuthModulo(t *testing.T) {
	type pair struct{ az, el float64 }
	cases := []struct {
		name string
		a, b pair // both pairs must produce the same direction vector.
	}{
		// WPT azimuth-and-elevation.html invariant.
		{"wpt-azimuth-elevation", pair{-45, 390}, pair{315, 30}},
		// Negative azimuth normalization.
		{"negative-azimuth", pair{-90, 0}, pair{270, 0}},
		// Elevation > 360.
		{"elevation-over-360", pair{0, 450}, pair{0, 90}},
		// Both wrapped.
		{"both-wrapped", pair{720, -720}, pair{0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			da := NewDistantLightSource(tc.a.az, tc.a.el)
			db := NewDistantLightSource(tc.b.az, tc.b.el)
			ax, ay, az, _ := da.Direction(0, 0, 0)
			bx, by, bz, _ := db.Direction(0, 0, 0)
			if !approxEq(ax, bx) || !approxEq(ay, by) || !approxEq(az, bz) {
				t.Errorf("Direction mismatch:\n  a (az=%g, el=%g) = (%g, %g, %g)\n  b (az=%g, el=%g) = (%g, %g, %g)",
					tc.a.az, tc.a.el, ax, ay, az,
					tc.b.az, tc.b.el, bx, by, bz)
			}
		})
	}
}

// TestSpotLightSource_LimitingConeAngle verifies the cone-narrowing
// visibility math implemented in Direction(). Mirrors Blink's
// spot_light_source path through Skia's $spotlight_scale shader (see
// sksl_rt_shader.sksl): if cos(angle_between_lightDir_and_lightToSurf) <
// cos(limitingConeAngle), visibility is 0; otherwise visibility is
// pow(cosAngle, specularExponent) with a small AA fade near the
// boundary.
//
// The WPT `limiting-cone-angle.html` reftest asserts that a negative
// limitingConeAngle behaves identically to its positive counterpart.
// Cosine is even, so cos(-45°) == cos(45°) and Direction() must return
// the SAME visibility for both.
func TestSpotLightSource_LimitingConeAngle(t *testing.T) {
	// Light at (0, 0, 10) pointing at origin → cone axis = (0, 0, -1).
	// Surface at origin → surfaceToLight = (0, 0, 1). cos(angle) = 1
	// (the surface is on-axis). Well inside any positive cone.
	t.Run("on-axis inside cone", func(t *testing.T) {
		s := NewSpotLightSource(0, 0, 10, 0, 0, 0, 1, 45)
		_, _, _, vis := s.Direction(0, 0, 0)
		if !approxEq(vis, 1.0) {
			t.Errorf("on-axis visibility = %g; want 1.0", vis)
		}
	})

	// Negative-vs-positive cone angle equivalence (the WPT invariant).
	t.Run("negative cone angle equals positive", func(t *testing.T) {
		// Surface displaced sideways so we get a non-trivial cone angle.
		sxPos := NewSpotLightSource(0, 0, 10, 0, 0, 0, 1, 45)
		sxNeg := NewSpotLightSource(0, 0, 10, 0, 0, 0, 1, -45)
		// Sample at (3, 4, 0): the angle between surfaceToLight and
		// cone-axis is acos(10/sqrt(125)) ≈ 26.57° — inside a 45° cone.
		_, _, _, vp := sxPos.Direction(3, 4, 0)
		_, _, _, vn := sxNeg.Direction(3, 4, 0)
		if !approxEq(vp, vn) {
			t.Errorf("vis(LCA=45) = %g but vis(LCA=-45) = %g; should be identical", vp, vn)
		}
		if vp <= 0 {
			t.Errorf("visibility inside 45° cone should be > 0; got %g", vp)
		}
	})

	// A surface point outside the cone must be fully occluded.
	t.Run("outside cone occluded", func(t *testing.T) {
		// 30° cone, sample far off-axis.
		s := NewSpotLightSource(0, 0, 10, 0, 0, 0, 1, 30)
		// (10, 0, 0) → surfaceToLight axis-cos = 10/sqrt(200) ≈ 0.707
		// = cos(45°). 45° > 30° → outside.
		_, _, _, vis := s.Direction(10, 0, 0)
		if vis != 0 {
			t.Errorf("outside-cone visibility = %g; want 0", vis)
		}
		// And with negative LCA — same outcome (cosine is even).
		sNeg := NewSpotLightSource(0, 0, 10, 0, 0, 0, 1, -30)
		_, _, _, visNeg := sNeg.Direction(10, 0, 0)
		if visNeg != 0 {
			t.Errorf("outside-cone visibility (LCA=-30) = %g; want 0", visNeg)
		}
	})
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
// For an on-axis surface point well inside the cone, the cone-narrowing
// factor is pow(cosAngle, specularExponent) = pow(1, 1) = 1.
func TestSpotLightSource_DirectionUnit(t *testing.T) {
	// Spot at (0, 0, 10) pointing at origin; surface at origin →
	// direction (0, 0, 1). The surface lies on the cone axis so
	// cosAngle = 1, visibility = 1.
	s := NewSpotLightSource(0, 0, 10, 0, 0, 0, 1, 45)
	lx, ly, lz, vis := s.Direction(0, 0, 0)
	if !approxEq(lx, 0) || !approxEq(ly, 0) || !approxEq(lz, 1) {
		t.Errorf("Direction = (%g,%g,%g); want (0,0,1)", lx, ly, lz)
	}
	if !approxEq(vis, 1.0) {
		t.Errorf("Visibility = %g; want 1.0 (on-axis surface, well inside cone)", vis)
	}
}
