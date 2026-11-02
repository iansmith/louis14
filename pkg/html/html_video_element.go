package html

// PosterImageURL returns a <video>'s poster URL, trimmed of HTML spaces and
// not yet completed against the document base. Mirrors
// HTMLVideoElement::PosterImageURL (html_video_element.cc:859-864 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which also completes it.
func (n *Node) PosterImageURL() string {
	return stripLeadingAndTrailingHTMLSpaces(n.ImageSourceURL())
}
