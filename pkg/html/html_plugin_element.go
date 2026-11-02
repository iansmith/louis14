package html

import (
	"net/url"
	"strings"

	"louis14/pkg/network/mime"
)

// This file ports the parts of Blink's HTMLPlugInElement
// (core/html/html_plugin_element.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) that decide which layout object
// an <object> or <embed> gets. louis14 has no plugins, so the plugin branches
// of GetObjectContentType never match.

// ObjectContentType classifies what an <object>/<embed> embeds. Mirrors
// HTMLPlugInElement::ObjectContentType (core/html/html_plugin_element.h:210-216
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); kPlugin and kExternalPlugin are
// unreachable without plugin data and are not ported.
type ObjectContentType int

const (
	ObjectContentTypeNone ObjectContentType = iota
	ObjectContentTypeImage
	ObjectContentTypeFrame
)

// IsPlugInElement reports whether n is an HTMLPlugInElement (<object> or
// <embed>).
func (n *Node) IsPlugInElement() bool {
	return n != nil && n.Type == ElementNode && (n.TagName == "object" || n.TagName == "embed")
}

// PlugInURL returns the plug-in element's url_: the data attribute of an
// <object> or the src attribute of an <embed>, stripped of leading and
// trailing HTML spaces (HTMLObjectElement::ParseAttribute kDataAttr,
// html_object_element.cc:110-111; HTMLEmbedElement::ParseAttribute kSrcAttr,
// html_embed_element.cc:113-121 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (n *Node) PlugInURL() string {
	return strings.Trim(n.ImageSourceURL(), htmlSpaces)
}

// htmlSpaces are the HTML ASCII whitespace characters
// (StripLeadingAndTrailingHtmlSpaces).
const htmlSpaces = " \t\n\f\r"

// ServiceType returns the plug-in element's service_type_: its type
// attribute lower-cased, cut at the first ';' (HTMLObjectElement::
// ParseAttribute kTypeAttr, html_object_element.cc:102-105;
// HTMLEmbedElement::ParseAttribute, html_embed_element.cc:97-101 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (n *Node) ServiceType() string {
	v, _ := n.GetAttribute("type")
	v = strings.ToLower(v)
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = v[:i]
	}
	return v
}

// getMIMETypeFromURL guesses a MIME type from the extension of the URL's last
// path component. Mirrors GetMIMETypeFromURL (html_plugin_element.cc:76-84 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the extension does not depend
// on the base the URL is completed against, so the raw url_ is used.
func getMIMETypeFromURL(rawURL string) string {
	path := rawURL
	if u, err := url.Parse(rawURL); err == nil {
		path = u.Path
		if u.Opaque != "" {
			path = u.Opaque
		}
	}
	filename := path[strings.LastIndexByte(path, '/')+1:]
	if i := strings.LastIndexByte(filename, '.'); i >= 0 {
		return mime.GetWellKnownMIMETypeForExtension(filename[i+1:])
	}
	return ""
}

// resolveMIMEType mirrors ResolveMIMEType (html_plugin_element.cc:86-92):
// the specified type, else a guess from the URL's extension.
func resolveMIMEType(specifiedType, rawURL string) string {
	if specifiedType != "" {
		return specifiedType
	}
	return getMIMETypeFromURL(rawURL)
}

// GetObjectContentType mirrors HTMLPlugInElement::GetObjectContentType
// (html_plugin_element.cc:643-670 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b)
// without plugin data: an unknown type is a frame, a supported image type an
// image, a supported non-image type a frame, anything else none.
func (n *Node) GetObjectContentType() ObjectContentType {
	mimeType := resolveMIMEType(n.ServiceType(), n.PlugInURL())
	switch {
	case mimeType == "":
		return ObjectContentTypeFrame
	case mime.IsSupportedImageMIMEType(mimeType):
		return ObjectContentTypeImage
	case mime.IsSupportedNonImageMIMEType(mimeType):
		return ObjectContentTypeFrame
	}
	return ObjectContentTypeNone
}

// IsImageType mirrors HTMLPlugInElement::IsImageType
// (html_plugin_element.cc:673-677): the document has a frame, so the content
// type decides.
func (n *Node) IsImageType() bool {
	return n.IsPlugInElement() && n.GetObjectContentType() == ObjectContentTypeImage
}

// ImageSourceURL returns the URL an element's ImageLoader loads: <img> src,
// <object> data, <embed> src. Mirrors Element::ImageSourceURL (src;
// core/dom/element.cc:4222-4224) and its override
// HTMLObjectElement::ImageSourceURL (data; html_object_element.cc:282-284),
// both @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func (n *Node) ImageSourceURL() string {
	attr := "src"
	if n.TagName == "object" {
		attr = "data"
	}
	v, _ := n.GetAttribute(attr)
	return v
}

// CreatesLayoutImage reports whether n's layout object is a LayoutImage: an
// <img> showing its image (HTMLImageElement::CreateLayoutObject,
// html_image_element.cc:517-543 @ d4ecdfed88f962439247c2ad36b8fe47805b1520)
// or an image-typed <object>/<embed> (HTMLPlugInElement::CreateLayoutObject,
// html_plugin_element.cc:349-364 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// louis14 does not model <object> fallback content (UseFallbackContent).
func CreatesLayoutImage(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if n.TagName == "img" {
		return IsReplacedElement(n)
	}
	return n.IsImageType()
}
