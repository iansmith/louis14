package layout

import (
	"math"
	"testing"
)

func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The three IMCB branches of ComputeUnclampedIMCBInOneAxis.

func TestIMCB_BothInsetsAuto_StartBias(t *testing.T) {
	// CB = 200, static at 50, bias = start.
	// IMCB = [50, 0] (inline-start pinned to the static offset).
	s, e, b, _, _, _, _ := ComputeUnclampedIMCBInOneAxis(
		200,
		0, false,
		0, false,
		true, /* parallel */
		50, BiasStart,
		BiasStart,
		BiasStart,
		BiasStart, false,
		BiasStart, false,
	)
	if !approxEq(s, 50) || !approxEq(e, 0) || b != BiasStart {
		t.Fatalf("both-auto/start: got (%v, %v, %v), want (50, 0, BiasStart)", s, e, b)
	}
}

func TestIMCB_BothInsetsAuto_EndBias(t *testing.T) {
	// CB = 200, static at 160, bias = end. IMCB = [0, 200-160=40].
	s, e, b, _, _, _, _ := ComputeUnclampedIMCBInOneAxis(
		200,
		0, false,
		0, false,
		true,
		160, BiasEnd,
		BiasStart, BiasStart,
		BiasStart, false,
		BiasStart, false,
	)
	if !approxEq(s, 0) || !approxEq(e, 40) || b != BiasEnd {
		t.Fatalf("both-auto/end: got (%v, %v, %v), want (0, 40, BiasEnd)", s, e, b)
	}
}

func TestIMCB_BothInsetsAuto_CenterClipping(t *testing.T) {
	// CB = 200, static at 80 (center). The center-clipping rule says:
	// half = min(80, 120) = 80. IMCB = [80-80=0, 200-80-80=40], so the box
	// can't escape the CB via center bias.
	s, e, b, _, _, _, _ := ComputeUnclampedIMCBInOneAxis(
		200,
		0, false,
		0, false,
		true,
		80, BiasEqual,
		BiasStart, BiasStart,
		BiasStart, false,
		BiasStart, false,
	)
	if !approxEq(s, 0) || !approxEq(e, 40) || b != BiasEqual {
		t.Fatalf("both-auto/center-clip: got (%v, %v, %v), want (0, 40, BiasEqual)", s, e, b)
	}

	// Symmetric test: static closer to the end edge.
	s, e, _, _, _, _, _ = ComputeUnclampedIMCBInOneAxis(
		200,
		0, false,
		0, false,
		true,
		150, BiasEqual,
		BiasStart, BiasStart,
		BiasStart, false,
		BiasStart, false,
	)
	// half = min(150, 50) = 50. IMCB = [150-50=100, 200-150-50=0].
	if !approxEq(s, 100) || !approxEq(e, 0) {
		t.Fatalf("both-auto/center-clip (end-side): got (%v, %v), want (100, 0)", s, e)
	}

	// Perfectly centered static: half = min(100, 100) = 100.
	// IMCB = [0, 0]. The full CB becomes available space.
	s, e, _, _, _, _, _ = ComputeUnclampedIMCBInOneAxis(
		200,
		0, false,
		0, false,
		true,
		100, BiasEqual,
		BiasStart, BiasStart,
		BiasStart, false,
		BiasStart, false,
	)
	if !approxEq(s, 0) || !approxEq(e, 0) {
		t.Fatalf("both-auto/center-clip (centered): got (%v, %v), want (0, 0)", s, e)
	}
}

func TestIMCB_OneAutoInset(t *testing.T) {
	// inset-start = 30, inset-end = auto. Bias points to the specified side
	// (kStart here). IMCB = [30, 0].
	s, e, b, _, _, _, _ := ComputeUnclampedIMCBInOneAxis(
		200,
		30, true,
		0, false,
		true,
		0, BiasStart,
		BiasCenter(), BiasStart,
		BiasStart, false,
		BiasStart, false,
	)
	if !approxEq(s, 30) || !approxEq(e, 0) || b != BiasStart {
		t.Fatalf("one-auto/start: got (%v, %v, %v), want (30, 0, BiasStart)", s, e, b)
	}

	// inset-start = auto, inset-end = 40. Bias = BiasEnd. IMCB = [0, 40].
	s, e, b, _, _, _, _ = ComputeUnclampedIMCBInOneAxis(
		200,
		0, false,
		40, true,
		true,
		0, BiasStart,
		BiasStart, BiasStart,
		BiasStart, false,
		BiasStart, false,
	)
	if !approxEq(s, 0) || !approxEq(e, 40) || b != BiasEnd {
		t.Fatalf("one-auto/end: got (%v, %v, %v), want (0, 40, BiasEnd)", s, e, b)
	}
}

func TestIMCB_BothSpecified(t *testing.T) {
	// inset-start = 20, inset-end = 30. IMCB = [20, 30]. Bias = alignment bias.
	s, e, b, _, _, d, hasD := ComputeUnclampedIMCBInOneAxis(
		200,
		20, true,
		30, true,
		true,
		0, BiasStart,
		BiasEqual, // alignment
		BiasStart, // default
		BiasStart, false,
		BiasStart, false,
	)
	if !approxEq(s, 20) || !approxEq(e, 30) || b != BiasEqual {
		t.Fatalf("both-spec: got (%v, %v, %v), want (20, 30, BiasEqual)", s, e, b)
	}
	if !hasD || d != BiasStart {
		t.Fatalf("both-spec: default bias lost (has=%v, d=%v)", hasD, d)
	}
}

// BiasCenter is a small helper for readability in tests — BiasEqual is the
// enum value meaning "center/split evenly".
func BiasCenter() InsetBias { return BiasEqual }

func TestResizeIMCB_EachBias(t *testing.T) {
	// Start pinned: amount grows the end.
	s, e := 10.0, 20.0
	ResizeIMCBInOneAxis(BiasStart, 50, &s, &e)
	if !approxEq(s, 10) || !approxEq(e, 70) {
		t.Fatalf("resize/start: got (%v, %v), want (10, 70)", s, e)
	}
	// End pinned: amount grows the start.
	s, e = 10.0, 20.0
	ResizeIMCBInOneAxis(BiasEnd, 50, &s, &e)
	if !approxEq(s, 60) || !approxEq(e, 20) {
		t.Fatalf("resize/end: got (%v, %v), want (60, 20)", s, e)
	}
	// Equal: half each.
	s, e = 10.0, 20.0
	ResizeIMCBInOneAxis(BiasEqual, 50, &s, &e)
	if !approxEq(s, 35) || !approxEq(e, 45) {
		t.Fatalf("resize/equal: got (%v, %v), want (35, 45)", s, e)
	}
	// Negative amount: works in reverse.
	s, e = 50.0, 50.0
	ResizeIMCBInOneAxis(BiasEqual, -20, &s, &e)
	if !approxEq(s, 40) || !approxEq(e, 40) {
		t.Fatalf("resize/neg: got (%v, %v), want (40, 40)", s, e)
	}
}

func TestComputeMargins_BothAuto_Positive(t *testing.T) {
	// Classic centering: IMCB=200, box=100, both margins auto → 50/50.
	mS, mE, applied := ComputeMargins(200, 0, 0, true, true, 100, false, true)
	if !approxEq(mS, 50) || !approxEq(mE, 50) || !applied {
		t.Fatalf("auto-auto/positive: got (%v, %v, applied=%v), want (50, 50, true)", mS, mE, applied)
	}
}

func TestComputeMargins_BothAuto_Negative_StartDominant(t *testing.T) {
	// Overflow: IMCB=100, box=150. freeSpace=-50. Start dominant → mS=0, mE=-50.
	mS, mE, applied := ComputeMargins(100, 0, 0, true, true, 150, false, true)
	if !approxEq(mS, 0) || !approxEq(mE, -50) || !applied {
		t.Fatalf("auto-auto/neg/start-dom: got (%v, %v, applied=%v), want (0, -50, true)", mS, mE, applied)
	}
}

func TestComputeMargins_OneAuto(t *testing.T) {
	// IMCB=200, box=100, mS=auto, mE=20. freeSpace=100, mS = 100-20 = 80.
	mS, mE, applied := ComputeMargins(200, 0, 20, true, false, 100, false, true)
	if !approxEq(mS, 80) || !approxEq(mE, 20) || !applied {
		t.Fatalf("mS=auto: got (%v, %v, applied=%v), want (80, 20, true)", mS, mE, applied)
	}
}

func TestComputeMargins_HasAutoInset_ForcesZero(t *testing.T) {
	// If either inset is auto, auto margins resolve to 0.
	mS, mE, applied := ComputeMargins(200, 0, 0, true, true, 100, true, true)
	if !approxEq(mS, 0) || !approxEq(mE, 0) || applied {
		t.Fatalf("auto-inset/auto-margin: got (%v, %v, applied=%v), want (0, 0, false)", mS, mE, applied)
	}
}

func TestGetAlignmentInsetBias_Core(t *testing.T) {
	wdm := WritingDirectionMode{WM: WritingModeHorizontalTB, Dir: DirectionLTR}
	cases := []struct {
		name        string
		pos         ItemPosition
		wantBias    InsetBias
		wantOverrfl bool
	}{
		{"normal", ItemPositionNormal, BiasStart, false},
		{"start", ItemPositionStart, BiasStart, false},
		{"stretch", ItemPositionStretch, BiasStart, false},
		{"center", ItemPositionCenter, BiasEqual, true},
		{"end", ItemPositionEnd, BiasEnd, true},
		{"flex-start", ItemPositionFlexStart, BiasStart, false},
		{"flex-end", ItemPositionFlexEnd, BiasEnd, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bias, overflow, _, _, _, _ := GetAlignmentInsetBias(
				AlignmentData{Position: c.pos},
				wdm, wdm, false,
			)
			if bias != c.wantBias {
				t.Fatalf("%s: got bias=%v, want %v", c.name, bias, c.wantBias)
			}
			if overflow != c.wantOverrfl {
				t.Fatalf("%s: got overflow=%v, want %v", c.name, overflow, c.wantOverrfl)
			}
		})
	}
}

func TestGetAlignmentInsetBias_Safe(t *testing.T) {
	wdm := WritingDirectionMode{WM: WritingModeHorizontalTB, Dir: DirectionLTR}
	_, _, _, _, safe, hasSafe := GetAlignmentInsetBias(
		AlignmentData{Position: ItemPositionCenter, IsSafe: true},
		wdm, wdm, false,
	)
	if !hasSafe || safe != BiasStart {
		t.Fatalf("safe bias: got (%v, has=%v), want (BiasStart, true)", safe, hasSafe)
	}
}

func TestComputeInsets_CenterAlignment(t *testing.T) {
	// IMCB=[10, 10] in a 200 CB (so imcb size = 180). Box = 80, margins = 0.
	// free = 200 - 10 - 10 - 80 = 100. Equal bias splits: 50/50.
	// Final insets: 10+50=60, 10+50=60.
	iS, iE := ComputeInsets(
		200, 10, 10,
		BiasEqual, false, BiasStart, false, BiasStart, false,
		0, 0, 80,
		false, // no auto margins
	)
	if !approxEq(iS, 60) || !approxEq(iE, 60) {
		t.Fatalf("center insets: got (%v, %v), want (60, 60)", iS, iE)
	}
}

func TestComputeInsets_StartAlignment_PassThroughWhenAutoMargins(t *testing.T) {
	// If ComputeMargins already applied auto margins, ComputeInsets returns
	// the IMCB offsets unchanged.
	iS, iE := ComputeInsets(
		200, 10, 10,
		BiasStart, false, BiasStart, false, BiasStart, false,
		50, 50, 80,
		true, // auto margins absorbed leftover
	)
	if !approxEq(iS, 10) || !approxEq(iE, 10) {
		t.Fatalf("auto-margin passthrough: got (%v, %v), want (10, 10)", iS, iE)
	}
}

func TestComputeInsets_EndAlignmentOverflowFallsBackToDefault(t *testing.T) {
	// Alignment = end, but box overflows (free < 0) and hasDefaultAlignmentOverflow
	// is set → fall back to the default bias (BiasStart).
	// CB=100, imcb=[10,10], box=120, margins=0 → free = 100-10-10-120 = -40.
	// Without overflow fallback, BiasEnd would shift -40 into the start side: iS=-30.
	// With overflow fallback to BiasStart, -40 goes into the end side: iE=-30.
	iS, iE := ComputeInsets(
		100, 10, 10,
		BiasEnd, true,
		BiasStart, true,
		BiasStart, false,
		0, 0, 120,
		false,
	)
	if !approxEq(iS, 10) || !approxEq(iE, -30) {
		t.Fatalf("end-overflow-fallback: got (%v, %v), want (10, -30)", iS, iE)
	}
}
