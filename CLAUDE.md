Go binary: GOTOOLCHAIN=auto /opt/homebrew/Cellar/go/1.26.0/libexec/bin/go
(go.mod specifies toolchain go1.25.5; GOTOOLCHAIN=auto causes automatic switch)

## Testing Policy

**NEVER use fuzzy matching in reftests.** Fuzzy matching (FuzzyRadius > 0) conceals real
rendering bugs by allowing pixel shifts. All reftest comparisons must use FuzzyRadius=0
(the default). Only Tolerance (per-channel color) and MaxDifferentPercent are acceptable.

**Threshold: 0%.** MaxDifferentPercent must be 0. Any pixel difference is a test failure.
Tolerance=2 (per-channel) remains to handle anti-aliasing, but the overall diff must be 0%.

## CLI tools

- `cmd/l14open` — Renders a local HTML file to PNG and opens it: `l14open <input.html> <output.png> [width] [height]`
- `cmd/l14show` — Fetches a URL and renders to PNG: `l14show [-w 800] [-h 600] [-o output.png] <url>`

## Analyzing test failures with error percentages

To run a CSS3 category and show each test's pass/fail status with pixel-diff percentage:

```bash
/opt/homebrew/Cellar/go/1.26.0/libexec/bin/go test ./pkg/visualtest/... \
  -run TestWPTCSS3Reftests/css-CATEGORY -v -json -timeout 120s 2>&1 \
  > /tmp/results.txt

python3 - << 'EOF'
import json, re
tests = {}
for line in open('/tmp/results.txt'):
    line = line.strip()
    if not line: continue
    try: ev = json.loads(line)
    except: continue
    test = ev.get('Test', ''); action = ev.get('Action', ''); output = ev.get('Output', '')
    if not test or 'css-CATEGORY/' not in test: continue
    if test not in tests: tests[test] = {'output': [], 'action': None}
    if action == 'output': tests[test]['output'].append(output)
    elif action in ('pass', 'fail', 'skip'): tests[test]['action'] = action
results = []
for tname, info in tests.items():
    short = tname.replace('TestWPTCSS3Reftests/css-CATEGORY/', '')
    action = info['action'] or '?'
    max_pct = max((float(m.group(1)) for line in info['output']
                   for m in [re.search(r'pixels differ \(([0-9.]+)%,', line)] if m), default=0.0)
    results.append((short, action, max_pct))
results.sort(key=lambda r: (0 if r[1]=='fail' else 1 if r[1]=='pass' else 2, -r[2], r[0]))
print(f"{'Test':<65} {'Status':<6} {'Diff%'}")
print('-'*82)
for name, action, pct in results:
    print(f"{name:<65} {action.upper():<6} {pct:.1f}%" if action=='fail' else f"{name:<65} {action.upper()}")
total = len(results)
fails = sum(1 for r in results if r[1]=='fail')
passes = sum(1 for r in results if r[1]=='pass')
print(f"\nTotal: {total}  PASS: {passes}  FAIL: {fails}  Pass rate: {passes/total*100:.0f}%")
EOF
```

Replace `css-CATEGORY` with the actual category (e.g. `css-grid`, `css-flexbox`).

## Key packages

- `std/net` — HTTP/HTTPS fetch, URL resolution (no internal deps)
- `pkg/resource` — Fetcher/Renderer interfaces for network-aware rendering pipeline
- `pkg/images` — Image loading with optional network fetcher support
- `pkg/html` — HTML parsing with optional CSS fetcher for external stylesheets
- `pkg/layout` — CSS layout engine with optional image fetcher
- `pkg/render` — Rendering engine with optional image fetcher
