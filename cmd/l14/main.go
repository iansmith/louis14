package main

import (
	"fmt"
	"image"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"louis14/pkg/js"
	"louis14/pkg/resource"
	stdnet "louis14/std/net"
)

func main() {
	a := app.New()
	w := a.NewWindow("louis14 browser")
	w.Resize(fyne.NewSize(1024, 768))

	// Blank initial render target
	target := image.NewRGBA(image.Rect(0, 0, 1024, 700))
	canvasImg := canvas.NewImageFromImage(target)
	canvasImg.FillMode = canvas.ImageFillOriginal

	// Status label
	status := widget.NewLabel("Enter a URL and press Enter")

	// URL bar
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("https://example.com")
	urlEntry.OnSubmitted = func(url string) {
		status.SetText("Loading " + url + "...")
		go func() {
			// Fetch
			body, _, err := stdnet.Fetch(url)
			if err != nil {
				status.SetText("Error: " + err.Error())
				return
			}

			// Render
			renderTarget := image.NewRGBA(image.Rect(0, 0, 1024, 700))
			fetcher := resource.NewFetcher(url)
			renderer := resource.NewLouis14Renderer(fetcher)
			renderer.SetJSEngine(js.New())
			if err := renderer.Render(string(body), renderTarget); err != nil {
				status.SetText("Render error: " + err.Error())
				return
			}

			// Update display
			canvasImg.Image = renderTarget
			canvasImg.Refresh()
			status.SetText(url)
			w.SetTitle(fmt.Sprintf("louis14 — %s", url))
		}()
	}

	// Layout: URL bar on top, status at bottom, image fills center
	topBar := container.NewBorder(nil, nil, nil, nil, urlEntry)
	content := container.NewBorder(topBar, status, nil, nil, canvasImg)
	w.SetContent(content)

	// Keep focus on URL entry to prevent Tab freeze with no other focusable widgets
	w.Canvas().Focus(urlEntry)

	w.ShowAndRun()
}
