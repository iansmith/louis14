Go binary: /opt/homebrew/Cellar/go/1.25.5/libexec/bin/go

## CLI tools

- `cmd/l14open` — Renders a local HTML file to PNG and opens it: `l14open <input.html> <output.png> [width] [height]`
- `cmd/l14show` — Fetches a URL and renders to PNG: `l14show [-w 800] [-h 600] [-o output.png] <url>`

## Key packages

- `std/net` — HTTP/HTTPS fetch, URL resolution (no internal deps)
- `pkg/resource` — Fetcher/Renderer interfaces for network-aware rendering pipeline
- `pkg/images` — Image loading with optional network fetcher support
- `pkg/html` — HTML parsing with optional CSS fetcher for external stylesheets
- `pkg/layout` — CSS layout engine with optional image fetcher
- `pkg/render` — Rendering engine with optional image fetcher
