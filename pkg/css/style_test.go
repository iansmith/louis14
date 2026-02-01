package css

import "testing"

func TestParseInlineStyle_SingleProperty(t *testing.T) {
	style := ParseInlineStyle("color: red")
	value, ok := style.Get("color")
	if !ok || value != "red" {
		t.Error("expected color='red'")
	}
}

func TestParseInlineStyle_MultipleProperties(t *testing.T) {
	style := ParseInlineStyle("color: red; width: 100px")
	color, _ := style.Get("color")
	width, _ := style.Get("width")
	if color != "red" || width != "100px" {
		t.Error("expected both properties to parse")
	}
}

func TestGetLength_PixelValue(t *testing.T) {
	style := ParseInlineStyle("width: 100px")
	width, ok := style.GetLength("width")
	if !ok || width != 100.0 {
		t.Errorf("expected width=100.0, got %f", width)
	}
}

func TestParseColor_BasicColors(t *testing.T) {
	tests := map[string]Color{
		"red":   {255, 0, 0},
		"blue":  {0, 0, 255},
		"green": {0, 128, 0},
	}
	for name, expected := range tests {
		color, ok := ParseColor(name)
		if !ok || color != expected {
			t.Errorf("color %s: expected %+v, got %+v", name, expected, color)
		}
	}
}

// Phase 2 tests: Box model properties

func TestParseInlineStyle_MarginShorthand(t *testing.T) {
	style := ParseInlineStyle("margin: 10px")
	margin := style.GetMargin()

	if margin.Top != 10 || margin.Right != 10 || margin.Bottom != 10 || margin.Left != 10 {
		t.Errorf("expected all margins to be 10, got %+v", margin)
	}
}

func TestParseInlineStyle_MarginTwoValues(t *testing.T) {
	style := ParseInlineStyle("margin: 10px 20px")
	margin := style.GetMargin()

	if margin.Top != 10 || margin.Bottom != 10 {
		t.Errorf("expected top/bottom margins to be 10, got %+v", margin)
	}
	if margin.Right != 20 || margin.Left != 20 {
		t.Errorf("expected left/right margins to be 20, got %+v", margin)
	}
}

func TestParseInlineStyle_MarginFourValues(t *testing.T) {
	style := ParseInlineStyle("margin: 10px 20px 30px 40px")
	margin := style.GetMargin()

	if margin.Top != 10 || margin.Right != 20 || margin.Bottom != 30 || margin.Left != 40 {
		t.Errorf("expected margins 10,20,30,40, got %+v", margin)
	}
}

func TestParseInlineStyle_PaddingShorthand(t *testing.T) {
	style := ParseInlineStyle("padding: 15px")
	padding := style.GetPadding()

	if padding.Top != 15 || padding.Right != 15 || padding.Bottom != 15 || padding.Left != 15 {
		t.Errorf("expected all padding to be 15, got %+v", padding)
	}
}

func TestParseInlineStyle_BorderShorthand(t *testing.T) {
	style := ParseInlineStyle("border: 2px solid black")

	borderWidth := style.GetBorderWidth()
	if borderWidth.Top != 2 || borderWidth.Right != 2 {
		t.Errorf("expected border width to be 2, got %+v", borderWidth)
	}

	borderStyle, ok := style.Get("border-style")
	if !ok || borderStyle != "solid" {
		t.Errorf("expected border-style 'solid', got '%s'", borderStyle)
	}

	borderColor, ok := style.Get("border-color")
	if !ok || borderColor != "black" {
		t.Errorf("expected border-color 'black', got '%s'", borderColor)
	}
}

func TestParseInlineStyle_IndividualMargins(t *testing.T) {
	style := ParseInlineStyle("margin-top: 5px; margin-left: 10px")
	margin := style.GetMargin()

	if margin.Top != 5 {
		t.Errorf("expected margin-top 5, got %f", margin.Top)
	}
	if margin.Left != 10 {
		t.Errorf("expected margin-left 10, got %f", margin.Left)
	}
	if margin.Right != 0 || margin.Bottom != 0 {
		t.Errorf("expected other margins to be 0, got %+v", margin)
	}
}

func TestParseInlineStyle_CombinedBoxModel(t *testing.T) {
	style := ParseInlineStyle("margin: 10px; padding: 20px; border: 1px solid red")

	margin := style.GetMargin()
	if margin.Top != 10 {
		t.Errorf("expected margin 10, got %+v", margin)
	}

	padding := style.GetPadding()
	if padding.Top != 20 {
		t.Errorf("expected padding 20, got %+v", padding)
	}

	borderWidth := style.GetBorderWidth()
	if borderWidth.Top != 1 {
		t.Errorf("expected border width 1, got %+v", borderWidth)
	}
}
