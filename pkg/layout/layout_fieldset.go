package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// This file holds the layout-tree side of a <fieldset>: which element boxes
// become fieldset containers, the rendered legend, and the anonymous
// fieldset content box with its style. Mirrors Blink's LayoutFieldset
// (core/layout/forms/layout_fieldset.{h,cc} @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Layout itself is
// FieldsetLayoutAlgorithm (fieldset_layout_algorithm.go).

// IsFieldsetContainerElement reports whether node, with computed style, gets
// a fieldset container box. Blink creates a LayoutFieldset for every rendered
// <fieldset> whatever its display (HTMLFieldSetElement::CreateLayoutObject,
// html_field_set_element.cc:223-225 @ a9f50e522efa). louis14 restricts this
// to the block-level and atomic-inline displays it lays out as boxes: a
// display:inline or table-part fieldset keeps its display-driven box.
func IsFieldsetContainerElement(node *html.Node, style *css.Style) bool {
	if node == nil || node.Type != html.ElementNode || node.TagName != "fieldset" || style == nil {
		return false
	}
	switch style.GetDisplay() {
	case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayInlineBlock,
		css.DisplayListItem, css.DisplayFlex, css.DisplayInlineFlex,
		css.DisplayGrid, css.DisplayInlineGrid:
		return true
	}
	return false
}

// isRenderedLegendCandidate reports whether child can be a fieldset's
// rendered legend: an in-flow, non-floating <legend>. Mirrors
// LayoutObject::IsRenderedLegendCandidate (layout_object.h:1647-1656 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func isRenderedLegendCandidate(child *LayoutInputNode) bool {
	return child != nil && child.DOMNode != nil && child.DOMNode.Type == html.ElementNode &&
		child.DOMNode.TagName == "legend" && child.style != nil && !isOutOfFlowOrFloat(child)
}

// fieldsetContentDisplay is the display of the anonymous fieldset content
// box: flex or grid for a flex or grid fieldset, flow-root otherwise.
// Mirrors LayoutFieldset::InsertedIntoTree (layout_fieldset.cc:73-89 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func fieldsetContentDisplay(fieldsetStyle *css.Style) css.DisplayType {
	switch fieldsetStyle.GetDisplay() {
	case css.DisplayFlex, css.DisplayInlineFlex:
		return css.DisplayFlex
	case css.DisplayGrid, css.DisplayInlineGrid:
		return css.DisplayGrid
	}
	return css.DisplayFlowRoot
}

// fieldsetContentInheritedProperties are the fieldset properties the
// anonymous content box takes over (HTML §15.3.13 "anonymous fieldset
// content box"). Mirrors the list in LayoutFieldset::UpdateAnonymousChildStyle
// (layout_fieldset.cc:106-201 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Shorthand remnants louis14 keeps alongside their longhands ("overflow",
// "gap") travel with them. Row-rule / rule-inset / column-rule-break
// properties are absent: louis14 does not parse them.
var fieldsetContentInheritedProperties = []string{
	"align-content", "align-items",
	"border-bottom-left-radius", "border-bottom-right-radius",
	"border-top-left-radius", "border-top-right-radius",
	"padding-top", "padding-right", "padding-bottom", "padding-left",
	"box-decoration-break",
	"column-gap", "column-fill",
	"column-rule-color", "column-rule-style", "column-rule-width",
	"flex-direction", "flex-wrap",
	"grid-auto-columns", "grid-auto-flow", "grid-auto-rows",
	"grid-column-end", "grid-column-start", "grid-row-end", "grid-row-start",
	"grid-template-columns", "grid-template-rows", "grid-template-areas",
	"row-gap", "gap",
	"justify-content", "justify-items",
	"overflow", "overflow-x", "overflow-y",
}

// fieldsetColumnProperties are copied only when the fieldset specifies
// columns (layout_fieldset.cc:130-141).
var fieldsetColumnProperties = []string{
	"column-count", "column-width", "column-height", "column-wrap",
}

// fieldsetContentStyle builds the anonymous fieldset content box's style:
// an anonymous style with the content display, plus the fieldset properties
// UpdateAnonymousChildStyle forwards. Mirrors
// LayoutBlock::CreateAnonymousWithParentAndDisplay + LayoutFieldset::
// UpdateAnonymousChildStyle (layout_fieldset.cc:91-92, :106-201 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func fieldsetContentStyle(fieldsetStyle *css.Style) *css.Style {
	s := css.CreateAnonymousStyleWithDisplay(fieldsetStyle, string(fieldsetContentDisplay(fieldsetStyle)))
	copyProps := func(props []string) {
		for _, prop := range props {
			if v, ok := fieldsetStyle.Properties[prop]; ok {
				s.Properties[prop] = v
			}
		}
	}
	copyProps(fieldsetContentInheritedProperties)
	if isMulticolContainer(fieldsetStyle) {
		copyProps(fieldsetColumnProperties)
	}
	return s
}

// buildFieldsetChildren arranges a fieldset's layout children as
// LayoutFieldset::AddChild does (layout_fieldset.cc:32-56 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the first rendered-legend
// candidate becomes the first child; every other child (::before/::after
// included) goes into the anonymous fieldset content box, which gets its own
// anonymous-block wrapping as a block container.
func (b *LayoutTreeBuilder) buildFieldsetChildren(fieldset *LayoutInputNode, rawChildren []*LayoutInputNode) []*LayoutInputNode {
	var legend *LayoutInputNode
	rest := make([]*LayoutInputNode, 0, len(rawChildren))
	for _, c := range rawChildren {
		if legend == nil && isRenderedLegendCandidate(c) {
			legend = c
			continue
		}
		rest = append(rest, c)
	}
	contentStyle := fieldsetContentStyle(fieldset.style)
	content := &LayoutInputNode{style: contentStyle, isAnonymous: true}
	content.children = b.maybeWrapAnonymousBlocks(rest, contentStyle)
	fieldset.isFieldsetContainer = true
	if legend != nil {
		return []*LayoutInputNode{legend, content}
	}
	return []*LayoutInputNode{content}
}
