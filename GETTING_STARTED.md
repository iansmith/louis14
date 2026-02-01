# Getting Started with Louis14

## Your Project is Ready!

All files are now in `~/louis14/`:

```
~/louis14/
├── ARCHITECTURE.md          # Complete technical design
├── README.md                # Quick overview
├── RUN_TESTS.md            # How to run tests
├── TESTING_PLAN.md         # Comprehensive testing strategy
├── GETTING_STARTED.md      # This file
├── go.mod                   # Go dependencies
├── cmd/
│   └── louis14/
│       ├── main.go              # CLI application
│       └── integration_test.go  # Integration tests
├── pkg/
│   ├── html/
│   │   ├── tokenizer.go         # HTML tokenizer
│   │   ├── tokenizer_test.go    # Tokenizer tests
│   │   ├── parser.go            # HTML parser
│   │   ├── parser_test.go       # Parser tests
│   │   └── dom.go               # DOM structures
│   ├── css/
│   │   ├── style.go             # CSS parser
│   │   └── style_test.go        # CSS tests
│   ├── layout/
│   │   ├── layout.go            # Layout engine
│   │   └── layout_test.go       # Layout tests
│   └── render/
│       └── render.go            # Renderer (uses fogleman/gg)
├── testdata/
│   └── phase1/
│       └── simple.html          # Test HTML file
└── output/                      # Generated PNGs go here

```

## Quick Start

### 1. Install Dependencies
```bash
cd ~/louis14
go mod download
```

### 2. Run the Tests (60+ tests, all should pass!)
```bash
go test ./... -v
```

Expected output:
- ✓ pkg/html: 28 tests PASS
- ✓ pkg/css: 23 tests PASS  
- ✓ pkg/layout: 11 tests PASS
- ✓ cmd/louis14: 10+ tests PASS

### 3. Render Your First Image
```bash
go run cmd/louis14/main.go testdata/phase1/simple.html output/simple.png
```

### 4. View the Result
```bash
# macOS
open output/simple.png

# Linux
xdg-open output/simple.png
```

You should see 4 colored boxes (red, blue, green, yellow) stacked vertically!

## What Phase 1 Can Do

✅ Parse HTML tags and attributes
✅ Parse inline CSS styles
✅ Render colored boxes
✅ Stack elements vertically (block layout)
✅ Custom widths and heights
✅ 17 named colors

## Try Creating Your Own Test

Create `testdata/phase1/custom.html`:
```html
<div style="background-color: purple; width: 400px; height: 150px;"></div>
<div style="background-color: orange; width: 100px; height: 100px;"></div>
<div style="background-color: pink; width: 350px; height: 75px;"></div>
```

Render it:
```bash
go run cmd/louis14/main.go testdata/phase1/custom.html output/custom.png
```

## Next Steps

Once you've tested Phase 1 and everything works:
1. Review the test results
2. Play with creating different HTML files
3. When ready, schedule Session 2 for Phase 2:
   - Nested elements
   - Full box model (margin, border, padding)
   - More CSS properties

## Documentation

- **ARCHITECTURE.md** - Detailed technical design and roadmap
- **RUN_TESTS.md** - Testing instructions
- **TESTING_PLAN.md** - Complete testing strategy

## Need Help?

If tests fail:
1. Check Go version: `go version` (need 1.21+)
2. Verify dependencies: `go mod download`
3. Check font path in `pkg/render/render.go` (line with LoadFontFace)

---

**Phase 1 Status**: ✅ Complete and tested!
**Session 1 Messages Used**: ~17
**Next Session**: Phase 2 (~20-25 messages)
