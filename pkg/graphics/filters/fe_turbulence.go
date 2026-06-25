package filters

import (
	"image"
	"math"
	"strconv"
)

// TurbulenceKind selects the fractal-sum mode, mirroring Blink's
// TurbulenceType enum (fe_turbulence.h). Values map 1-1 to Blink's
// FETURBULENCE_TYPE_FRACTALNOISE / FETURBULENCE_TYPE_TURBULENCE.
type TurbulenceKind int

const (
	// TurbulenceFractalNoise sums signed noise and maps to [0,1] via
	// (sum + 1) / 2.
	TurbulenceFractalNoise TurbulenceKind = iota
	// TurbulenceTurbulence sums the absolute value of noise, yielding [0,1).
	TurbulenceTurbulence
)

// FETurbulence is a generator primitive producing Perlin noise per SVG Filter
// Effects 1 §feTurbulence. It mirrors platform/graphics/filters/fe_turbulence.cc,
// whose per-pixel math is in turn a faithful port of the spec's reference
// pseudocode (the "PerlinNoise" / "Turbulence" functions). The lattice
// gradient tables are initialised from the seed exactly as the spec's
// `init()` describes (the same LCG, the same Knuth shuffle, the same
// wrap-around copy into the [256, 2*256+2) range).
//
// Output is a premultiplied RGBA image covering the filter region. Each of the
// four channels (R, G, B, A) is an independent noise sum; the result is
// non-premultiplied [0,1] then premultiplied for storage.
//
// Invalid-attribute handling (crbug.com/1068863): a negative baseFrequency is
// invalid and the SVG element falls back to the default (0). The builder maps
// that to BaseFreqX/Y == 0 here. At frequency 0 the lattice is sampled at a
// single point and the noise sum is identically 0, so fractalNoise yields a
// constant 0.5 and turbulence yields 0 — which is the spec-correct constant
// image an invalid feTurbulence produces.
type FETurbulence struct {
	baseEffect
	Kind        TurbulenceKind
	BaseFreqX   float64
	BaseFreqY   float64
	NumOctaves  int
	Seed        float64
	StitchTiles bool
}

// NewFETurbulence constructs a turbulence generator. A negative baseFrequency
// is clamped to 0 here as a defensive mirror of the element-level invalid
// handling (the builder already substitutes the default for a negative value);
// numOctaves below 0 is clamped to 0 per the spec.
func NewFETurbulence(space InterpolationSpace, kind TurbulenceKind,
	baseFreqX, baseFreqY float64, numOctaves int, seed float64, stitchTiles bool) *FETurbulence {
	if baseFreqX < 0 {
		baseFreqX = 0
	}
	if baseFreqY < 0 {
		baseFreqY = 0
	}
	if numOctaves < 0 {
		numOctaves = 0
	}
	return &FETurbulence{
		baseEffect:  baseEffect{space: space},
		Kind:        kind,
		BaseFreqX:   baseFreqX,
		BaseFreqY:   baseFreqY,
		NumOctaves:  numOctaves,
		Seed:        seed,
		StitchTiles: stitchTiles,
	}
}

// Perlin-noise lattice constants, per SVG Filter Effects 1 §feTurbulence
// reference code and Blink's fe_turbulence.cc.
const (
	turbBSize     = 0x100
	turbBM        = 0xff
	turbPerlinN   = 0x1000
	turbBSizeMask = turbBSize + turbBSize + 2 // 514 entries in the wrapped tables
)

// turbPaintingData holds the seeded lattice tables. Mirrors Blink's
// FETurbulence::PaintingData (the lattice_selector_ + gradient_ arrays).
type turbPaintingData struct {
	latticeSelector [turbBSizeMask]int
	gradient        [4][turbBSizeMask][2]float64
}

// random is the spec's LCG (Park-Miller minimal standard), used to seed the
// lattice. Mirrors FETurbulence::PaintingData::Random / the spec's setup_seed.
func turbSetupSeed(seed int) int {
	if seed <= 0 {
		seed = -(seed % (turbRandM - 1)) + 1
	}
	if seed > turbRandM-1 {
		seed = turbRandM - 1
	}
	return seed
}

const (
	turbRandM = 2147483647 // 2**31 - 1
	turbRandA = 16807      // 7**5; primitive root of m
	turbRandQ = 127773     // m / a
	turbRandR = 2836       // m % a
)

func turbRandom(seed int) int {
	result := turbRandA*(seed%turbRandQ) - turbRandR*(seed/turbRandQ)
	if result <= 0 {
		result += turbRandM
	}
	return result
}

// initPaintingData seeds the lattice selector and gradient tables exactly as
// the SVG spec's init() pseudocode (and Blink's FETurbulence::InitPaint).
func (e *FETurbulence) initPaintingData() *turbPaintingData {
	pd := &turbPaintingData{}
	seed := turbSetupSeed(int(e.Seed))

	for k := 0; k < 4; k++ {
		for i := 0; i < turbBSize; i++ {
			if k == 0 {
				pd.latticeSelector[i] = i
			}
			var grad [2]float64
			for j := 0; j < 2; j++ {
				seed = turbRandom(seed)
				grad[j] = float64(seed%(turbBSize+turbBSize)-turbBSize) / float64(turbBSize)
			}
			// Normalise the gradient to unit length.
			s := math.Sqrt(grad[0]*grad[0] + grad[1]*grad[1])
			if s != 0 {
				grad[0] /= s
				grad[1] /= s
			}
			pd.gradient[k][i] = grad
		}
	}
	// Knuth shuffle the lattice selector.
	for i := turbBSize - 1; i > 0; i-- {
		seed = turbRandom(seed)
		j := seed % turbBSize
		pd.latticeSelector[i], pd.latticeSelector[j] = pd.latticeSelector[j], pd.latticeSelector[i]
	}
	// Wrap-around copy into the upper half of the tables (indices
	// [BSize, 2*BSize+1]) so lattice lookups never need a modulo.
	for i := 0; i < turbBSize+2; i++ {
		pd.latticeSelector[turbBSize+i] = pd.latticeSelector[i]
		for k := 0; k < 4; k++ {
			pd.gradient[k][turbBSize+i] = pd.gradient[k][i]
		}
	}
	return pd
}

// sCurve / lerp per the spec.
func turbSCurve(t float64) float64     { return t * t * (3.0 - 2.0*t) }
func turbLerp(t, a, b float64) float64 { return a + t*(b-a) }

// noise2 computes the 2D gradient noise for one colour channel at point
// (vx, vy). Mirrors FETurbulence::Noise2 / the spec's noise2().
func (pd *turbPaintingData) noise2(channel int, vx, vy float64) float64 {
	const bm = turbBM
	t := vx + turbPerlinN
	bx0 := int(t) & bm
	bx1 := (bx0 + 1) & bm
	rx0 := t - math.Floor(t)
	rx1 := rx0 - 1.0

	t = vy + turbPerlinN
	by0 := int(t) & bm
	by1 := (by0 + 1) & bm
	ry0 := t - math.Floor(t)
	ry1 := ry0 - 1.0

	i := pd.latticeSelector[bx0]
	j := pd.latticeSelector[bx1]
	b00 := pd.latticeSelector[i+by0]
	b10 := pd.latticeSelector[j+by0]
	b01 := pd.latticeSelector[i+by1]
	b11 := pd.latticeSelector[j+by1]

	sx := turbSCurve(rx0)
	sy := turbSCurve(ry0)

	q := pd.gradient[channel][b00]
	u := rx0*q[0] + ry0*q[1]
	q = pd.gradient[channel][b10]
	v := rx1*q[0] + ry0*q[1]
	a := turbLerp(sx, u, v)

	q = pd.gradient[channel][b01]
	u = rx0*q[0] + ry1*q[1]
	q = pd.gradient[channel][b11]
	v = rx1*q[0] + ry1*q[1]
	b := turbLerp(sx, u, v)

	return turbLerp(sy, a, b)
}

// turbulenceSum computes one channel's fractal sum at point (x, y). Mirrors
// FETurbulence::CalculateTurbulenceValueForPoint (the octave loop).
func (e *FETurbulence) turbulenceSum(pd *turbPaintingData, channel int, x, y float64) float64 {
	sum := 0.0
	vx := x * e.BaseFreqX
	vy := y * e.BaseFreqY
	ratio := 1.0
	for octave := 0; octave < e.NumOctaves; octave++ {
		n := pd.noise2(channel, vx, vy)
		if e.Kind == TurbulenceFractalNoise {
			sum += n / ratio
		} else {
			sum += math.Abs(n) / ratio
		}
		vx *= 2
		vy *= 2
		ratio *= 2
	}
	if e.Kind == TurbulenceFractalNoise {
		sum = (sum + 1.0) / 2.0
	}
	return sum
}

// ApplyEffect renders the noise field over the filter region.
func (e *FETurbulence) ApplyEffect(_ []*image.RGBA, region image.Rectangle) *image.RGBA {
	out := newRGBA(region)
	w, h := region.Dx(), region.Dy()
	if w <= 0 || h <= 0 {
		return out
	}
	pd := e.initPaintingData()

	clamp01 := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}

	for py := 0; py < h; py++ {
		// Sample at the pixel centre in the filter's coordinate system. The
		// turbulence function is defined in the filter region's user space;
		// region.Min is the absolute device origin, so the in-region sample
		// point is (region.Min + pixel + 0.5).
		fy := float64(region.Min.Y) + float64(py) + 0.5
		row := py * out.Stride
		for px := 0; px < w; px++ {
			fx := float64(region.Min.X) + float64(px) + 0.5
			r := clamp01(e.turbulenceSum(pd, 0, fx, fy))
			g := clamp01(e.turbulenceSum(pd, 1, fx, fy))
			b := clamp01(e.turbulenceSum(pd, 2, fx, fy))
			a := clamp01(e.turbulenceSum(pd, 3, fx, fy))
			res := premultiply(r, g, b, a)
			i := row + px*4
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = res[0], res[1], res[2], res[3]
		}
	}
	return out
}

// newFETurbulenceFromPrimitive builds an FETurbulence from an SVG
// `<feTurbulence>` element view. Negative baseFrequency values are invalid per
// crbug.com/1068863 and fall back to the default (0); negative numOctaves and
// negative seed follow the spec's clamping in NewFETurbulence / setupSeed.
func newFETurbulenceFromPrimitive(space InterpolationSpace, prim SVGFilterPrimitive) *FETurbulence {
	kind := TurbulenceFractalNoise
	if parseStringAttr(prim, "type", "turbulence") == "turbulence" {
		kind = TurbulenceTurbulence
	}
	// baseFrequency: one or two numbers (fx [fy]). A negative value is
	// invalid → treat the whole attribute as unspecified (default 0, 0).
	fx, fy := 0.0, 0.0
	if s, ok := prim.Attribute("baseFrequency"); ok {
		parts := splitSVGList(s)
		valid := true
		vals := make([]float64, 0, 2)
		for _, p := range parts {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil || v < 0 {
				valid = false
				break
			}
			vals = append(vals, v)
		}
		if valid && len(vals) >= 1 {
			fx = vals[0]
			fy = vals[0]
			if len(vals) >= 2 {
				fy = vals[1]
			}
		}
	}
	numOctaves, _ := parseIntAttr(prim, "numOctaves", 1)
	seed, _ := parseFloatAttr(prim, "seed", 0)
	stitch := parseStringAttr(prim, "stitchTiles", "noStitch") == "stitch"
	return NewFETurbulence(space, kind, fx, fy, numOctaves, seed, stitch)
}
