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
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14 gets there with its
// counter model:
//   - a style-contained <li> takes its implicit list-item step before its
//     containment boundary is pushed, as CountersAttachmentContext::
//     EnterObject orders MaybeCreateListItemCounter
//     (css/counters_attachment_context.cc:214-256), so it steps the outer
//     list;
//   - the step's direction comes from the counter it updates, and past a
//     containment boundary that is a fresh counter (UpdateCounterValue,
//     :622-627), which is not reversed;
//   - an <ol>'s reversed start count skips list owners whole, nested lists
//     and style-contained elements alike (InitialCounterForReversedOrderedList,
//     list_item_ordinal.cc:359-425); other reversed counters skip only a
//     contained element's subtree.
//
// Mirrors WPT css-contain/contain-style-ol-ordinal-*.
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

// TestStyleContainmentCounterEdges covers the edges of style containment in
// louis14's counter model (see TestListItemOrdinalsUnderStyleContainment).
func TestStyleContainmentCounterEdges(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		pseudo string
		want   []string
	}{
		// A style-contained <li> is a list owner, so the reversed start
		// count skips it whole (InitialCounterForReversedOrderedList walks
		// NextListItem, html/list_item_ordinal.cc:366-372), yet it still
		// steps the outer list (PreviousListItem does not skip owners). As
		// in Chrome: count 2, numbered 2, 1, 0.
		{"contained li skipped in reversed start", `<ol reversed><li>A</li><li style="contain:style">B</li><li>C</li></ol>`,
			"::marker", []string{"2.", "1.", "0."}},
		// Style containment cuts off every counter's descendants, not only
		// list-item under an <ol>: the auto-initial count of reversed(c)
		// skips the contained subtree, whose increments make a fresh c.
		{"contained subtree skipped for any reversed counter", `<style>.r { counter-reset: reversed(c); } .i { counter-increment: c -1; } .i::before { content: counter(c); }</style>` +
			`<div class="r"><span class="i"></span><div style="contain:style"><span class="i"></span><span class="i"></span></div><span class="i"></span></div>`,
			"::before", []string{"2", "-1", "-2", "1"}},
		// A reversed list with style containment is still its items' list:
		// the boundary it pushes does not cut its own items off from it.
		{"contained reversed ol", `<ol reversed style="contain:style"><li>A</li><li>B</li></ol>`,
			"::marker", []string{"2.", "1."}},
		// The direction is read after stale counters are pruned: once the
		// reversed list's scope has ended, a later list item counts up.
		{"direction after stale counters pruned", `<div><ol reversed><li>a</li></ol></div><div><div style="display:list-item; list-style-type: decimal">b</div></div>`,
			"::marker", []string{"1.", "1."}},
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
