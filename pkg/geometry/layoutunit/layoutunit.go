// Package layoutunit implements Blink's LayoutUnit fixed-point pixel type.
//
// LayoutUnit is a 26.6 fixed-point value: a 32-bit signed integer holding
// CSS-pixel quantities in units of 1/64 px. The 6-bit fractional choice
// matches HarfBuzz's hb_position_t and FreeType's 26.6 format, so the
// text-shaping boundary doesn't introduce an extra precision impedance
// mismatch.
//
// The point of LayoutUnit (vs float64) is bit-exact reproducibility:
// layout output drives paint invalidation, scroll anchoring, and edge
// snapping. Float associativity failures cause sibling drift that
// integer fixed-point eliminates.
//
// Mirror of third_party/blink/renderer/platform/geometry/layout_unit.h:
//   using LayoutUnit = FixedPoint<6, int32_t>;
//   inline constexpr LayoutUnit kIndefiniteSize(-1);
//
// All arithmetic saturates to the int32 range. Float entry must go
// through one of FromFloat64Round/Ceil/Floor — Go has no implicit
// conversion, but exposing only explicit-rounding constructors enforces
// the discipline at every call site.
package layoutunit

import "math"

// FractionalBits is the number of fractional bits (Blink: kFractionalBits = 6).
// One CSS pixel is FixedPointDenominator (= 64) raw units.
const (
	FractionalBits        = 6
	FixedPointDenominator = 1 << FractionalBits // 64
)

const (
	rawMax int64 = math.MaxInt32
	rawMin int64 = math.MinInt32
)

// IntMax is the largest CSS-pixel integer representable without saturation.
// Mirrors Blink's kIntMax ≈ 33,554,431 (= rawMax / 64, ~33M px headroom).
const IntMax = math.MaxInt32 / FixedPointDenominator

// Epsilon is one raw unit, the minimum representable sub-pixel quantum
// (= 1/64 px ≈ 0.015625). Mirrors Blink's kEpsilon.
const Epsilon = 1.0 / FixedPointDenominator

// LayoutUnit is a 26.6 fixed-point CSS-pixel value.
type LayoutUnit struct {
	raw int32
}

// Zero is the LayoutUnit representing 0 px.
var Zero = LayoutUnit{}

// IndefiniteSize is the sentinel for "indefinite" sizes, mirroring
// Blink's kIndefiniteSize (= -1 CSS px = -64 raw).
var IndefiniteSize = LayoutUnit{raw: -FixedPointDenominator}

// MaxValue is the largest representable LayoutUnit (raw = math.MaxInt32).
var MaxValue = LayoutUnit{raw: math.MaxInt32}

// MinValue is the smallest representable LayoutUnit (raw = math.MinInt32).
var MinValue = LayoutUnit{raw: math.MinInt32}

// New constructs a LayoutUnit from an integer pixel value, saturating on
// overflow. Mirrors Blink's LayoutUnit(int v) ctor.
func New(px int) LayoutUnit {
	return clamp64(int64(px) * FixedPointDenominator)
}

// FromRaw constructs from a raw 26.6 value, saturating to int32 range.
// Mirrors Blink's FromRawValueWithClamp.
func FromRaw(r int64) LayoutUnit {
	return clamp64(r)
}

// FromFloat64Round converts a float64 to LayoutUnit using round-half-away-
// from-zero on the raw quantum. Mirrors Blink's FromFloatRound /
// FromDoubleRound.
func FromFloat64Round(v float64) LayoutUnit {
	return clamp64(int64(math.Round(v * FixedPointDenominator)))
}

// FromFloat64Ceil converts a float64 to LayoutUnit using ceil on the raw
// quantum. Mirrors Blink's FromFloatCeil — used at text-shaping exit
// (ShapeResult::SnappedWidth) so the LayoutUnit width is a superset of
// the float width.
func FromFloat64Ceil(v float64) LayoutUnit {
	return clamp64(int64(math.Ceil(v * FixedPointDenominator)))
}

// FromFloat64Floor converts a float64 to LayoutUnit using floor on the
// raw quantum. Mirrors Blink's FromFloatFloor — used at text-shaping for
// SnappedStartPositionForOffset.
func FromFloat64Floor(v float64) LayoutUnit {
	return clamp64(int64(math.Floor(v * FixedPointDenominator)))
}

// Raw returns the underlying 26.6 fixed-point value.
func (a LayoutUnit) Raw() int32 { return a.raw }

// Float64 returns lossless conversion to float64.
func (a LayoutUnit) Float64() float64 {
	return float64(a.raw) / FixedPointDenominator
}

// ToInt truncates toward zero, returning the integer-pixel part.
// Mirrors Blink's ToInt().
func (a LayoutUnit) ToInt() int32 {
	return a.raw / FixedPointDenominator
}

// Round returns the integer-pixel value using round-half-away-from-zero.
// Mirrors Blink's Round() ((value + denom/2) >> bits for non-negative;
// -((-value + denom/2) >> bits) for negative).
func (a LayoutUnit) Round() int32 {
	r := int64(a.raw)
	if r >= 0 {
		return int32((r + FixedPointDenominator/2) >> FractionalBits)
	}
	return -int32((-r + FixedPointDenominator/2) >> FractionalBits)
}

// Floor returns the integer-pixel value rounded toward -infinity.
// Mirrors Blink's Floor() (= value_ >> kFractionalBits, arithmetic shift).
func (a LayoutUnit) Floor() int32 {
	return a.raw >> FractionalBits
}

// Ceil returns the integer-pixel value rounded toward +infinity.
// Mirrors Blink's Ceil() (= SaturateAdd(value_, denom-1) >> bits).
func (a LayoutUnit) Ceil() int32 {
	sum := int64(a.raw) + (FixedPointDenominator - 1)
	if sum > rawMax {
		sum = rawMax
	}
	return int32(sum >> FractionalBits)
}

// Fraction returns the sub-pixel remainder (-1 px < x < 1 px), with the
// same sign as the receiver. Mirrors Blink's Fraction(): for positive
// values, the low 6 bits; for negative values, the same magnitude with
// negative sign.
func (a LayoutUnit) Fraction() LayoutUnit {
	return LayoutUnit{raw: a.raw % FixedPointDenominator}
}

// Abs returns |a|, saturating at MaxValue when raw == MinInt32 (since
// -MinInt32 doesn't fit in int32).
func (a LayoutUnit) Abs() LayoutUnit {
	if a.raw == math.MinInt32 {
		return MaxValue
	}
	if a.raw < 0 {
		return LayoutUnit{raw: -a.raw}
	}
	return a
}

// Add returns a+b with int32 saturation. Mirrors Blink's ClampAdd.
func (a LayoutUnit) Add(b LayoutUnit) LayoutUnit {
	return clamp64(int64(a.raw) + int64(b.raw))
}

// Sub returns a-b with int32 saturation. Mirrors Blink's ClampSub.
func (a LayoutUnit) Sub(b LayoutUnit) LayoutUnit {
	return clamp64(int64(a.raw) - int64(b.raw))
}

// Mul returns a*b in LayoutUnit semantics: int64 widening, divide by
// FixedPointDenominator (arithmetic right-shift), saturate. Mirrors
// Blink's BoundedMultiply >> kFractionalBits.
func (a LayoutUnit) Mul(b LayoutUnit) LayoutUnit {
	product := int64(a.raw) * int64(b.raw)
	return clamp64(product >> FractionalBits)
}

// MulInt scales by an integer with saturation.
func (a LayoutUnit) MulInt(b int) LayoutUnit {
	return clamp64(int64(a.raw) * int64(b))
}

// Div returns a/b in LayoutUnit semantics: shift up by FixedPointDenominator
// then int-divide, saturating. b == 0 saturates to MaxValue.
// Mirrors Blink's operator/.
func (a LayoutUnit) Div(b LayoutUnit) LayoutUnit {
	if b.raw == 0 {
		return MaxValue
	}
	return clamp64(int64(a.raw) * FixedPointDenominator / int64(b.raw))
}

// DivInt divides by an integer with saturation. b == 0 saturates.
func (a LayoutUnit) DivInt(b int) LayoutUnit {
	if b == 0 {
		return MaxValue
	}
	return clamp64(int64(a.raw) / int64(b))
}

// MulDiv computes a*m/d using int64 widening to avoid intermediate
// saturation. Used by percentage-resolution paths per Blink (avoids the
// "(a*m) saturates before /d brings it back" pitfall).
func (a LayoutUnit) MulDiv(m, d LayoutUnit) LayoutUnit {
	if d.raw == 0 {
		return MaxValue
	}
	return clamp64(int64(a.raw) * int64(m.raw) / int64(d.raw))
}

// IsIndefinite reports whether a is the kIndefinite sentinel.
func (a LayoutUnit) IsIndefinite() bool {
	return a.raw == IndefiniteSize.raw
}

// IsZero reports whether a == 0.
func (a LayoutUnit) IsZero() bool { return a.raw == 0 }

// Less reports whether a < b.
func (a LayoutUnit) Less(b LayoutUnit) bool { return a.raw < b.raw }

// LessEqual reports whether a <= b.
func (a LayoutUnit) LessEqual(b LayoutUnit) bool { return a.raw <= b.raw }

// Min returns the smaller of a, b.
func Min(a, b LayoutUnit) LayoutUnit {
	if a.raw < b.raw {
		return a
	}
	return b
}

// Max returns the larger of a, b.
func Max(a, b LayoutUnit) LayoutUnit {
	if a.raw > b.raw {
		return a
	}
	return b
}

// clamp64 saturates an int64 raw value to int32 range and wraps it in a
// LayoutUnit. The single saturation site for all arithmetic.
func clamp64(v int64) LayoutUnit {
	if v > rawMax {
		return MaxValue
	}
	if v < rawMin {
		return MinValue
	}
	return LayoutUnit{raw: int32(v)}
}
