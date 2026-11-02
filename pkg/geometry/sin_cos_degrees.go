package geometry

import "math"

// SinCos is gfx::SinCos (ui/gfx/geometry/sin_cos_degrees.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type SinCos struct {
	Sin float64
	Cos float64
}

// IsZeroAngle mirrors gfx::SinCos::IsZeroAngle.
func (s SinCos) IsZeroAngle() bool { return s.Sin == 0 && s.Cos == 1 }

// sinCosN45 holds exact values for multiples of 45°, indexed by octant.
var sinCosN45 = [8]SinCos{
	{0, 1},
	{math.Sqrt2 / 2, math.Sqrt2 / 2},
	{1, 0},
	{math.Sqrt2 / 2, -math.Sqrt2 / 2},
	{0, -1},
	{-math.Sqrt2 / 2, -math.Sqrt2 / 2},
	{-1, 0},
	{-math.Sqrt2 / 2, math.Sqrt2 / 2},
}

// SinCosDegrees returns sin and cos of degrees with exact results at
// multiples of 90°, range-reducing to [0..45] first. Port of
// gfx::SinCosDegrees (sin_cos_degrees.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func SinCosDegrees(degrees float64) SinCos {
	if degrees > -90000000.0 && degrees < 90000000.0 {
		n45degrees := degrees / 45.0
		octant := int(n45degrees)
		if float64(octant) == n45degrees {
			return sinCosN45[octant&7]
		}
		if degrees < 0 {
			// Moves the range reduction below into [0..45] instead of
			// [-45..0].
			octant--
		}
		degrees -= float64(octant) * 45.0 // Range-reduce to [0..45].
		// Odd octants: sin(90 - a) = cos(a); flip below.
		if octant&1 != 0 {
			degrees = 45.0 - degrees
		}
		rad := degrees * math.Pi / 180
		s := math.Sin(rad)
		c := math.Cos(rad)
		if (octant+1)&2 != 0 {
			s, c = c, s
		}
		// Sine is negative in 180..360.
		if octant&4 != 0 {
			s = -s
		}
		// Cosine is negative in 90..270.
		if (octant+2)&4 != 0 {
			c = -c
		}
		return SinCos{s, c}
	}
	// Slow path for extreme cases.
	degrees = math.Mod(degrees, 360.0)
	rad := degrees * math.Pi / 180
	return SinCos{math.Sin(rad), math.Cos(rad)}
}
