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
//
// Azimuth and elevation are normalized modulo 360° before the radian
// conversion so that wrapped inputs (e.g. WPT azimuth-and-elevation
// reftest uses azimuth=-45 vs the reference's 315, elevation=390 vs
// 30) produce identical outputs. `math.Cos`/`math.Sin` are periodic
// and produce mathematically-equivalent results for unwrapped inputs,
// but the explicit normalization mirrors the spec convention and
// guards against any future change that would care about the canonical
// degree value (e.g. surfacing the angle for debug output).
func (d *DistantLightSource) Direction(_, _, _ float64) (Lx, Ly, Lz, Visibility float64) {
	az := normalizeDeg360(d.AzimuthDeg) * math.Pi / 180.0
	el := normalizeDeg360(d.ElevationDeg) * math.Pi / 180.0
	Lx = math.Cos(az) * math.Cos(el)
	Ly = math.Sin(az) * math.Cos(el)
	Lz = math.Sin(el)
	Visibility = 1.0
	return
}

// normalizeDeg360 reduces an arbitrary angle in degrees to the
// canonical [0, 360) range. Mirrors what Blink does in
// distant_light_source.cc via the implicit periodicity of cosf/sinf —
// expressed here as an explicit mod so the angle is canonical before
// the trig call.
func normalizeDeg360(deg float64) float64 {
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	return d
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
// The cone math follows Skia's `$spotlight_scale` runtime shader
// (src/sksl/sksl_rt_shader.sksl) which is what Blink delegates to. Let
// L = surface→light unit direction (returned by Direction), and
// coneAxis = unit vector from light position toward PointsAt. Define
// `cosAngle = -dot(L, coneAxis) = dot(lightToSurface, coneAxis)` — the
// cosine of the angle between the cone axis and the light-to-pixel
// direction. Let `cosCutoff = cos(LimitingConeAngle in radians)`.
//   - If `cosAngle < cosCutoff`: pixel is outside the cone → visibility 0.
//   - Else: visibility = pow(cosAngle, SpecularExponent), with a small
//     linear fade near the cone boundary (kConeAAThreshold = 0.016) to
//     avoid aliasing at the rim. This mirrors the SkSL exactly.
//
// Cosine is even, so a negative `LimitingConeAngle` yields the same
// cosCutoff as its positive counterpart — satisfying the WPT
// `limiting-cone-angle.html` reftest invariant without any explicit
// `abs()` call.
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
// (x, y, z) toward (X, Y, Z) — same shape as PointLightSource — plus
// the cone-narrowing visibility factor. The formula mirrors Skia's
// `$spotlight_scale` (src/sksl/sksl_rt_shader.sksl); see the type
// docstring for the math.
func (s *SpotLightSource) Direction(x, y, z float64) (Lx, Ly, Lz, Visibility float64) {
	dx := s.X - x
	dy := s.Y - y
	dz := s.Z - z
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length == 0 {
		return 0, 0, 1, 1.0
	}
	inv := 1.0 / length
	Lx = dx * inv
	Ly = dy * inv
	Lz = dz * inv

	// Cone axis: unit vector from light position toward PointsAt.
	axDx := s.PointsAtX - s.X
	axDy := s.PointsAtY - s.Y
	axDz := s.PointsAtZ - s.Z
	axLen := math.Sqrt(axDx*axDx + axDy*axDy + axDz*axDz)
	if axLen == 0 {
		// Degenerate cone axis. Per Blink/Skia, a zero-length axis means
		// the spotlight has no defined direction; emit full visibility
		// rather than NaN. Mirrors $surface_to_light's defensive
		// normalize-or-zero pattern.
		return Lx, Ly, Lz, 1.0
	}
	axInv := 1.0 / axLen
	cax := axDx * axInv
	cay := axDy * axInv
	caz := axDz * axInv

	// cosAngle = dot(lightToSurface, coneAxis) = -dot(L, coneAxis).
	// Equivalent to Skia's `-dot(surfaceToLight, lightDir)`.
	cosAngle := -(Lx*cax + Ly*cay + Lz*caz)

	// cosCutoff = cos(LimitingConeAngle°). Cosine is even, so a
	// negative LimitingConeAngle produces the same cutoff as its
	// positive counterpart (the WPT limiting-cone-angle.html
	// invariant). Blink's fe_lighting.cc clamps LCA to 90 when the
	// attribute is unset (0) or out of [-90, 90]; that clamp lives in
	// the adapter so this primitive only sees a well-formed value.
	cosCutoff := math.Cos(s.LimitingConeAngle * math.Pi / 180.0)

	const coneAAThreshold = 0.016
	const coneScale = 1.0 / coneAAThreshold

	if cosAngle < cosCutoff {
		Visibility = 0
		return
	}
	// Inside the cone: scale = cosAngle^specularExponent, with a linear
	// fade across the AA-threshold band at the rim. Matches Skia's
	// `$spotlight_scale` exactly.
	scale := math.Pow(cosAngle, s.SpecularExponent)
	if cosAngle < cosCutoff+coneAAThreshold {
		scale *= (cosAngle - cosCutoff) * coneScale
	}
	if scale < 0 {
		scale = 0
	}
	Visibility = scale
	return
}
