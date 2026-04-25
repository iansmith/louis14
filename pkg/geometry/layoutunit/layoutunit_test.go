package layoutunit

import (
	"math"
	"testing"
)

func TestConstants(t *testing.T) {
	if FractionalBits != 6 {
		t.Errorf("FractionalBits = %d, want 6", FractionalBits)
	}
	if FixedPointDenominator != 64 {
		t.Errorf("FixedPointDenominator = %d, want 64", FixedPointDenominator)
	}
	if IntMax != math.MaxInt32/64 {
		t.Errorf("IntMax = %d, want %d", IntMax, math.MaxInt32/64)
	}
	if Epsilon != 1.0/64.0 {
		t.Errorf("Epsilon = %v, want 1/64", Epsilon)
	}
	if IndefiniteSize.Raw() != -64 {
		t.Errorf("IndefiniteSize.Raw() = %d, want -64", IndefiniteSize.Raw())
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		px      int
		wantRaw int32
	}{
		{0, 0},
		{1, 64},
		{-1, -64},
		{96, 6144},
		{-96, -6144},
		{IntMax, math.MaxInt32 - (math.MaxInt32 % 64)}, // 33554431 * 64 ≤ MaxInt32
	}
	for _, tt := range tests {
		got := New(tt.px).Raw()
		if got != tt.wantRaw {
			t.Errorf("New(%d).Raw() = %d, want %d", tt.px, got, tt.wantRaw)
		}
	}

	// Saturation past IntMax.
	if r := New(math.MaxInt32).Raw(); r != math.MaxInt32 {
		t.Errorf("New(MaxInt32).Raw() = %d, want saturated MaxInt32", r)
	}
	if r := New(math.MinInt32).Raw(); r != math.MinInt32 {
		t.Errorf("New(MinInt32).Raw() = %d, want saturated MinInt32", r)
	}
}

func TestFromFloat64Round(t *testing.T) {
	tests := []struct {
		v       float64
		wantRaw int32
	}{
		{0.0, 0},
		{1.0, 64},
		{-1.0, -64},
		{0.5, 32},   // 0.5 px → raw 32
		{-0.5, -32}, // round-half-to-even via math.Round? Go math.Round is round-half-away-from-zero.
		{0.015625, 1},
		{0.0078125, 1},      // half-quantum: math.Round is half-away-from-zero → 1
		{1.0/64 + 1e-12, 1}, // just above one raw, rounds to 1
	}
	for _, tt := range tests {
		got := FromFloat64Round(tt.v).Raw()
		if got != tt.wantRaw {
			t.Errorf("FromFloat64Round(%v).Raw() = %d, want %d", tt.v, got, tt.wantRaw)
		}
	}

	// Half-raw rounding: 0.5 raw = 0.0078125 px
	if r := FromFloat64Round(0.0078125).Raw(); r != 1 {
		t.Errorf("FromFloat64Round(0.0078125).Raw() = %d, want 1 (half-away)", r)
	}

	// Saturation.
	if r := FromFloat64Round(1e30).Raw(); r != math.MaxInt32 {
		t.Errorf("FromFloat64Round(1e30) should saturate, got raw %d", r)
	}
	if r := FromFloat64Round(-1e30).Raw(); r != math.MinInt32 {
		t.Errorf("FromFloat64Round(-1e30) should saturate, got raw %d", r)
	}
}

func TestFromFloat64CeilFloor(t *testing.T) {
	// 1/64 = 0.015625; values just above/below quanta.
	cases := []struct {
		v          float64
		wantCeil   int32
		wantFloor  int32
		wantRound  int32
	}{
		{0.0, 0, 0, 0},
		{0.01, 1, 0, 1},     // 0.01 * 64 = 0.64 → ceil=1, floor=0, round=1
		{0.005, 1, 0, 0},    // 0.005 * 64 = 0.32 → ceil=1, floor=0, round=0
		{-0.01, 0, -1, -1},  // -0.01 * 64 = -0.64 → ceil=0, floor=-1, round=-1
		{1.0, 64, 64, 64},
		{1.5, 96, 96, 96},   // exact-on-quantum
		{1.50001, 97, 96, 96}, // 1.50001 * 64 = 96.0006 → ceil=97, floor=96, round=96
	}
	for _, c := range cases {
		if got := FromFloat64Ceil(c.v).Raw(); got != c.wantCeil {
			t.Errorf("FromFloat64Ceil(%v).Raw() = %d, want %d", c.v, got, c.wantCeil)
		}
		if got := FromFloat64Floor(c.v).Raw(); got != c.wantFloor {
			t.Errorf("FromFloat64Floor(%v).Raw() = %d, want %d", c.v, got, c.wantFloor)
		}
		if got := FromFloat64Round(c.v).Raw(); got != c.wantRound {
			t.Errorf("FromFloat64Round(%v).Raw() = %d, want %d", c.v, got, c.wantRound)
		}
	}
}

func TestFloat64RoundTrip(t *testing.T) {
	// Quanta survive round-trip exactly.
	for _, raw := range []int32{0, 1, -1, 64, -64, 6144, -6144, math.MaxInt32, math.MinInt32} {
		u := FromRaw(int64(raw))
		f := u.Float64()
		back := FromFloat64Round(f).Raw()
		if back != raw {
			t.Errorf("round-trip raw %d → %v → raw %d, want raw unchanged", raw, f, back)
		}
	}
}

func TestRoundFloorCeil(t *testing.T) {
	cases := []struct {
		raw                 int32
		wantRound, wantFloor, wantCeil int32
	}{
		{0, 0, 0, 0},
		{32, 1, 0, 1},     // 0.5 → round=1, floor=0, ceil=1
		{31, 0, 0, 1},     // 0.484 → round=0, floor=0, ceil=1
		{33, 1, 0, 1},     // 0.515 → round=1
		{64, 1, 1, 1},
		{96, 2, 1, 2},     // 1.5 → round=2, floor=1, ceil=2
		{-32, -1, -1, 0},  // -0.5 → round=-1 (half-away-from-zero), floor=-1, ceil=0
		{-31, 0, -1, 0},   // -0.484 → round=0
		{-33, -1, -1, 0},  // -0.515 → round=-1
		{-64, -1, -1, -1},
		{-96, -2, -2, -1}, // -1.5 → round=-2 (half-away), floor=-2, ceil=-1
	}
	for _, c := range cases {
		u := LayoutUnit{raw: c.raw}
		if got := u.Round(); got != c.wantRound {
			t.Errorf("LayoutUnit{%d}.Round() = %d, want %d", c.raw, got, c.wantRound)
		}
		if got := u.Floor(); got != c.wantFloor {
			t.Errorf("LayoutUnit{%d}.Floor() = %d, want %d", c.raw, got, c.wantFloor)
		}
		if got := u.Ceil(); got != c.wantCeil {
			t.Errorf("LayoutUnit{%d}.Ceil() = %d, want %d", c.raw, got, c.wantCeil)
		}
	}
}

func TestFraction(t *testing.T) {
	cases := []struct {
		raw, wantFrac int32
	}{
		{0, 0},
		{32, 32},
		{64, 0},
		{96, 32},   // 1.5 → fraction 0.5 = raw 32
		{-32, -32}, // -0.5 → fraction -0.5
		{-64, 0},
		{-96, -32}, // -1.5 → fraction -0.5
	}
	for _, c := range cases {
		got := LayoutUnit{raw: c.raw}.Fraction().Raw()
		if got != c.wantFrac {
			t.Errorf("LayoutUnit{%d}.Fraction().Raw() = %d, want %d", c.raw, got, c.wantFrac)
		}
	}
}

func TestAbs(t *testing.T) {
	if r := New(5).Abs().Raw(); r != 5*64 {
		t.Errorf("Abs(5) = %d, want 320", r)
	}
	if r := New(-5).Abs().Raw(); r != 5*64 {
		t.Errorf("Abs(-5) = %d, want 320", r)
	}
	if r := New(0).Abs().Raw(); r != 0 {
		t.Errorf("Abs(0) = %d, want 0", r)
	}
	// MinInt32 saturates at MaxValue (since -MinInt32 is unrepresentable).
	if r := MinValue.Abs().Raw(); r != math.MaxInt32 {
		t.Errorf("Abs(MinValue) = %d, want MaxInt32 (saturated)", r)
	}
}

func TestAddSubSaturating(t *testing.T) {
	a := New(10)
	b := New(3)
	if r := a.Add(b).Raw(); r != 13*64 {
		t.Errorf("Add(10, 3).Raw() = %d, want 832", r)
	}
	if r := a.Sub(b).Raw(); r != 7*64 {
		t.Errorf("Sub(10, 3).Raw() = %d, want 448", r)
	}

	// Saturating overflow: MaxValue + 1 = MaxValue.
	if r := MaxValue.Add(New(1)).Raw(); r != math.MaxInt32 {
		t.Errorf("MaxValue.Add(1) should saturate, got %d", r)
	}
	if r := MinValue.Sub(New(1)).Raw(); r != math.MinInt32 {
		t.Errorf("MinValue.Sub(1) should saturate, got %d", r)
	}
	// Saturating underflow on Add(negative): MinValue + (-1) saturates at MinValue.
	if r := MinValue.Add(New(-1)).Raw(); r != math.MinInt32 {
		t.Errorf("MinValue.Add(-1) should saturate, got %d", r)
	}
}

func TestMul(t *testing.T) {
	// 2 * 3 = 6 px → raw (2*64)*(3*64)/64 = 6*64 = 384.
	if r := New(2).Mul(New(3)).Raw(); r != 6*64 {
		t.Errorf("Mul(2, 3).Raw() = %d, want 384", r)
	}
	// 0.5 * 0.5 = 0.25 → raw 32 * 32 / 64 = 16.
	half := FromFloat64Round(0.5)
	if r := half.Mul(half).Raw(); r != 16 {
		t.Errorf("Mul(0.5, 0.5).Raw() = %d, want 16", r)
	}
	// Negative: -1.5 * 2 = -3 → raw -96*128/64 = -192.
	if r := FromFloat64Round(-1.5).Mul(New(2)).Raw(); r != -192 {
		t.Errorf("Mul(-1.5, 2).Raw() = %d, want -192", r)
	}
	// Saturation: MaxValue * 2 saturates.
	if r := MaxValue.Mul(New(2)).Raw(); r != math.MaxInt32 {
		t.Errorf("MaxValue.Mul(2) should saturate, got %d", r)
	}
}

func TestMulInt(t *testing.T) {
	if r := New(5).MulInt(7).Raw(); r != 5*7*64 {
		t.Errorf("MulInt(5, 7).Raw() = %d, want %d", r, 5*7*64)
	}
	if r := New(1).MulInt(IntMax + 1).Raw(); r != math.MaxInt32 {
		t.Errorf("MulInt(1, IntMax+1) should saturate, got %d", r)
	}
}

func TestDiv(t *testing.T) {
	// 10 / 2 = 5 → raw (10*64)*64 / (2*64) = 5*64 = 320.
	if r := New(10).Div(New(2)).Raw(); r != 5*64 {
		t.Errorf("Div(10, 2).Raw() = %d, want 320", r)
	}
	// 1 / 0.5 = 2 → raw (1*64)*64 / 32 = 128 = 2*64.
	if r := New(1).Div(FromFloat64Round(0.5)).Raw(); r != 2*64 {
		t.Errorf("Div(1, 0.5).Raw() = %d, want 128", r)
	}
	// Divide by zero saturates at MaxValue.
	if r := New(10).Div(Zero).Raw(); r != math.MaxInt32 {
		t.Errorf("Div(10, 0) should saturate, got %d", r)
	}
}

func TestDivInt(t *testing.T) {
	if r := New(10).DivInt(2).Raw(); r != 5*64 {
		t.Errorf("DivInt(10, 2).Raw() = %d, want 320", r)
	}
	if r := New(10).DivInt(0).Raw(); r != math.MaxInt32 {
		t.Errorf("DivInt(10, 0) should saturate, got %d", r)
	}
}

func TestMulDivWidens(t *testing.T) {
	// Percent-style: 50% of 200 px = 100 px.
	// MulDiv(200, 50, 100): 200*50/100 = 100. With raw values:
	// raw = (200*64) * (50*64) / (100*64) = 12800 * 3200 / 6400 = 6400 = 100*64. ✓
	a := New(200)
	m := New(50)
	d := New(100)
	if r := a.MulDiv(m, d).Raw(); r != 100*64 {
		t.Errorf("MulDiv(200, 50, 100).Raw() = %d, want 6400", r)
	}

	// The headline pitfall: Mul-then-Div would saturate, MulDiv must not.
	// Pick a where a*m overflows int32 even though (a*m)/d fits.
	// raw a = MaxInt32 / 2, raw m = 4, raw d = 2. Naive a*m = 2*MaxInt32 (overflows int32).
	// MulDiv int64-widens to (raw_a*raw_m)/raw_d = (MaxInt32/2 * 4)/2 = MaxInt32. Saturates at boundary; no garbage value.
	bigA := FromRaw(int64(math.MaxInt32 / 2))
	smallM := FromRaw(4)
	smallD := FromRaw(2)
	want := int64(math.MaxInt32/2) * 4 / 2
	if want > math.MaxInt32 {
		want = math.MaxInt32
	}
	if r := int64(bigA.MulDiv(smallM, smallD).Raw()); r != want {
		t.Errorf("MulDiv int64 widening: got %d, want %d", r, want)
	}

	// Divide by zero saturates.
	if r := a.MulDiv(m, Zero).Raw(); r != math.MaxInt32 {
		t.Errorf("MulDiv with d=0 should saturate, got %d", r)
	}
}

func TestIsIndefinite(t *testing.T) {
	if !IndefiniteSize.IsIndefinite() {
		t.Error("IndefiniteSize.IsIndefinite() should be true")
	}
	if Zero.IsIndefinite() {
		t.Error("Zero.IsIndefinite() should be false")
	}
	if New(-1).IsIndefinite() != true {
		t.Error("New(-1).IsIndefinite() should be true (raw -64 == kIndefinite)")
	}
}

func TestIsZero(t *testing.T) {
	if !Zero.IsZero() {
		t.Error("Zero.IsZero() should be true")
	}
	if New(1).IsZero() {
		t.Error("New(1).IsZero() should be false")
	}
}

func TestComparisons(t *testing.T) {
	a := New(3)
	b := New(5)
	if !a.Less(b) {
		t.Error("3 < 5 should be true")
	}
	if b.Less(a) {
		t.Error("5 < 3 should be false")
	}
	if !a.LessEqual(a) {
		t.Error("3 <= 3 should be true")
	}
	if Min(a, b) != a {
		t.Errorf("Min(3, 5) = %v, want 3", Min(a, b))
	}
	if Max(a, b) != b {
		t.Errorf("Max(3, 5) = %v, want 5", Max(a, b))
	}
	// Struct equality.
	if a != New(3) {
		t.Error("LayoutUnit struct equality should work via ==")
	}
}
