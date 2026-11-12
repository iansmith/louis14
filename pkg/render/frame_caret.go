package render

import "louis14/pkg/html"

// SetFocusedElement supplies the document's focused element, which decides
// whether the caret at a collapsed selection paints (FrameCaret::
// ShouldShowCaret, core/editing/frame_caret.cc:308-337 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) SetFocusedElement(focused *html.Node) {}
