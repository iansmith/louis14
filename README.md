# Louis14 Rendering Engine

A web rendering engine built in Go, targeting ACID2 compliance through incremental development.

## Current Status

**Phase 1** ✅ (Complete): Basic HTML + Simple Rendering
**Phase 2** ✅ (Complete): Nested Elements + Box Model

## Quick Start

```bash
# Install dependencies
go mod download

# Run the renderer
go run cmd/louis14/main.go testdata/phase1/simple.html output/simple.png
go run cmd/louis14/main.go testdata/phase2/nested.html output/nested.png

# Run tests
go test ./... -v
```

## Features Implemented

### Phase 1
- HTML tokenizer and parser
- Inline CSS styles
- Basic layout (width, height, background-color)
- PNG rendering

### Phase 2 (New!)
- **Nested HTML elements** with proper tree structure
- **Full CSS box model**: margin, padding, border
- **Recursive layout** and rendering
- **Box model calculations** with auto-sizing

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) - System design and phases
- [PHASE2_SUMMARY.md](PHASE2_SUMMARY.md) - Phase 2 implementation details
- [TESTING_PLAN.md](TESTING_PLAN.md) - Test strategy
- [VISUAL_TESTING.md](VISUAL_TESTING.md) - Visual regression testing guide
- [RUN_TESTS.md](RUN_TESTS.md) - How to run tests
