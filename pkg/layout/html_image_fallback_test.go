package layout

// LOU-433: an <img> whose image is not potentially available gets fallback
// content (HTMLImageElement::LayoutDisposition::kFallbackContent) instead of
// being laid out as a replaced element.
//
// Blink references, all @ Chromium d4ecdfed88f962439247c2ad36b8fe47805b1520:
//   - HTMLImageElement::ResetLayoutDisposition (html_image_element.cc:1066-1088)
//     picks kFallbackContent when ImageLoader::ImageIsPotentiallyAvailable
//     (image_loader.cc:284-310) is false.
//   - HTMLImageElement::CreateLayoutObject (html_image_element.cc:517-543)
//     creates a LayoutBlockFlow, not a LayoutImage, for kFallbackContent.
//   - HTMLImageFallbackHelper::AdjustHostStyle (html_image_fallback_helper.cc:
//     244-282) resets width/height/aspect-ratio to auto for a display:inline
//     host unless TreatImageAsReplaced (:50-62).
//   - HTMLImageFallbackHelper::CreateAltTextShadowTree (:216-242) builds
//     span#alttext-container > [img#alttext-image 16x16, span#alttext > alt].
//   - LayoutBlockFlow::AllowsColumns (layout_block_flow.cc:641-645) never lets
//     an <img> become a multicol container.
//
// Expected geometry is derived from those rules plus CSS 2.1 §10.3.9
// shrink-to-fit with Ahem metrics (every glyph is 1em x 1em), never from
// louis14 output.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// fallbackTestFont is the Ahem setup shared by the geometry tests: 10px
// glyphs on a 20px line, so "XXXXX" is 50px wide and every line is 20px tall.
const fallbackTestFont = "font-family:Ahem; font-size:10px; line-height:20px"

// erroringImageFetcher fails every fetch: a load that completed with an error.
func erroringImageFetcher(uri string) ([]byte, error) {
	return nil, errors.New("404: " + uri)
}

type fallbackLayout struct {
	doc    *html.Document
	root   *Box
	styles map[*html.Node]*css.Style
}

// layoutFallbackHTML parses src and lays it out at 800x600 with the given
// image fetcher (nil = no fetcher).
func layoutFallbackHTML(t *testing.T, src string, fetcher func(string) ([]byte, error)) fallbackLayout {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	engine := NewLayoutEngine(800, 600)
	if fetcher != nil {
		engine.SetImageFetcher(fetcher)
	}
	boxes := engine.Layout(doc)
	if len(boxes) != 1 {
		t.Fatalf("Layout returned %d root boxes, want 1", len(boxes))
	}
	return fallbackLayout{doc: doc, root: boxes[0], styles: engine.ComputedStyles()}
}

// findDOMElement returns the first element in document order matching pred.
func findDOMElement(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && pred(n) {
		return n
	}
	for _, c := range n.Children {
		if found := findDOMElement(c, pred); found != nil {
			return found
		}
	}
	return nil
}

func byTag(tag string) func(*html.Node) bool {
	return func(n *html.Node) bool { return n.TagName == tag }
}

func byID(id string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		v, ok := n.GetAttribute("id")
		return ok && v == id
	}
}

// findBox returns the first box in pre-order whose Node matches pred.
func findBox(b *Box, pred func(*html.Node) bool) *Box {
	if b == nil {
		return nil
	}
	if b.Node != nil && b.Node.Type == html.ElementNode && pred(b.Node) {
		return b
	}
	for _, c := range b.Children {
		if found := findBox(c, pred); found != nil {
			return found
		}
	}
	return nil
}

// collectTextBoxes returns every box under b that carries rendered text.
func collectTextBoxes(b *Box, out []*Box) []*Box {
	if b == nil {
		return out
	}
	if strings.TrimSpace(b.Text) != "" {
		out = append(out, b)
	}
	for _, c := range b.Children {
		out = collectTextBoxes(c, out)
	}
	return out
}

func (fl fallbackLayout) img(t *testing.T) (*html.Node, *Box) {
	t.Helper()
	node := findDOMElement(fl.doc.Root, byTag("img"))
	if node == nil {
		t.Fatal("no <img> in the document")
	}
	return node, findBox(fl.root, func(n *html.Node) bool { return n == node })
}

func assertBoxGeometry(t *testing.T, what string, b *Box, x, y, w, h float64) {
	t.Helper()
	if b == nil {
		t.Fatalf("%s: no box", what)
	}
	if b.X != x || b.Y != y || b.Width != w || b.Height != h {
		t.Errorf("%s: box = (%v,%v %vx%v), want (%v,%v %vx%v)", what, b.X, b.Y, b.Width, b.Height, x, y, w, h)
	}
}

// TestImageFallback_AltTextHostIsShrinkToFitBlockFlow pins D2: a broken img
// with non-empty alt in a standards-mode document is a non-replaced,
// inline-level block container (LayoutBlockFlow) whose width/height
// presentational hints are reset to auto (AdjustHostStyle :274-281), so it
// shrinks to fit its alt text.
func TestImageFallback_AltTextHostIsShrinkToFitBlockFlow(t *testing.T) {
	t.Run("five words in a 100px block", func(t *testing.T) {
		// shrink-to-fit = min(max(min-content 50, available 100), max-content
		// 16+290) = 100. Beside the 16px icon float 84px remain, and with the
		// float cleared 100px: "XXXXX XXXXX" (110px) never fits, so each word
		// takes its own 20px line: 5 x 20 = 100.
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">
<div style="width:100px; `+fallbackTestFont+`"><img alt="XXXXX XXXXX XXXXX XXXXX XXXXX" src="invalid.jpg" width="300" height="200"></div>`,
			erroringImageFetcher)
		node, box := fl.img(t)
		if IsReplacedElement(node) {
			t.Errorf("broken img with alt is replaced; want fallback content (LayoutBlockFlow)")
		}
		assertBoxGeometry(t, "fallback img", box, 0, 0, 100, 100)
		if s := fl.styles[node]; s != nil {
			for _, prop := range []string{"width", "height"} {
				if v, ok := s.Get(prop); ok && v != "auto" {
					t.Errorf("computed %s = %q; want auto (AdjustHostStyle resets the presentational hint)", prop, v)
				}
			}
		}
	})
	t.Run("single word shrinks below the available width", func(t *testing.T) {
		// max-content = 16 (icon float) + 50 = 66 < available 200.
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">
<div style="width:200px; `+fallbackTestFont+`"><img alt="XXXXX" src="invalid.jpg" width="300" height="200"></div>`,
			erroringImageFetcher)
		_, box := fl.img(t)
		assertBoxGeometry(t, "fallback img", box, 0, 0, 66, 20)
	})
	t.Run("an author inline-size is reset too", func(t *testing.T) {
		// AdjustHostStyle sees the final physical width, whatever its
		// source: inline-size maps to width in horizontal-tb, so it must be
		// resolved before the reset runs.
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">
<div dir="rtl" style="width:200px; `+fallbackTestFont+`"><img alt="XXXXX" src="invalid.jpg" style="inline-size:150px"></div>`,
			erroringImageFetcher)
		_, box := fl.img(t)
		if box == nil || box.Width != 66 || box.Height != 20 {
			t.Errorf("fallback img with inline-size = %v; want 66x20 (width reset to auto)", boxDims(box))
		}
	})
}

func boxDims(b *Box) string {
	if b == nil {
		return "<no box>"
	}
	return fmt.Sprintf("(%v,%v %vx%v)", b.X, b.Y, b.Width, b.Height)
}

// TestImageFallback_DoesNotEstablishMulticol pins D3: LayoutBlockFlow::
// AllowsColumns returns false for an HTMLImageElement, so `columns` on a
// fallback img is ignored at both multicol-establishment sites.
func TestImageFallback_DoesNotEstablishMulticol(t *testing.T) {
	t.Run("inline host min/max sizing", func(t *testing.T) {
		// As a multicol container its min-content would be 5*50 + 4*10 (gap
		// normal = 1em) = 290 > 100. Without multicol: max(50, 100) capped
		// by max-content 16+110 = 100.
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">
<div style="width:100px; `+fallbackTestFont+`"><img alt="XXXXX XXXXX" src="missing.png" style="columns:5"></div>`,
			erroringImageFetcher)
		_, box := fl.img(t)
		if box == nil {
			t.Fatal("no box for the fallback img")
		}
		if box.Width != 100 {
			t.Errorf("fallback img width = %v, want 100 (not the multicol min-content of 290)", box.Width)
		}
		if box.IsMulticolContainer {
			t.Errorf("fallback img is a multicol container; AllowsColumns must veto <img>")
		}
	})
	t.Run("block host layout dispatch", func(t *testing.T) {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">
<div style="width:100px; `+fallbackTestFont+`"><img alt="XXXXX XXXXX" src="missing.png" style="display:block; columns:5"></div>`,
			erroringImageFetcher)
		_, box := fl.img(t)
		if box == nil {
			t.Fatal("no box for the fallback img")
		}
		var cols int
		var walk func(*Box)
		walk = func(b *Box) {
			if b.IsColumnBox {
				cols++
			}
			for _, c := range b.Children {
				walk(c)
			}
		}
		walk(box)
		if cols != 0 || box.IsMulticolContainer {
			t.Errorf("display:block fallback img produced %d column boxes (multicol=%v); want none", cols, box.IsMulticolContainer)
		}
		if box.Height != 40 {
			t.Errorf("display:block fallback img height = %v, want 40 (two one-word lines)", box.Height)
		}
	})
}

// TestImageFallback_AltTextShadowTree pins D4: the fallback content mirrors
// CreateAltTextShadowTree — a 16x16 broken-image icon floated to the start
// side (ShowBrokenImageIcon :192-207) followed by the alt text, which
// therefore starts 16px in.
func TestImageFallback_AltTextShadowTree(t *testing.T) {
	t.Run("ltr", func(t *testing.T) {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">
<div style="width:200px; `+fallbackTestFont+`"><img alt="XXXXX" src="invalid.jpg"></div>`,
			erroringImageFetcher)
		host, hostBox := fl.img(t)
		icon := findBox(fl.root, byID("alttext-image"))
		assertBoxGeometry(t, "alttext-image icon", icon, 0, 0, 16, 16)
		if icon != nil {
			container := icon.Node.Parent
			if v, _ := container.GetAttribute("id"); v != "alttext-container" || container.Parent != host {
				t.Errorf("icon parent = %v (id %q); want span#alttext-container whose parent is the img host", container.TagName, v)
			}
			if s := fl.styles[icon.Node]; s == nil || s.GetFloat() != css.FloatLeft || s.GetDisplay() != css.DisplayFlowRoot {
				t.Errorf("icon style: want display flow-root + float left (ShowBrokenImageIcon, ltr)")
			}
		}
		texts := collectTextBoxes(hostBox, nil)
		if len(texts) == 0 || strings.TrimSpace(texts[0].Text) != "XXXXX" {
			t.Fatalf("alt text boxes = %v; want one line \"XXXXX\"", textsOf(texts))
		}
		if got := texts[0].X - hostBox.X; got != 16 {
			t.Errorf("alt text starts at inline offset %v, want 16 (beside the 16px icon float)", got)
		}
		alt := findDOMElement(icon.Node.Parent, byID("alttext"))
		if alt == nil || alt.Parent != icon.Node.Parent {
			t.Errorf("span#alttext missing from the alttext-container")
		}
	})
	t.Run("rtl floats the icon right", func(t *testing.T) {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">
<div dir="rtl" style="width:200px; `+fallbackTestFont+`"><img alt="XXXXX" src="invalid.jpg"></div>`,
			erroringImageFetcher)
		_, hostBox := fl.img(t)
		icon := findBox(fl.root, byID("alttext-image"))
		if hostBox == nil || icon == nil {
			t.Fatal("missing host or icon box")
		}
		if want := hostBox.X + hostBox.Width - 16; icon.X != want {
			t.Errorf("rtl icon x = %v, want %v (float right at the host's inline-start)", icon.X, want)
		}
	})
	t.Run("title is the alt text when alt is absent", func(t *testing.T) {
		// HTMLImageElement::AltText (html_image_element.cc:434-443).
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">
<div style="width:200px; `+fallbackTestFont+`"><img title="TT" src="invalid.jpg"></div>`,
			erroringImageFetcher)
		_, hostBox := fl.img(t)
		texts := collectTextBoxes(hostBox, nil)
		if len(texts) != 1 || strings.TrimSpace(texts[0].Text) != "TT" {
			t.Errorf("alt text boxes = %v; want the title \"TT\"", textsOf(texts))
		}
	})
}

func textsOf(boxes []*Box) []string {
	var out []string
	for _, b := range boxes {
		out = append(out, b.Text)
	}
	return out
}

// TestImageFallback_TreatImageAsReplaced pins D5: a broken img with no (or
// an empty) alt attribute and both dimensions keeps its size; its container
// is shown as replaced (ShowAsReplaced :103-116) with a 1px silver border and
// 1px padding (ShowBorder :118-144) unless smaller than the 18px alt image
// (ImageSmallerThanAltImage :34-48), in which case the icon is hidden too.
func TestImageFallback_TreatImageAsReplaced(t *testing.T) {
	const pre = `<!DOCTYPE html><body style="margin:0"><div style="width:400px; ` + fallbackTestFont + `">`
	t.Run("no alt, 100x50: bordered container", func(t *testing.T) {
		fl := layoutFallbackHTML(t, pre+`<img src="missing.png" width="100" height="50"></div>`, erroringImageFetcher)
		node, box := fl.img(t)
		if IsReplacedElement(node) {
			t.Errorf("broken img is replaced; want fallback content")
		}
		assertBoxGeometry(t, "host", box, 0, 0, 100, 50)
		container := findBox(fl.root, byID("alttext-container"))
		assertBoxGeometry(t, "alttext-container", container, 0, 0, 100, 50)
		if container != nil {
			if container.Border.Left != 1 || container.Border.Top != 1 || container.Padding.Left != 1 || container.Padding.Top != 1 {
				t.Errorf("container border/padding = %+v / %+v; want 1px / 1px (ShowBorder)", container.Border, container.Padding)
			}
			s := fl.styles[container.Node]
			if s == nil || s.GetDisplay() != css.DisplayFlowRoot {
				t.Errorf("container display: want flow-root (ShowAsReplaced)")
			} else {
				if s.GetOverflowX() != css.OverflowHidden {
					t.Errorf("container overflow-x = %v, want hidden (ShowAsReplaced)", s.GetOverflowX())
				}
				if c, _ := s.Get("border-top-color"); c != "silver" {
					t.Errorf("container border-top-color = %q, want silver", c)
				}
			}
		}
		icon := findBox(fl.root, byID("alttext-image"))
		assertBoxGeometry(t, "icon", icon, 2, 2, 16, 16)
	})
	t.Run("no alt, 10x10: no border, icon hidden", func(t *testing.T) {
		fl := layoutFallbackHTML(t, pre+`<img src="missing.png" width="10" height="10"></div>`, erroringImageFetcher)
		_, box := fl.img(t)
		// Only the size is pinned: the 10px host is shorter than the line's
		// strut, so its block offset depends on baseline alignment.
		if box == nil || box.Width != 10 || box.Height != 10 {
			t.Errorf("host = %v; want 10x10", boxDims(box))
		}
		container := findBox(fl.root, byID("alttext-container"))
		if container == nil {
			t.Fatal("no alttext-container box")
		}
		if container.Border.Left != 0 || container.Padding.Left != 0 {
			t.Errorf("container border/padding = %+v / %+v; want none below 18px", container.Border, container.Padding)
		}
		if icon := findBox(fl.root, byID("alttext-image")); icon != nil {
			t.Errorf("icon box present; want display:none (HideBrokenImageIcon)")
		}
	})
	t.Run("empty alt with dimensions is treated as replaced", func(t *testing.T) {
		fl := layoutFallbackHTML(t, pre+`<img src="missing.png" alt="" width="60" height="40"></div>`, erroringImageFetcher)
		_, box := fl.img(t)
		assertBoxGeometry(t, "host", box, 0, 0, 60, 40)
		if c := findBox(fl.root, byID("alttext-container")); c == nil || c.Border.Left != 1 {
			t.Errorf("alt=\"\" with dims: want the bordered replaced container")
		}
	})
	t.Run("src set, empty alt, no dims: represents nothing", func(t *testing.T) {
		fl := layoutFallbackHTML(t, pre+`<img src="missing.png" alt=""></div>`, erroringImageFetcher)
		_, box := fl.img(t)
		if box == nil || box.Width != 0 {
			t.Errorf("host = %v; want width 0 (empty inline content)", boxDims(box))
		}
		if icon := findBox(fl.root, byID("alttext-image")); icon != nil {
			t.Errorf("icon box present; want hidden (ImageRepresentsNothing)")
		}
	})
	t.Run("display:block host keeps its width hint", func(t *testing.T) {
		// AdjustHostStyle resets width/height only for display:inline.
		fl := layoutFallbackHTML(t, pre+`<img src="missing.png" alt="XXXXX" width="40" style="display:block"></div>`, erroringImageFetcher)
		node, box := fl.img(t)
		if IsReplacedElement(node) {
			t.Errorf("broken img with alt is replaced; want fallback content")
		}
		if box == nil || box.Width != 40 {
			t.Errorf("display:block fallback host = %v; want width 40", boxDims(box))
		}
		if icon := findBox(fl.root, byID("alttext-image")); icon == nil {
			t.Errorf("display:block fallback host has no alttext-image icon")
		}
	})
}

// onePNG returns a w x h PNG.
func onePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// TestImageFallback_PrimaryContentUnchanged pins D6 and the nil-fetcher
// policy: an image that loads stays a replaced LayoutImage sized from its
// natural dimensions; with no fetcher a non-empty src is treated as still
// loading (image_loader.cc:289-291) and stays primary; a missing src is never
// potentially available.
func TestImageFallback_PrimaryContentUnchanged(t *testing.T) {
	t.Run("loaded image", func(t *testing.T) {
		data := onePNG(t, 20, 10)
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div><img src="ok.png" alt="XXXXX"></div>`,
			func(uri string) ([]byte, error) {
				if uri == "ok.png" {
					return data, nil
				}
				return nil, errors.New("404")
			})
		node, box := fl.img(t)
		if !IsReplacedElement(node) {
			t.Errorf("loaded img is not replaced")
		}
		if box == nil || box.Width != 20 || box.Height != 10 {
			t.Errorf("loaded img box = %v; want 20x10", boxDims(box))
		}
		if findBox(fl.root, byID("alttext-container")) != nil {
			t.Errorf("loaded img grew fallback content")
		}
	})
	t.Run("no fetcher keeps primary content", func(t *testing.T) {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body><img src="x.png" alt="XXXXX"></body>`, nil)
		node, _ := fl.img(t)
		if !IsReplacedElement(node) {
			t.Errorf("img with src and no fetcher is not replaced; want primary content")
		}
	})
	t.Run("no src is fallback content", func(t *testing.T) {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body><img alt="XXXXX"></body>`, nil)
		node, _ := fl.img(t)
		if IsReplacedElement(node) {
			t.Errorf("img with no src is replaced; want fallback content")
		}
	})
}

// TestImageFallback_TextDecorationPropagatesIntoAltText: the fallback host
// keeps computed display inline in Blink, so StopPropagateTextDecorations
// (style_adjuster.cc:384-390) does not stop an ancestor's decoration and the
// alt text is underlined. A primary img is still a boundary.
func TestImageFallback_TextDecorationPropagatesIntoAltText(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body><span style="text-decoration:underline"><img alt="foo"></span></body>`, nil)
	alt := styledElementByID(fl.styles, "alttext")
	if alt == nil {
		t.Fatal("no span#alttext in the computed styles")
	}
	if got := len(fl.styles[alt].AppliedTextDecorations); got != 1 {
		t.Errorf("span#alttext carries %d applied decorations, want 1 (the ancestor underline)", got)
	}

	fl = layoutFallbackHTML(t, `<!DOCTYPE html><body><span style="text-decoration:underline"><img src="x.png"></span></body>`, nil)
	node, _ := fl.img(t)
	if got := len(fl.styles[node].AppliedTextDecorations); got != 0 {
		t.Errorf("primary img carries %d applied decorations, want 0 (replaced elements are propagation boundaries)", got)
	}
}

// styledElementByID finds a styled element by id, including synthesized
// fallback nodes that are not reachable from the DOM.
func styledElementByID(styles map[*html.Node]*css.Style, id string) *html.Node {
	match := byID(id)
	for n := range styles {
		if n.Type == html.ElementNode && match(n) {
			return n
		}
	}
	return nil
}

// TestImageFallback_NestedDocument: the disposition pass also runs for a
// nested (iframe) document, with the re-rooted image fetcher.
func TestImageFallback_NestedDocument(t *testing.T) {
	doc, err := html.Parse(`<!DOCTYPE html><body style="margin:0"><iframe src="inner.html" style="border:0; width:300px; height:100px"></iframe></body>`)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewLayoutEngine(800, 600)
	engine.SetImageFetcher(erroringImageFetcher)
	engine.SetDocumentFetcher(func(uri string) (string, error) {
		if strings.HasSuffix(uri, "inner.html") {
			return `<!DOCTYPE html><body style="margin:0"><div style="width:200px; ` + fallbackTestFont + `"><img alt="XXXXX" src="missing.png" width="300" height="200"></div></body>`, nil
		}
		return "", errors.New("404")
	})
	engine.Layout(doc)
	iframe := findDOMElement(doc.Root, byTag("iframe"))
	if iframe == nil || iframe.NestedDocument == nil {
		t.Fatal("iframe has no nested document after layout")
	}
	img := findDOMElement(iframe.NestedDocument.Root, byTag("img"))
	if img == nil {
		t.Fatal("no img in the nested document")
	}
	if IsReplacedElement(img) {
		t.Errorf("broken img with alt in a nested document is replaced; want fallback content")
	}
}
