package layout

import (
	"strings"
	"testing"
)

// pseudoTexts returns the non-empty texts of the pseudo-element boxes named
// tag ("::marker", "::before") under b in tree order. An outside marker is a
// ::marker box holding a ::marker text box, so empty entries are skipped.
func pseudoTexts(b *Box, tag string) []string {
	if b == nil {
		return nil
	}
	var out []string
	if b.Node != nil && b.Node.TagName == tag {
		if text := strings.TrimSpace(b.Text); text != "" {
			out = append(out, text)
		}
	}
	for _, c := range b.Children {
		out = append(out, pseudoTexts(c, tag)...)
	}
	return out
}

// An element with style containment is a list owner: its list items form
// their own list, counting up from 1, and do not consume the outer list's
// ordinals (ListItemOrdinal::IsListOwner / HasStyleContainment,
// html/list_item_ordinal.cc:21-36, :76-81 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A style-contained <li> still
// takes its own step in the outer list: CountersAttachmentContext::EnterObject
// creates the implicit list-item counter before it enters the containment
// scope (css/counters_attachment_context.cc:214-256, :303-319). The step is
// -1 only when the item's enclosing list is reversed (IsInReversedOrderedList,
// list_item_ordinal.cc:46-64), and the reversed start count skips list-owner
// subtrees (InitialCounterForReversedOrderedList, :359-425). Mirrors WPT
// css-contain/contain-style-ol-ordinal-*.
func TestListItemOrdinalsUnderStyleContainment(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		pseudo string
		want   []string
	}{
		{"li-container", `<ol><li>A</li><li>B</li><li style="contain:style">X</li><li style="contain:style">Y</li><li>C</li><li>D</li></ol>`,
			"::marker", []string{"1.", "2.", "3.", "4.", "5.", "6."}},
		{"reversed", `<ol reversed><li>A</li><li>B</li><div style="contain:style"><li>X</li><li>Y</li></div><li>C</li><li>D</li></ol>`,
			"::marker", []string{"4.", "3.", "1.", "2.", "2.", "1."}},
		{"start-reversed", `<ol start=10 reversed><li>A</li><li>B</li><div style="contain:style"><li>X</li><li>Y</li></div><li>C</li><li>D</li></ol>`,
			"::marker", []string{"10.", "9.", "1.", "2.", "8.", "7."}},
		{"pseudo-reversed", `<style>li::before { content: counter(list-item); }</style><ol start=10 reversed><li>A</li><li>B</li><div style="contain:style"><li>X</li><li>Y</li></div><li>C</li><li>D</li></ol>`,
			"::before", []string{"10", "9", "1", "2", "8", "7"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fl := layoutFallbackHTML(t, `<!DOCTYPE html>`+tc.src, nil)
			if got := pseudoTexts(fl.root, tc.pseudo); strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("%s texts %v, want %v", tc.pseudo, got, tc.want)
			}
		})
	}
}
