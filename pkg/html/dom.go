package html

import (
	"sort"
	"strings"
	"unicode/utf16"
)

type Node struct {
	Type       NodeType
	TagName    string
	Attributes map[string]string
	Text       string
	RawText    string // Original text with whitespace preserved (for pre-wrap contexts)
	Children   []*Node
	Parent     *Node // Phase 2: Support proper tree structure

	// NestedDocument is set on <iframe> and <object> nodes after layout to
	// retain the parsed sub-document. This allows JS to access iframe.contentDocument.
	NestedDocument *Document

	// IsFocused is true when this element is the document's currently focused
	// element. Mirrors Blink's per-Element focus bit, maintained by the focus
	// pathway (Document::SetFocusedElement). Consulted by the CSS matcher for
	// :focus and :focus-within. In louis14 it is set by the JS
	// Element.focus() binding in pkg/js/dom.go and cleared on the previously
	// focused element.
	IsFocused bool

	// HasFocusWithin is true when this element OR any of its descendants has
	// IsFocused set. Mirrors Blink's per-Element HasFocusWithin() bit
	// (Element::HasFocusWithin), updated by SetFocusedElement walking up from
	// the new focused element and walking up from the old one to clear. Lets
	// :focus-within evaluate O(1) during selector matching.
	HasFocusWithin bool
}

type NodeType int

const (
	ElementNode NodeType = iota
	TextNode
)

// StylesheetSource is one CSS source attached to a Document: either an
// inline `<style>` block (Href is empty) or an external `<link rel=
// stylesheet>` (Href is the link's href attribute, used to derive the
// per-sheet BaseDir at CSS parse time). Mirrors Blink's
// `CSSStyleSheet::ParserOwnerURL` model where each sheet carries the
// URL it was fetched from so url() refs inside resolve against the
// sheet's own base, not the owning document's
// (core/css/css_style_sheet.h @ chromium-main d4ecdfed8).
//
// Per-sheet BaseDir is NOT computed at HTML-parse time because
// doc.BaseDir is set after parsing for iframe documents (see
// layoutNestedDocument). The pkg/css cascade computes it via
// `url.URLDir(url.CompleteURL(Href, doc.BaseDir))` when each
// StyleSheetContents is built.
type StylesheetSource struct {
	Text string
	Href string // empty for <style> blocks; the link's href for external sheets
}

type Document struct {
	Root          *Node
	Stylesheets   []StylesheetSource // CSS from <style> tags + <link rel=stylesheet>
	Scripts       []string           // JavaScript from <script> tags
	ViewportWidth int                // From <meta name="viewport" content="width=...">

	// BaseDir is the document's base directory for resolving relative
	// `url()` references inside CSS values, mirroring Blink's
	// `Document::BaseURL()` (the value captured into a `CSSParserContext`
	// at parse time, see `core/css/parser/css_parser_context.cc:76-78` @
	// chromium-main bf955d02bf0b0c67868b2e62359c0af199af9acc). Empty for
	// the top-level document (URLs resolve against the renderer's
	// basePath). For nested documents (iframe/object content) the layout
	// engine sets it to the path of the nested doc relative to the outer
	// document's BaseDir, so a moved-out node's URLs naturally re-resolve
	// against the outer base on the next cascade pass — louis14's
	// re-cascade-per-layout-pass pipeline obviates the explicit
	// `Element::DidMoveToNewDocument` hook Blink needs.
	BaseDir string

	// ParsedStylesheetsCache holds the per-document parse cache populated
	// lazily by css.ParseDocumentStylesheets. Each entry wraps a
	// StylesheetSource together with the ParserContext used to parse it,
	// so the 2-4 callers within a single layout pass (cascade, layout
	// tree builder for pseudo-elements, @font-face registration,
	// @counter-style collection) share one parse instead of repeating the
	// work.
	//
	// Typed as any to keep pkg/html from importing pkg/css. The concrete
	// type is []*css.StyleSheetContents.
	ParsedStylesheetsCache any

	// FocusedElement is the element currently holding document focus, or nil
	// if no element is focused. Mirrors Blink's Document::FocusedElement().
	// Maintained by SetFocusedElement (defined as a method on *Document
	// below), which also walks the ancestor chain to keep IsFocused /
	// HasFocusWithin bits coherent.
	FocusedElement *Node

	// CSSResourceFetcher is the per-document fetcher used by pkg/css to
	// resolve `@import` targets at CSS-parse time. Mirrors Blink's
	// `Document::Fetcher()`, which `StyleRuleImport::NotifyFinished`
	// consults to load imported sheets
	// (third_party/blink/renderer/core/css/style_rule_import.cc:77-82 @
	// chromium-main d4ecdfed88f962439247c2ad36b8fe47805b1520). Set by
	// ParseWithFetcher; nil for parses that don't supply a fetcher
	// (in which case @import silently no-ops, matching pre-LOU-138-phase-6
	// behavior). Same shape as CSSFetcher; defined as a bare function
	// type here to avoid forward references that would constrain field
	// ordering.
	CSSResourceFetcher func(uri string) (string, error)

	// Selection is the document's current DOM Selection/Range, or nil if
	// nothing is selected. Mirrors Blink's FrameSelection (owned by
	// LocalFrame; reached via Document::GetFrame()->Selection() —
	// third_party/blink/renderer/core/editing/frame_selection.h @ blob
	// 5011f9bab79cae12d0d7757103eac204e4aaf1b2), simplified to a single
	// Range since louis14 (like the DOM Selection API surface used by the
	// LOU-344 target tests) only ever needs one active range — no
	// multi-range selection support. Lives on *Document (alongside
	// FocusedElement) rather than on the JS engine so pkg/render's paint
	// pass can read it without importing pkg/js. Maintained by the
	// document.createRange/getSelection/addRange/removeAllRanges bindings
	// in pkg/js/dom_selection.go.
	Selection *Range
}

// Range is a simplified DOM Range: a pair of boundary points
// (startContainer, startOffset) / (endContainer, endOffset). Mirrors
// Blink's Range boundary-point model (core/dom/range.h) — for an element
// container, offset is a child index; for a text container, offset is a
// UTF-16 character offset, per DOM §5.4 "boundary points".
type Range struct {
	StartContainer *Node
	StartOffset    int
	EndContainer   *Node
	EndOffset      int
}

// UTF16OffsetToByteOffset converts a Range UTF-16 code-unit offset (this
// type's StartOffset/EndOffset, per DOM §5.4 above) into the equivalent
// UTF-8 byte offset into text — text is always a Go string (UTF-8), e.g.
// a Node.Text, while a Range offset into a text container counts UTF-16
// code units. They only coincide for all-ASCII/BMP-single-unit text,
// where every rune is both 1 UTF-16 unit and (for ASCII) 1 byte; outside
// that (e.g. ∫ U+222B, a single UTF-16 unit but 3 UTF-8 bytes), using the
// raw offset as a byte index slices mid-rune and corrupts the string.
// Lives next to Range (rather than in a caller package, e.g. pkg/render)
// so every consumer of a Range text-container offset converts through
// the same function instead of re-deriving the UTF-16/UTF-8 distinction
// per call site. utf16Offset is clamped to text's UTF-16 length so an
// out-of-range Range offset (disallowed on a live Range by the DOM spec,
// but defended against here) degrades to "select to end" rather than
// panicking or returning an invalid index.
//
// No Blink analog cited: Blink's core/editing/position.h works in UTF-16
// natively throughout (WTF::String is UTF-16), so this conversion has no
// equivalent step there — it exists purely because louis14's Node.Text is
// a Go (UTF-8) string while the DOM Range contract above is UTF-16.
func UTF16OffsetToByteOffset(text string, utf16Offset int) int {
	if utf16Offset <= 0 {
		return 0
	}
	units := 0
	for byteIdx, r := range text {
		if units >= utf16Offset {
			return byteIdx
		}
		// utf16.RuneLen returns 2 for runes needing a surrogate pair
		// (outside the Basic Multilingual Plane), 1 for every BMP rune
		// (all of ASCII and ∫ U+222B included), -1 only for values that
		// cannot be UTF-16 encoded at all (unpaired surrogates) — which
		// cannot occur here since r came from ranging over a valid Go
		// string, so the -1 case is unreachable and not special-cased.
		units += utf16.RuneLen(r)
	}
	return len(text)
}

func NewDocument() *Document {
	return &Document{
		Root: &Node{
			Type:     ElementNode,
			TagName:  "document",
			Children: make([]*Node, 0),
		},
		Stylesheets: make([]StylesheetSource, 0),
		Scripts:     make([]string, 0),
	}
}

// NotifyNodeDetached must be called by DOM mutation paths (JS removeChild,
// replaceChild, replaceWith, etc.) after a subtree has been removed from
// the document tree. If the document's focused element is anywhere inside
// the removed subtree, focus is cleared — this matches Blink's behavior
// in Document::NodeChildrenWillBeRemoved/NodeWillBeRemoved, which blur
// the focused element when its ancestor chain breaks. Cheap: the walk is
// bounded by the removed subtree, and skipped entirely when no element
// is currently focused.
func (d *Document) NotifyNodeDetached(removed *Node) {
	if d == nil || d.FocusedElement == nil || removed == nil {
		return
	}
	if removed.Contains(d.FocusedElement) {
		d.SetFocusedElement(nil)
	}
}

// SetFocusedElement makes node the document's currently focused element,
// or clears focus if node is nil. Mirrors Blink's
// Document::SetFocusedElement: the old focused element and all its
// ancestors get their IsFocused / HasFocusWithin bits cleared, then the
// new focused element and all its ancestors get them set. Per Selectors 4
// §9.4 :focus-within propagates up regardless of containing block, so the
// walk uses the DOM parent chain. The synthetic "document" root node
// itself is not marked.
//
// A non-element node is ignored (matching the spec: only elements can be
// focused).
func (d *Document) SetFocusedElement(node *Node) {
	if node != nil && node.Type != ElementNode {
		return
	}
	if d.FocusedElement == node {
		return
	}
	if old := d.FocusedElement; old != nil {
		old.IsFocused = false
		for a := old; a != nil && a.TagName != "document"; a = a.Parent {
			a.HasFocusWithin = false
		}
	}
	d.FocusedElement = node
	if node != nil {
		node.IsFocused = true
		for a := node; a != nil && a.TagName != "document"; a = a.Parent {
			a.HasFocusWithin = true
		}
	}
}

func (n *Node) GetAttribute(name string) (string, bool) {
	if n.Attributes == nil {
		return "", false
	}
	val, ok := n.Attributes[name]
	return val, ok
}

// InheritedLanguage returns the language tag inherited from the DOM tree by
// walking up the parent chain until a `lang` attribute is found (or the root
// is reached). Mirrors Blink's `Element::ComputeInheritedLanguage()` at
// third_party/blink/renderer/core/dom/element.cc:10328 @ SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Returns "" if no ancestor declares
// a `lang` attribute.
//
// Note: louis14 ignores `xml:lang` (Blink's algorithm consults it first because
// xml:lang takes precedence per XHTML §C.7). This is fine for HTML inputs;
// XHTML documents are not in scope.
func (n *Node) InheritedLanguage() string {
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Type != ElementNode {
			continue
		}
		if v, ok := cur.GetAttribute("lang"); ok && v != "" {
			return v
		}
	}
	return ""
}

// AddChild adds a child node and sets up the parent relationship
func (n *Node) AddChild(child *Node) {
	child.Parent = n
	n.Children = append(n.Children, child)
}

// AppendText creates a text node and adds it as a child
func (n *Node) AppendText(text string) {
	if text == "" {
		return
	}
	textNode := &Node{
		Type:   TextNode,
		Text:   text,
		Parent: n,
	}
	n.Children = append(n.Children, textNode)
}

// AppendTextWithRaw creates a text node with both normalized and raw text preserved.
// rawText is the original text before whitespace normalization.
func (n *Node) AppendTextWithRaw(text, rawText string) {
	if text == "" {
		return
	}
	textNode := &Node{
		Type:    TextNode,
		Text:    text,
		RawText: rawText,
		Parent:  n,
	}
	n.Children = append(n.Children, textNode)
}

// RemoveChild removes the given child from this node's children list,
// clears its parent pointer, and returns the removed child.
// Returns nil if child is not found.
func (n *Node) RemoveChild(child *Node) *Node {
	for i, c := range n.Children {
		if c == child {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			child.Parent = nil
			return child
		}
	}
	return nil
}

// InsertBefore inserts newChild before refChild in this node's children.
// If refChild is nil, appends newChild at the end.
// If newChild already has a parent, it is removed from that parent first.
func (n *Node) InsertBefore(newChild, refChild *Node) *Node {
	// Remove from old parent if re-parenting
	if newChild.Parent != nil {
		newChild.Parent.RemoveChild(newChild)
	}

	if refChild == nil {
		n.AddChild(newChild)
		return newChild
	}

	for i, c := range n.Children {
		if c == refChild {
			// Insert at position i
			n.Children = append(n.Children, nil)
			copy(n.Children[i+1:], n.Children[i:])
			n.Children[i] = newChild
			newChild.Parent = n
			return newChild
		}
	}

	// refChild not found — append
	n.AddChild(newChild)
	return newChild
}

// CloneNode returns a copy of the node. If deep is true, all descendants
// are cloned recursively. The clone has no parent.
func (n *Node) CloneNode(deep bool) *Node {
	clone := &Node{
		Type:    n.Type,
		TagName: n.TagName,
		Text:    n.Text,
	}
	if n.Attributes != nil {
		clone.Attributes = make(map[string]string, len(n.Attributes))
		for k, v := range n.Attributes {
			clone.Attributes[k] = v
		}
	}
	if deep {
		clone.Children = make([]*Node, len(n.Children))
		for i, child := range n.Children {
			childClone := child.CloneNode(true)
			childClone.Parent = clone
			clone.Children[i] = childClone
		}
	} else {
		clone.Children = make([]*Node, 0)
	}
	return clone
}

// Contains returns true if other is a descendant of n (or n itself).
func (n *Node) Contains(other *Node) bool {
	if n == other {
		return true
	}
	for _, child := range n.Children {
		if child.Contains(other) {
			return true
		}
	}
	return false
}

// IndexInParent returns the index of this node among its parent's children,
// or -1 if it has no parent.
func (n *Node) IndexInParent() int {
	if n.Parent == nil {
		return -1
	}
	for i, c := range n.Parent.Children {
		if c == n {
			return i
		}
	}
	return -1
}

// Serialize returns the innerHTML of this node — the serialized HTML of
// all child nodes, but not the node's own tags.
func (n *Node) Serialize() string {
	var sb strings.Builder
	for _, child := range n.Children {
		serializeNode(&sb, child)
	}
	return sb.String()
}

// SerializeOuter returns the outerHTML of this node — the node's own tags
// plus all descendants.
func (n *Node) SerializeOuter() string {
	var sb strings.Builder
	serializeNode(&sb, n)
	return sb.String()
}

func serializeNode(sb *strings.Builder, n *Node) {
	if n.Type == TextNode {
		sb.WriteString(escapeHTML(n.Text))
		return
	}

	sb.WriteByte('<')
	sb.WriteString(n.TagName)

	// Sort attributes for deterministic output
	if len(n.Attributes) > 0 {
		keys := make([]string, 0, len(n.Attributes))
		for k := range n.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteByte(' ')
			sb.WriteString(k)
			sb.WriteString(`="`)
			sb.WriteString(escapeAttr(n.Attributes[k]))
			sb.WriteByte('"')
		}
	}

	if isVoidElement(n.TagName) {
		sb.WriteString(">")
		return
	}

	sb.WriteByte('>')
	for _, child := range n.Children {
		serializeNode(sb, child)
	}
	sb.WriteString("</")
	sb.WriteString(n.TagName)
	sb.WriteByte('>')
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func isVoidElement(tag string) bool {
	switch tag {
	case "br", "hr", "img", "input", "meta", "link", "area", "base",
		"col", "embed", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// IsReplacedElementTag reports whether the given HTML tag name names a
// replaced element — one with intrinsic dimensions whose rendering is
// not directly controlled by CSS. Replaced elements are laid out as
// atomic inlines (CSS Display 3 §2.2) regardless of computed
// `display`, and they act as boundaries for several propagating
// effects (e.g. CSS Text Decor 4 §1.3 text-decoration propagation,
// which does NOT cross into an atomic-inline subtree).
//
// Single source of truth for the louis14 codebase: callers in
// pkg/layout (IsReplacedElement) and pkg/css (text-decoration cascade
// boundary) both reach for this list. Add a tag to this switch and
// every replaced-element behavior site picks it up.
func IsReplacedElementTag(tag string) bool {
	switch tag {
	case "img", "video", "canvas", "svg", "iframe", "embed", "object",
		"input", "textarea", "select", "button":
		return true
	}
	return false
}

// IsSemiReplacedElementTag returns true for form control elements that have
// intrinsic sizes but are NOT fully replaced: they must stretch when positioned
// absolutely with explicit insets and auto width/height (CSS Position §10.3.7).
// Per https://github.com/w3c/csswg-drafts/issues/6789, form controls are
// semi-replaced: they differ from fully-replaced elements (img, video, etc.)
// which never stretch. Mirrors Blink's exclusion of form controls from
// LayoutReplaced (they are LayoutButton, LayoutTextControl, LayoutFlexibleBox).
func IsSemiReplacedElementTag(tag string) bool {
	switch tag {
	case "input", "textarea", "select", "button":
		return true
	}
	return false
}
