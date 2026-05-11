module louis14

go 1.26.2

require (
	github.com/dop251/goja v0.0.0-20260106131823-651366fbe6e3
	github.com/fogleman/gg v1.3.0
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
	golang.org/x/text v0.22.0
	mazarin/textshape v0.0.0-00010101000000-000000000000
)

replace github.com/fogleman/gg v1.3.0 => ./third_party/gg

replace mazarin/textshape => ../mazzy/mazarin/textshape

require (
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-text/typesetting v0.3.4 // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	golang.org/x/image v0.24.0 // indirect
	golang.org/x/net v0.35.0 // indirect
)
