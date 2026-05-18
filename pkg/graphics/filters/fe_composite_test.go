package filters

import (
	"image"
	"testing"
)

// solidInput builds a region-sized buffer filled with one premultiplied colour.
// Mirrors solidImage from fe_color_matrix_test.go but is local to keep the two
// test files independent if either is moved.
func solidInput(region image.Rectangle, r, g, b, a uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i] = r
		img.Pix[i+1] = g
		img.Pix[i+2] = b
		img.Pix[i+3] = a
	}
	return img
}

// pixelInputEffect is a tiny FilterEffect that just returns the buffer it was
// constructed with — convenient for driving FEComposite/FETile directly from
// pre-baked inputs in unit tests.
type pixelInputEffect struct {
	baseEffect
	img *image.RGBA
}

func newPixelInputEffect(img *image.RGBA, space InterpolationSpace) *pixelInputEffect {
	return &pixelInputEffect{baseEffect: baseEffect{space: space}, img: img}
}

func (p *pixelInputEffect) ApplyEffect(_ []*image.RGBA, region image.Rectangle) *image.RGBA {
	return p.img
}

// TestFEComposite_Over_AllOpaque verifies the Porter-Duff "over" operator on
// two fully opaque inputs: red over blue yields red.
func TestFEComposite_Over_AllOpaque(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	in1 := newPixelInputEffect(solidInput(region, 255, 0, 0, 255), InterpolationSpaceSRGB)
	in2 := newPixelInputEffect(solidInput(region, 0, 0, 255, 255), InterpolationSpaceSRGB)
	fe := NewFEComposite(in1, in2, InterpolationSpaceSRGB, CompositeOver, 0, 0, 0, 0)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	if out == nil {
		t.Fatal("Apply returned nil")
	}
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 255 || out.Pix[i+1] != 0 || out.Pix[i+2] != 0 || out.Pix[i+3] != 255 {
			t.Fatalf("over: pixel %d expected red, got %v", i/4, out.Pix[i:i+4])
		}
	}
}

// TestFEComposite_In_AlphaMask verifies "in": fully opaque red `in` 50%-alpha
// transparent mask should give premultiplied (128, 0, 0, 128) (±1 rounding).
func TestFEComposite_In_AlphaMask(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	// in1: opaque red.
	in1 := newPixelInputEffect(solidInput(region, 255, 0, 0, 255), InterpolationSpaceSRGB)
	// in2: 50% alpha transparent (premultiplied {0,0,0,128}).
	in2 := newPixelInputEffect(solidInput(region, 0, 0, 0, 128), InterpolationSpaceSRGB)
	fe := NewFEComposite(in1, in2, InterpolationSpaceSRGB, CompositeIn, 0, 0, 0, 0)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		r := int(out.Pix[i])
		g := int(out.Pix[i+1])
		b := int(out.Pix[i+2])
		a := int(out.Pix[i+3])
		if r < 127 || r > 129 || g != 0 || b != 0 || a < 127 || a > 129 {
			t.Fatalf("in: pixel %d expected ~(128,0,0,128), got %v", i/4, out.Pix[i:i+4])
		}
	}
}

// TestFEComposite_Out verifies "out": red out of opaque mask is fully
// transparent everywhere (1 - Da = 0).
func TestFEComposite_Out(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	in1 := newPixelInputEffect(solidInput(region, 255, 0, 0, 255), InterpolationSpaceSRGB)
	in2 := newPixelInputEffect(solidInput(region, 0, 0, 0, 255), InterpolationSpaceSRGB)
	fe := NewFEComposite(in1, in2, InterpolationSpaceSRGB, CompositeOut, 0, 0, 0, 0)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 0 || out.Pix[i+1] != 0 || out.Pix[i+2] != 0 || out.Pix[i+3] != 0 {
			t.Fatalf("out: pixel %d expected fully transparent, got %v", i/4, out.Pix[i:i+4])
		}
	}
}

// TestFEComposite_Atop verifies "atop": red atop blue with both opaque should
// produce red (since within the destination's extent the source replaces it).
// Formula: out = i1*Da + i2*(1-Sa). With Sa=Da=1: out = i1*1 + i2*0 = i1 = red.
func TestFEComposite_Atop(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	in1 := newPixelInputEffect(solidInput(region, 255, 0, 0, 255), InterpolationSpaceSRGB)
	in2 := newPixelInputEffect(solidInput(region, 0, 0, 255, 255), InterpolationSpaceSRGB)
	fe := NewFEComposite(in1, in2, InterpolationSpaceSRGB, CompositeAtop, 0, 0, 0, 0)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 255 || out.Pix[i+1] != 0 || out.Pix[i+2] != 0 || out.Pix[i+3] != 255 {
			t.Fatalf("atop: pixel %d expected red, got %v", i/4, out.Pix[i:i+4])
		}
	}
}

// TestFEComposite_Xor verifies "xor": where both inputs are opaque, the output
// is fully transparent. Formula: out = i1*(1-Da) + i2*(1-Sa). With Sa=Da=1
// the result is zero on all channels.
func TestFEComposite_Xor(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	in1 := newPixelInputEffect(solidInput(region, 255, 0, 0, 255), InterpolationSpaceSRGB)
	in2 := newPixelInputEffect(solidInput(region, 0, 0, 255, 255), InterpolationSpaceSRGB)
	fe := NewFEComposite(in1, in2, InterpolationSpaceSRGB, CompositeXor, 0, 0, 0, 0)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 0 || out.Pix[i+1] != 0 || out.Pix[i+2] != 0 || out.Pix[i+3] != 0 {
			t.Fatalf("xor: pixel %d expected transparent, got %v", i/4, out.Pix[i:i+4])
		}
	}
}

// TestFEComposite_Arithmetic_K2Only verifies the arithmetic operator with
// k1=0,k2=1,k3=0,k4=0 is an identity over in1 (output = in1 unchanged). This
// pins down the un-premultiply / re-premultiply boundaries: if either is off
// the identity will visibly fail.
func TestFEComposite_Arithmetic_K2Only(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	// Use a partly-transparent in1 so the un-premultiply boundary is exercised.
	in1 := newPixelInputEffect(solidInput(region, 50, 100, 150, 200), InterpolationSpaceSRGB)
	in2 := newPixelInputEffect(solidInput(region, 0, 0, 0, 255), InterpolationSpaceSRGB)
	fe := NewFEComposite(in1, in2, InterpolationSpaceSRGB, CompositeArithmetic, 0, 1, 0, 0)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		for c := 0; c < 4; c++ {
			d := int(out.Pix[i+c]) - int(in1.img.Pix[i+c])
			if d < -1 || d > 1 {
				t.Fatalf("arith identity: pixel %d channel %d expected ~%d got %d",
					i/4, c, in1.img.Pix[i+c], out.Pix[i+c])
			}
		}
	}
}

// TestFEComposite_Arithmetic_GeneralCase verifies the full formula with hand-
// computed values. Per SVG Filter Effects 1 §feComposite the arithmetic mode
// operates on non-premultiplied channels (Blink Skia's ArithmeticPaintFilter
// converts both inputs out of premultiplied form before applying).
//
//	in1 unpremul: (0.5, 0.0, 0.0, 1.0)   -- red, half-bright
//	in2 unpremul: (0.0, 0.0, 1.0, 1.0)   -- blue, full
//	k1=0.5, k2=0.5, k3=0.25, k4=0.1
//	R = 0.5*0.5*0.0 + 0.5*0.5 + 0.25*0.0 + 0.1 = 0.35
//	G = 0 + 0 + 0 + 0.1 = 0.1
//	B = 0 + 0 + 0.25*1 + 0.1 = 0.35
//	A = 0.5*1*1 + 0.5*1 + 0.25*1 + 0.1 = 1.35 → clamp to 1
//	premul output = (0.35, 0.1, 0.35, 1) → (89, 26, 89, 255) ±1.
func TestFEComposite_Arithmetic_GeneralCase(t *testing.T) {
	region := image.Rect(0, 0, 2, 2)
	// premultiplied red half-bright opaque = (128, 0, 0, 255).
	in1 := newPixelInputEffect(solidInput(region, 128, 0, 0, 255), InterpolationSpaceSRGB)
	in2 := newPixelInputEffect(solidInput(region, 0, 0, 255, 255), InterpolationSpaceSRGB)
	fe := NewFEComposite(in1, in2, InterpolationSpaceSRGB, CompositeArithmetic, 0.5, 0.5, 0.25, 0.1)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		r := int(out.Pix[i])
		g := int(out.Pix[i+1])
		b := int(out.Pix[i+2])
		a := int(out.Pix[i+3])
		// Tolerance of ±1 absorbs rounding from the un/re-premultiply path.
		if r < 88 || r > 90 || g < 25 || g > 27 || b < 88 || b > 90 || a != 255 {
			t.Fatalf("arith general: pixel %d expected ~(89,26,89,255) got %v",
				i/4, out.Pix[i:i+4])
		}
	}
}
