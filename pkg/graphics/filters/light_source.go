package filters

import "math"

// LightSource is the abstract interface every concrete SVG light source
// implements (`<feDistantLight>`, `<fePointLight>`, `<feSpotLight>`).
// Mirrors Blink's platform/graphics/filters/light_source.{h,cc} — a
// single virtual method that, given the absolute device-pixel surface
// coordinate of the pixel being shaded, returns the unit-length light
// direction L pointing FROM the surface TOWARDS the light source, plus a
// visibility factor in [0, 1] that lets cone-limited sources (spot light)
// fade off.
//
// The z-coordinate of the surface point is the height-field "lift" from
// the alpha channel (per Filter Effects 1 §"feDiffuseLighting" — the
// alpha bump z = SurfaceScale * alpha / 255). The caller computes this
// and supplies it as z.
//
// Visibility is multiplicative on the lighting intensity. A distant
// light always returns 1 (uniform across the surface). A point light
// also returns 1 (the light shines in all directions). Only a spot
// light's narrowing cone reduces visibility below 1. For Phase 6 the
// spot light's cone-narrowing is stubbed to always return 1; the
// LimitingConeAngle math lands in Phase 6.1.
type LightSource interface {
	// Direction returns the unit-length direction vector pointing from
	// the surface point (x, y, z) toward the light, and a visibility
	// factor in [0, 1] for cone-limited sources. (x, y, z) is in
	// absolute device pixels with z = SurfaceScale * alpha / 255.
	Direction(x, y, z float64) (Lx, Ly, Lz, Visibility float64)
}

// DistantLightSource mirrors Blink's DistantLightSource
// (platform/graphics/filters/distant_light_source.{h,cc}). The direction
// is independent of the surface point: it is determined entirely by the
// `<feDistantLight>` element's `azimuth` + `elevation` attributes, both
// in degrees per SVG Filter Effects 1 §"feDistantLight".
//
// The unit vector L (pointing from surface to light) is:
//
//	Lx = cos(azimuth) * cos(elevation)
//	Ly = sin(azimuth) * cos(elevation)
//	Lz = sin(elevation)
//
// Both angles are converted from degrees to radians by the caller of
// NewDistantLightSource (the unit-test exposed signature takes degrees,
// matching the SVG attribute convention). Visibility is always 1 — a
// distant light shines uniformly across the entire surface.
type DistantLightSource struct {
	// AzimuthDeg / ElevationDeg are stored in degrees (SVG attribute
	// convention). Direction() converts to radians internally.
	AzimuthDeg   float64
	ElevationDeg float64
}

// NewDistantLightSource constructs a DistantLightSource from the
// azimuth + elevation attribute values (both in degrees, per the SVG
// attribute convention).
func NewDistantLightSource(azimuthDeg, elevationDeg float64) *DistantLightSource {
	return &DistantLightSource{
		AzimuthDeg:   azimuthDeg,
		ElevationDeg: elevationDeg,
	}
}

// Direction implements LightSource. Returns the constant precomputed
// unit vector + visibility=1. Per Blink distant_light_source.cc the
// surface-point argument is ignored.
func (d *DistantLightSource) Direction(_, _, _ float64) (Lx, Ly, Lz, Visibility float64) {
	az := d.AzimuthDeg * math.Pi / 180.0
	el := d.ElevationDeg * math.Pi / 180.0
	Lx = math.Cos(az) * math.Cos(el)
	Ly = math.Sin(az) * math.Cos(el)
	Lz = math.Sin(el)
	Visibility = 1.0
	return
}

// PointLightSource mirrors Blink's PointLightSource
// (platform/graphics/filters/point_light_source.{h,cc}). The direction
// is the unit vector from the surface point toward the fixed light
// position. Visibility is always 1 — a point light radiates in all
// directions.
type PointLightSource struct {
	// X, Y, Z are the light's position in absolute device pixels (z
	// uses the same units as x/y — the height-field convention).
	X, Y, Z float64
}

// NewPointLightSource constructs a PointLightSource at the given device-
// pixel position.
func NewPointLightSource(x, y, z float64) *PointLightSource {
	return &PointLightSource{X: x, Y: y, Z: z}
}

// Direction implements LightSource. Returns the unit vector from
// (x, y, z) toward (P.X, P.Y, P.Z), and visibility=1.
func (p *PointLightSource) Direction(x, y, z float64) (Lx, Ly, Lz, Visibility float64) {
	dx := p.X - x
	dy := p.Y - y
	dz := p.Z - z
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length == 0 {
		// Degenerate: surface point coincides with the light. Mirror
		// Blink's defensive behavior: pick the up direction so the
		// downstream BRDF computes a finite (non-NaN) result.
		return 0, 0, 1, 1.0
	}
	inv := 1.0 / length
	return dx * inv, dy * inv, dz * inv, 1.0
}

// SpotLightSource mirrors Blink's SpotLightSource
// (platform/graphics/filters/spot_light_source.{h,cc}). Like a point
// light, but the beam is restricted to a cone pointing from the light
// position toward (PointsAtX, PointsAtY, PointsAtZ). Within the cone,
// the intensity falls off as cos(angle_to_cone_axis)^SpecularExponent.
// Outside the LimitingConeAngle, visibility is 0.
//
// Phase 6 ships the position + cone-axis arithmetic so the Direction()
// vector is correct for a `<feSpotLight>` test, but the visibility
// factor always returns 1: the cone-narrowing math (LimitingConeAngle
// hard cutoff + exponent falloff) is deferred to Phase 6.1. None of the
// 4 in-scope bucket-J tests exercises a `<feSpotLight>` — the stub
// keeps the type buildable so future work is incremental.
type SpotLightSource struct {
	// Position of the light.
	X, Y, Z float64
	// PointsAt is the cone-axis target point. The cone axis is the
	// unit vector from (X, Y, Z) toward (PointsAtX, PointsAtY,
	// PointsAtZ).
	PointsAtX, PointsAtY, PointsAtZ float64
	// SpecularExponent controls the cosine falloff inside the cone
	// (cos(theta)^SpecularExponent). Used by the cone-narrowing math
	// (Phase 6.1).
	SpecularExponent float64
	// LimitingConeAngle is the half-angle of the cone in degrees.
	// Beyond this angle the light is fully occluded. The SVG default
	// when the attribute is missing is to omit cone-narrowing entirely
	// — same as a 90° half-angle (full hemisphere). Phase 6 always
	// reports visibility=1 regardless of this value.
	LimitingConeAngle float64
}

// NewSpotLightSource constructs a SpotLightSource. PointsAt parameters
// give the cone-axis target.
func NewSpotLightSource(x, y, z, pointsAtX, pointsAtY, pointsAtZ, specularExponent, limitingConeAngleDeg float64) *SpotLightSource {
	return &SpotLightSource{
		X: x, Y: y, Z: z,
		PointsAtX: pointsAtX, PointsAtY: pointsAtY, PointsAtZ: pointsAtZ,
		SpecularExponent:  specularExponent,
		LimitingConeAngle: limitingConeAngleDeg,
	}
}

// Direction implements LightSource. Returns the unit vector from
// (x, y, z) toward (X, Y, Z) — same as PointLightSource — and a
// stubbed visibility of 1.
//
// TODO(LOU-129 follow-up Phase 6.1): implement the cone-narrowing
// visibility factor (cos(theta)^SpecularExponent inside the cone, 0
// outside LimitingConeAngle). Mirror Blink spot_light_source.cc's
// GetColor / GetPaintingData. Until then this is functionally a point
// light, which is sufficient for none of the in-scope bucket-J tests
// but does not regress anything either (no bucket-J test uses
// `<feSpotLight>`).
func (s *SpotLightSource) Direction(x, y, z float64) (Lx, Ly, Lz, Visibility float64) {
	dx := s.X - x
	dy := s.Y - y
	dz := s.Z - z
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length == 0 {
		return 0, 0, 1, 1.0
	}
	inv := 1.0 / length
	return dx * inv, dy * inv, dz * inv, 1.0
}
