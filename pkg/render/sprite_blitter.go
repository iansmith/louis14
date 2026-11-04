package render

import "image"

// rasterPipelineSpriteBlit draws src over dst the way Skia's float pipeline draws
// a layer on restore: SkRasterPipelineSpriteBlitter (src/core/SkBlitter_Sprite.cpp:
// 122-191 @ d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e) loads premultiplied src,
// applies scale_1_float with the paint's float alpha, then srcover and store_8888
// (src/opts/SkRasterPipeline_opts.h:2891-2896, 2448, 3184-3191, with to_unorm's
// clamp and round at :2269-2272 @ d19e557a):
//
//	out = round(clamp(s*alpha + d*(1 - sa*alpha)))
//
// r is in dst coordinates; sp is the src point that lands on r.Min. A source pixel
// with RGB > A saturates, as in Skia, rather than wrapping.
func rasterPipelineSpriteBlit(dst *image.RGBA, r image.Rectangle, src *image.RGBA, sp image.Point, alpha float64) {
	clipped := r.Intersect(dst.Bounds())
	if clipped.Empty() {
		return
	}
	sp = sp.Add(clipped.Min.Sub(r.Min))
	rowBytes := clipped.Dx() * 4
	for y := 0; y < clipped.Dy(); y++ {
		d := dst.Pix[dst.PixOffset(clipped.Min.X, clipped.Min.Y+y):][:rowBytes]
		s := src.Pix[src.PixOffset(sp.X, sp.Y+y):][:rowBytes]
		for i := 0; i+3 < rowBytes; i += 4 {
			if s[i]|s[i+1]|s[i+2]|s[i+3] == 0 {
				continue
			}
			if alpha == 1 && s[i+3] == 255 {
				copy(d[i:i+4], s[i:i+4])
				continue
			}
			inv := 1 - float64(s[i+3])/255*alpha
			for c := 0; c < 4; c++ {
				d[i+c] = uint8(min(float64(s[i+c])*alpha+float64(d[i+c])*inv, 255) + 0.5)
			}
		}
	}
}
