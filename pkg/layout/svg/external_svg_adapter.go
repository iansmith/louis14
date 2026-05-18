package svg

import (
	"encoding/xml"
	"io"
	"strings"
)

// externalSVGElement is an ElementAdapter implementation backed by a
// parsed XML element. Used for SVG documents fetched as an external
// resource (e.g. `filter: url(file.svg#id)`), where there's no
// existing DOM tree to wrap. Keeps this package free of any concrete
// HTML parser dependency.
//
// Mirrors the pkg/layout svgElementAdapter shape (TagName / Attribute /
// SVGChildren), but reads from a pre-parsed tree built by
// ParseExternalSVG.
type externalSVGElement struct {
	tag      string
	attrs    map[string]string
	children []*externalSVGElement
}

// TagName implements ElementAdapter. Returns the lowercased tag name —
// matches the HTML tokenizer's canonical form that the rest of
// pkg/layout/svg consumes.
func (e *externalSVGElement) TagName() string { return e.tag }

// Attribute implements ElementAdapter. Attribute names are looked up
// case-insensitively (callers may pass camelCase like "filterUnits" or
// lowercased "filterunits").
func (e *externalSVGElement) Attribute(name string) (string, bool) {
	if v, ok := e.attrs[name]; ok {
		return v, true
	}
	v, ok := e.attrs[strings.ToLower(name)]
	return v, ok
}

// SVGChildren implements ElementAdapter.
func (e *externalSVGElement) SVGChildren() []ElementAdapter {
	out := make([]ElementAdapter, 0, len(e.children))
	for _, c := range e.children {
		out = append(out, c)
	}
	return out
}

// ParseExternalSVG parses an SVG document's bytes into a tree of
// ElementAdapters. The returned element is the root `<svg>` (or
// whichever top-level element appears first). Unknown content
// (comments, processing instructions, text nodes) is dropped — filter
// resolution only needs the element tree.
//
// Returns an error if the input is empty or has no element content.
// Tolerant of XML namespaces (the local name is used).
func ParseExternalSVG(data []byte) (ElementAdapter, error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	// Filter elements may declare default namespaces; we accept any
	// namespace and use only the local name.
	dec.Strict = false
	dec.AutoClose = nil
	dec.Entity = xml.HTMLEntity

	var root *externalSVGElement
	stack := []*externalSVGElement{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			elt := &externalSVGElement{
				tag:   strings.ToLower(t.Name.Local),
				attrs: make(map[string]string, len(t.Attr)),
			}
			for _, a := range t.Attr {
				// Keep both original case and lowercased — Attribute()
				// looks up either form. Strip namespace prefix.
				name := a.Name.Local
				elt.attrs[name] = a.Value
				if low := strings.ToLower(name); low != name {
					elt.attrs[low] = a.Value
				}
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, elt)
			} else if root == nil {
				root = elt
			}
			stack = append(stack, elt)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return root, nil
}

// FindElementByID walks the tree rooted at elt (depth-first, pre-order)
// and returns the first element whose `id` attribute equals id. Returns
// (nil, false) if no match. Used by external-filter resolution to
// locate the `<filter id="...">` target.
func FindElementByID(elt ElementAdapter, id string) (ElementAdapter, bool) {
	if elt == nil {
		return nil, false
	}
	if v, ok := elt.Attribute("id"); ok && v == id {
		return elt, true
	}
	for _, c := range elt.SVGChildren() {
		if got, ok := FindElementByID(c, id); ok {
			return got, true
		}
	}
	return nil, false
}
