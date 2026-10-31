package text

// RED stub (LOU-424): compile scaffolding for the HarfBuzzFace port; the
// implementation commit fills it in.

type harfBuzzFace struct{ platformData fontPlatformData }

func (p fontPlatformData) GetHarfBuzzFace() harfBuzzFace { return harfBuzzFace{platformData: p} }

func (h harfBuzzFace) UnitsPerEmFromHeadTable() int { return 0 }

func (h harfBuzzFace) VerticalData() openTypeVerticalData { return openTypeVerticalData{} }
