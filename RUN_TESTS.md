# Running Tests for Louis14

## Quick Test

To run all tests:
```bash
cd ~/louis14
go test ./... -v
```

## Run Tests by Package

### HTML Package Tests
```bash
go test ./pkg/html/... -v
```

Expected output:
- TestTokenizer_* (16 tests)
- TestParser_* (12 tests)
All should PASS ✓

### CSS Package Tests
```bash
go test ./pkg/css/... -v
```

Expected output:
- TestParseInlineStyle_* (9 tests)
- TestGetLength_* (5 tests)
- TestParseColor_* (6 tests)
- TestStyle_* (3 tests)
All should PASS ✓

### Layout Package Tests
```bash
go test ./pkg/layout/... -v
```

Expected output:
- TestLayoutEngine_* (11 tests)
All should PASS ✓

### Integration Tests
```bash
go test ./cmd/louis14/... -v
```

Expected output:
- TestIntegration_* (10+ tests)
All should PASS ✓

## Test Coverage

To see code coverage:
```bash
go test ./... -cover
```

To generate detailed coverage report:
```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## What the Tests Validate

### Tokenizer Tests
✓ Parse simple tags: `<div>`, `</div>`
✓ Parse attributes: `style="..."`, `id="..."`
✓ Handle quotes: single and double
✓ Parse text content
✓ Handle complete sequences
✓ Case insensitivity
✓ Whitespace handling

### Parser Tests
✓ Build DOM from tokens
✓ Handle multiple elements
✓ Preserve attributes
✓ Handle text nodes
✓ Empty documents
✓ Mixed content

### CSS Tests
✓ Parse inline styles
✓ Extract lengths (px values)
✓ Parse colors (named colors)
✓ Handle whitespace
✓ Case insensitive properties
✓ Get/Set operations

### Layout Tests
✓ Position boxes correctly
✓ Apply dimensions from styles
✓ Vertical stacking (block layout)
✓ Default width/height
✓ Style parsing integration
✓ Node references

### Integration Tests
✓ Full HTML → Parse → Layout → Render pipeline
✓ PNG file generation
✓ All named colors
✓ Edge cases (empty, many boxes, various sizes)
✓ Error handling

## Expected Test Results

All tests should PASS. If any fail, check:
1. Go version (need 1.21+)
2. Dependencies installed (`go mod download`)
3. File paths are correct

## Test Count

- **Total tests**: ~60+
- **Expected failures**: 0
- **Time to run**: < 1 second

## Next Steps After Tests Pass

1. ✓ All tests pass → Phase 1 is solid!
2. Run manual visual test:
   ```bash
   go run cmd/louis14/main.go testdata/phase1/simple.html output/simple.png
   ```
3. Verify the PNG looks correct (4 colored boxes)
4. Ready for Phase 2!
