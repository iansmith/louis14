# Visual Regression Testing for Louis14

Visual regression testing ensures that rendering changes don't accidentally break existing functionality by comparing rendered output against known-good reference images.

## Overview

The visual testing framework:
- **Renders HTML** to PNG images
- **Compares pixel-by-pixel** with reference images
- **Allows small tolerances** for rendering differences (anti-aliasing, etc.)
- **Generates diff images** showing what changed
- **Easy updates** when changes are intentional

## Running Visual Tests

### Run all visual tests:
```bash
go test -v ./cmd/louis14 -run TestVisual
```

### Run a specific visual test:
```bash
go test -v ./cmd/louis14 -run TestVisualRegression_Phase1_Simple
```

### Run with verbose output:
```bash
go test -v ./cmd/louis14 -run TestVisual
```

## Test Results

### When tests PASS:
```
✓ Visual test passed: simple (max diff: 0)
```

### When tests FAIL:
```
Visual regression test failed: simple
  Different pixels: 1234 / 480000 (0.26%)
  Max difference: 45 (tolerance: 2)
  Actual output: /tmp/test123/actual.png
  Reference: testdata/phase1/reference/simple.png
  Diff image: /tmp/test123/diff.png

To update reference image if this change is intentional:
  UPDATE_REFS=1 go test -v ./cmd/louis14 -run TestVisualRegression_Phase1_Simple
```

## Updating Reference Images

Reference images need to be updated when you **intentionally** change rendering behavior.

### Update ALL reference images:
```bash
UPDATE_REFS=1 go test -v ./cmd/louis14 -run TestVisual
```

### Update a SPECIFIC reference image:
```bash
UPDATE_REFS=1 go test -v ./cmd/louis14 -run TestVisualRegression_Phase1_Simple
```

### Using the helper tool:
```bash
go run cmd/update-references/main.go phase1
```

## Adding New Visual Tests

### 1. Create a new test in `cmd/louis14/visual_regression_test.go`:

```go
func TestVisualRegression_Phase2_MyNewTest(t *testing.T) {
	testCase := visualTestCase{
		name: "my_new_test",
		htmlContent: `<div style="margin: 10px; padding: 20px; background-color: blue;"></div>`,
		referenceFile: "testdata/phase2/reference/my_new_test.png",
		width: 800,
		height: 600,
	}

	runVisualTest(t, testCase)
}
```

### 2. Generate the reference image:

```bash
UPDATE_REFS=1 go test -v ./cmd/louis14 -run TestVisualRegression_Phase2_MyNewTest
```

### 3. Verify the reference image looks correct:

```bash
open testdata/phase2/reference/my_new_test.png
```

### 4. Run the test normally to ensure it passes:

```bash
go test -v ./cmd/louis14 -run TestVisualRegression_Phase2_MyNewTest
```

## Understanding Tolerance

The comparison allows a small **tolerance** (default: 2) for pixel differences. This accounts for:
- Anti-aliasing differences across systems
- Font rendering variations
- Minor floating-point rounding

### Tolerance values:
- **0**: Exact pixel match (very strict, may fail on different systems)
- **2**: Default - allows tiny rendering differences
- **5**: More permissive - for tests with text/fonts
- **10+**: Very permissive - may miss real bugs

### Adjusting tolerance for a specific test:

Edit `visual_regression_test.go`:

```go
opts := visualtest.DefaultOptions()
opts.Tolerance = 5  // More permissive
result, err := visualtest.CompareImages(actualPath, tc.referenceFile, opts)
```

## Directory Structure

```
testdata/
├── phase1/
│   ├── simple.html              # Test HTML files
│   └── reference/
│       ├── simple.png           # Reference images
│       ├── single_box.png
│       ├── color_red.png
│       └── ...
├── phase2/
│   ├── nested.html
│   └── reference/
│       └── ...
```

## Current Test Coverage

### Phase 1 Visual Tests:
- ✓ `simple` - Four colored boxes from simple.html
- ✓ `single_box` - Single red box
- ✓ `all_colors` - Each named color (12 tests)
- ✓ `vertical_stacking` - Multiple boxes stacking vertically
- ✓ `different_sizes` - Various width/height combinations
- ✓ `empty` - Empty document (white background)

Total: **18 visual regression tests**

## Troubleshooting

### "Reference image does not exist"

Generate it:
```bash
UPDATE_REFS=1 go test -v ./cmd/louis14 -run YourTestName
```

### Tests fail on different machines

- Font rendering may differ - consider increasing tolerance
- Ensure same font files are available
- Consider platform-specific reference images if needed

### All pixels are different

- Check if image dimensions match
- Verify you're using the correct reference image
- View the diff image to see what changed

### Visual test passes but output looks wrong

- Open and inspect the reference image manually
- Reference images may be outdated
- Update references if current behavior is correct

## Best Practices

1. **Always visually inspect** reference images before committing them
2. **Don't blindly update** references when tests fail - investigate why first
3. **Keep tests focused** - one concept per test
4. **Use descriptive names** - clear what the test validates
5. **Test edge cases** - empty, very large, very small, etc.
6. **Commit reference images** to version control

## Integration with CI/CD

Visual tests run in normal CI:

```bash
# In CI pipeline
go test ./... -v
```

Reference images should be committed to git so CI can validate them.

To prevent accidental reference updates in CI, never set `UPDATE_REFS=1` in CI scripts.
