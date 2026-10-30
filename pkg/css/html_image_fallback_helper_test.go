package css

import (
	"testing"

	"louis14/pkg/html"
)

// fallbackHostStyles cascades src, marks every <img> as fallback content
// (the disposition pass runs before the cascade in the engine), and runs the
// host style adjustment. It returns the first img and the style map.
func fallbackHostStyles(t *testing.T, src string) (*html.Node, map[*html.Node]*Style) {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	var first *html.Node
	var mark func(*html.Node)
	mark = func(n *html.Node) {
		if n.Type == html.ElementNode && n.TagName == "img" {
			if v, _ := n.GetAttribute("class"); v != "primary" {
				n.LayoutDisposition = html.LayoutDispositionFallbackContent
			}
			if first == nil {
				first = n
			}
		}
		for _, c := range n.Children {
			mark(c)
		}
	}
	mark(doc.Root)
	styles := ApplyStylesToDocument(doc, 800, 600)
	ResolveLogicalPropertiesInTree(doc.Root, styles)
	AdjustImageFallbackHostStylesInTree(doc.Root, styles)
	return first, styles
}

func imgByID(root *html.Node, id string) *html.Node {
	if root.Type == html.ElementNode {
		if v, _ := root.GetAttribute("id"); v == id {
			return root
		}
	}
	for _, c := range root.Children {
		if n := imgByID(c, id); n != nil {
			return n
		}
	}
	return nil
}

func rootOf(n *html.Node) *html.Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

// TestAdjustHostStyle pins HTMLImageFallbackHelper::AdjustHostStyle
// (html_image_fallback_helper.cc:244-282 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): it records the host's sizing
// and alt/src values in StyleUAShadowHostData BEFORE resetting width/height
// to auto for a non-TreatImageAsReplaced display:inline host; and the
// louis14 display remap that stands in for the LayoutBlockFlow class choice.
func TestAdjustHostStyle(t *testing.T) {
	img, styles := fallbackHostStyles(t, `<!DOCTYPE html><body>
<img id="a" alt="XXXXX" src="invalid.jpg" width="300" height="200">
<img id="b" alt="X" style="display:inline-flex">
<img id="c" alt="X" style="display:flex">
<img id="d" alt="X" style="display:list-item">
<img id="e" class="primary" alt="X" width="300">
<img id="f" alt="X" width="40" style="display:block">
<img id="g" width="30" height="20">
</body>`)
	s := styles[img]
	h := s.UAShadowHostData
	if h == nil {
		t.Fatal("fallback host has no UAShadowHostData")
	}
	want := StyleUAShadowHostData{Width: "300px", Height: "200px", AltText: "XXXXX", AltTextIsSet: true, AltAttribute: "XXXXX", SrcAttribute: "invalid.jpg"}
	if h.Width != want.Width || h.Height != want.Height || h.AltText != want.AltText ||
		h.AltTextIsSet != want.AltTextIsSet || h.AltAttribute != want.AltAttribute || h.SrcAttribute != want.SrcAttribute {
		t.Errorf("UAShadowHostData = %+v, want %+v (recorded before the reset)", *h, want)
	}
	for _, prop := range []string{"width", "height"} {
		if v, _ := s.Get(prop); v != "auto" {
			t.Errorf("host %s = %q, want auto", prop, v)
		}
	}
	if d := s.GetDisplay(); d != DisplayInlineBlock {
		t.Errorf("inline host display = %v, want inline-block", d)
	}

	root := rootOf(img)
	for _, c := range []struct {
		id   string
		want DisplayType
	}{
		{"b", DisplayInlineBlock},
		{"c", DisplayBlock},
		{"d", DisplayListItem},
		{"f", DisplayBlock},
	} {
		n := imgByID(root, c.id)
		if got := styles[n].GetDisplay(); got != c.want {
			t.Errorf("#%s display = %v, want %v", c.id, got, c.want)
		}
	}
	if v, _ := styles[imgByID(root, "f")].Get("width"); v != "40px" {
		t.Errorf("display:block host width = %q, want 40px (the reset is inline-only)", v)
	}
	// TreatImageAsReplaced (no alt, both dims): dimensions kept.
	if v, _ := styles[imgByID(root, "g")].Get("width"); v != "30px" {
		t.Errorf("no-alt host width = %q, want 30px (TreatImageAsReplaced keeps the hints)", v)
	}

	primary := styles[imgByID(root, "e")]
	if primary.UAShadowHostData != nil || primary.GetDisplay() != DisplayInline {
		t.Errorf("primary img adjusted: host data %v, display %v; want untouched", primary.UAShadowHostData, primary.GetDisplay())
	}
	if v, _ := primary.Get("width"); v != "300px" {
		t.Errorf("primary img width = %q, want 300px", v)
	}
}

// TestAltTextContainerAndImageStyle pins the HTMLAltTextContainerElement and
// HTMLAltTextImageElement AdjustStyle branches (html_image_fallback_helper.cc:
// 73-144 and :158-211 @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
func TestAltTextContainerAndImageStyle(t *testing.T) {
	inline := func() *Style { s := NewStyle(); s.Set("display", "inline"); return s }
	cases := []struct {
		name              string
		host              StyleUAShadowHostData
		wantReplaced      bool // container ShowAsReplaced
		wantBorder        bool // container ShowBorder
		wantIcon          bool // icon shown
		direction         string
		wantIconFloatSide FloatType
	}{
		{"alt text, no dims", StyleUAShadowHostData{Width: "auto", Height: "auto", AltText: "x", AltTextIsSet: true, AltAttribute: "x", SrcAttribute: "a.png"}, false, false, true, "ltr", FloatLeft},
		{"alt text, rtl", StyleUAShadowHostData{Width: "auto", Height: "auto", AltText: "x", AltTextIsSet: true, AltAttribute: "x", SrcAttribute: "a.png"}, false, false, true, "rtl", FloatRight},
		{"no alt, 100x50", StyleUAShadowHostData{Width: "100px", Height: "50px", SrcAttribute: "a.png"}, true, true, true, "ltr", FloatLeft},
		{"no alt, 10x10", StyleUAShadowHostData{Width: "10px", Height: "10px", SrcAttribute: "a.png"}, true, false, false, "ltr", FloatNone},
		{"no alt, aspect-ratio + width", StyleUAShadowHostData{Width: "100px", Height: "auto", AspectRatio: "2/1", SrcAttribute: "a.png"}, true, true, true, "ltr", FloatLeft},
		{"src, empty alt, no dims", StyleUAShadowHostData{Width: "auto", Height: "auto", AltTextIsSet: true, SrcAttribute: "a.png"}, false, false, false, "ltr", FloatNone},
		{"no src, no alt", StyleUAShadowHostData{Width: "auto", Height: "auto"}, false, false, false, "ltr", FloatNone},
		{"title only", StyleUAShadowHostData{Width: "auto", Height: "auto", AltText: "t", AltTextIsSet: true}, false, false, true, "ltr", FloatLeft},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := c.host
			container := inline()
			container.Set("direction", c.direction)
			AdjustAltTextContainerStyle(container, &h)
			if got := container.GetDisplay() == DisplayFlowRoot; got != c.wantReplaced {
				t.Errorf("container flow-root = %v, want %v", got, c.wantReplaced)
			}
			if c.wantReplaced {
				if w, _ := container.Get("width"); w != h.Width {
					t.Errorf("container width = %q, want host width %q", w, h.Width)
				}
				if container.GetOverflowX() != OverflowHidden || container.GetOverflowY() != OverflowHidden {
					t.Errorf("container overflow not hidden")
				}
			}
			if got := container.GetBorderWidth().Top == 1; got != c.wantBorder {
				t.Errorf("container 1px border = %v, want %v", got, c.wantBorder)
			}
			if c.wantBorder {
				if col, _ := container.Get("border-left-color"); col != "silver" {
					t.Errorf("border color = %q, want silver", col)
				}
				if bs, _ := container.Get("box-sizing"); bs != "border-box" {
					t.Errorf("box-sizing = %q, want border-box", bs)
				}
			}

			icon := inline()
			icon.Set("direction", c.direction)
			AdjustAltTextImageStyle(icon, &h)
			shown := icon.GetDisplay() != DisplayNone
			if shown != c.wantIcon {
				t.Errorf("icon shown = %v, want %v", shown, c.wantIcon)
			}
			if shown {
				if icon.GetDisplay() != DisplayFlowRoot || icon.GetFloat() != c.wantIconFloatSide {
					t.Errorf("icon = display %v float %v, want flow-root float %v", icon.GetDisplay(), icon.GetFloat(), c.wantIconFloatSide)
				}
			}
		})
	}
}
