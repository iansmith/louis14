package html

import "testing"

func pluginNode(tag string, attrs map[string]string) *Node {
	n := &Node{Type: ElementNode, TagName: tag, Attributes: attrs}
	return n
}

// HTMLPlugInElement::GetObjectContentType / IsImageType
// (html_plugin_element.cc:643-677 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestGetObjectContentType(t *testing.T) {
	for _, tc := range []struct {
		tag   string
		attrs map[string]string
		want  ObjectContentType
	}{
		{"object", map[string]string{"data": "support/colors-16x8.png"}, ObjectContentTypeImage},
		{"embed", map[string]string{"src": " support/a.JPG?x=1 "}, ObjectContentTypeImage},
		{"object", map[string]string{"data": "page.html"}, ObjectContentTypeFrame},
		{"object", map[string]string{"data": "img.svg"}, ObjectContentTypeFrame}, // SVG is a non-image type
		{"object", map[string]string{"data": "noext"}, ObjectContentTypeFrame},   // no MIME type → frame
		{"object", map[string]string{"data": "doc.pdf"}, ObjectContentTypeNone},
		{"object", map[string]string{"type": "Image/PNG; foo=bar", "data": "data:image/png;base64,AAAA"}, ObjectContentTypeImage},
		{"embed", map[string]string{"type": "text/html", "src": "x.png"}, ObjectContentTypeFrame}, // type wins over extension
	} {
		n := pluginNode(tc.tag, tc.attrs)
		if got := n.GetObjectContentType(); got != tc.want {
			t.Errorf("<%s %v>: content type %v, want %v", tc.tag, tc.attrs, got, tc.want)
		}
		if got, want := n.IsImageType(), tc.want == ObjectContentTypeImage; got != want {
			t.Errorf("<%s %v>: IsImageType %v, want %v", tc.tag, tc.attrs, got, want)
		}
	}
}

// Element::ImageSourceURL (element.cc:4222) / HTMLObjectElement override
// (html_object_element.cc:282) and CreatesLayoutImage.
func TestImageSourceURLAndCreatesLayoutImage(t *testing.T) {
	obj := pluginNode("object", map[string]string{"data": "a.png", "src": "b.png"})
	emb := pluginNode("embed", map[string]string{"src": "c.png"})
	if obj.ImageSourceURL() != "a.png" || emb.ImageSourceURL() != "c.png" {
		t.Errorf("ImageSourceURL: object %q embed %q", obj.ImageSourceURL(), emb.ImageSourceURL())
	}
	if !CreatesLayoutImage(obj) || !CreatesLayoutImage(emb) {
		t.Error("image-typed object/embed should create a LayoutImage")
	}
	if CreatesLayoutImage(pluginNode("object", map[string]string{"data": "a.html"})) {
		t.Error("a frame-typed object is not a LayoutImage")
	}
	if !CreatesLayoutImage(pluginNode("img", map[string]string{"src": "a.png"})) {
		t.Error("an <img> showing its image is a LayoutImage")
	}
}
