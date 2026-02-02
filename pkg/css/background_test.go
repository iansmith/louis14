package css

import (
	"testing"
)

func TestParseURLValue(t *testing.T) {
	tests := []struct {
		input    string
		wantURL  string
		wantOK   bool
	}{
		{"url(image.png)", "image.png", true},
		{"url('image.png')", "image.png", true},
		{`url("image.png")`, "image.png", true},
		{"url( image.png )", "image.png", true},
		{"url(data:image/png;base64,iVBOR)", "data:image/png;base64,iVBOR", true},
		{"url()", "", false},
		{"none", "", false},
		{"", "", false},
		{"url(  'spaced.png'  )", "spaced.png", true},
	}

	for _, tt := range tests {
		url, ok := ParseURLValue(tt.input)
		if ok != tt.wantOK || url != tt.wantURL {
			t.Errorf("ParseURLValue(%q) = (%q, %v), want (%q, %v)", tt.input, url, ok, tt.wantURL, tt.wantOK)
		}
	}
}

func TestGetBackgroundImage(t *testing.T) {
	s := NewStyle()
	s.Set("background-image", "url(test.png)")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "test.png" {
		t.Errorf("GetBackgroundImage() = (%q, %v), want (\"test.png\", true)", url, ok)
	}
}

func TestGetBackgroundImage_DataURI(t *testing.T) {
	s := NewStyle()
	s.Set("background-image", "url(data:image/png;base64,abc123)")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "data:image/png;base64,abc123" {
		t.Errorf("GetBackgroundImage() = (%q, %v)", url, ok)
	}
}

func TestGetBackgroundImage_NotSet(t *testing.T) {
	s := NewStyle()
	_, ok := s.GetBackgroundImage()
	if ok {
		t.Error("expected false for unset background-image")
	}
}

func TestExpandBackgroundShorthand_URL(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "url(test.png)")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "test.png" {
		t.Errorf("background shorthand url: got (%q, %v)", url, ok)
	}
}

func TestExpandBackgroundShorthand_URLAndColor(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "red url(bg.png) no-repeat")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "bg.png" {
		t.Errorf("background-image: got (%q, %v)", url, ok)
	}

	if color, ok := s.Get("background-color"); !ok || color != "red" {
		t.Errorf("background-color: got (%q, %v)", color, ok)
	}

	if repeat, ok := s.Get("background-repeat"); !ok || repeat != "no-repeat" {
		t.Errorf("background-repeat: got (%q, %v)", repeat, ok)
	}
}

func TestExpandBackgroundShorthand_DataURI(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "url(data:image/png;base64,iVBORw0KGgo=) no-repeat")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "data:image/png;base64,iVBORw0KGgo=" {
		t.Errorf("background data URI: got (%q, %v)", url, ok)
	}

	if repeat, ok := s.Get("background-repeat"); !ok || repeat != "no-repeat" {
		t.Errorf("background-repeat: got (%q, %v)", repeat, ok)
	}
}

func TestExpandBackgroundShorthand_ColorOnly(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "yellow")

	if color, ok := s.Get("background-color"); !ok || color != "yellow" {
		t.Errorf("background-color: got (%q, %v)", color, ok)
	}

	_, ok := s.GetBackgroundImage()
	if ok {
		t.Error("expected no background-image for color-only background")
	}
}

func TestGetBackgroundRepeat(t *testing.T) {
	tests := []struct {
		value string
		want  BackgroundRepeatType
	}{
		{"no-repeat", BackgroundRepeatNoRepeat},
		{"repeat-x", BackgroundRepeatRepeatX},
		{"repeat-y", BackgroundRepeatRepeatY},
		{"repeat", BackgroundRepeatRepeat},
	}

	for _, tt := range tests {
		s := NewStyle()
		s.Set("background-repeat", tt.value)
		if got := s.GetBackgroundRepeat(); got != tt.want {
			t.Errorf("GetBackgroundRepeat() for %q = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestGetBackgroundRepeat_Default(t *testing.T) {
	s := NewStyle()
	if got := s.GetBackgroundRepeat(); got != BackgroundRepeatRepeat {
		t.Errorf("default GetBackgroundRepeat() = %q, want repeat", got)
	}
}

func TestGetBackgroundPosition(t *testing.T) {
	s := NewStyle()
	s.Set("background-position", "-46px 0")
	pos := s.GetBackgroundPosition()
	if pos.X != -46 || pos.Y != 0 {
		t.Errorf("GetBackgroundPosition() = (%v, %v), want (-46, 0)", pos.X, pos.Y)
	}
}

func TestGetBackgroundPosition_Default(t *testing.T) {
	s := NewStyle()
	pos := s.GetBackgroundPosition()
	if pos.X != 0 || pos.Y != 0 {
		t.Errorf("default GetBackgroundPosition() = (%v, %v), want (0, 0)", pos.X, pos.Y)
	}
}

func TestExpandBackgroundShorthand_WithPosition(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "background", "url(sprite.png) -46px 0 no-repeat")

	url, ok := s.GetBackgroundImage()
	if !ok || url != "sprite.png" {
		t.Errorf("background-image: got (%q, %v)", url, ok)
	}

	pos, ok := s.Get("background-position")
	if !ok || pos != "-46px 0" {
		t.Errorf("background-position: got (%q, %v)", pos, ok)
	}

	repeat, ok := s.Get("background-repeat")
	if !ok || repeat != "no-repeat" {
		t.Errorf("background-repeat: got (%q, %v)", repeat, ok)
	}
}

func TestParseInlineStyle_BackgroundImage(t *testing.T) {
	s := ParseInlineStyle("background-image: url(test.png)")
	url, ok := s.GetBackgroundImage()
	if !ok || url != "test.png" {
		t.Errorf("ParseInlineStyle background-image: got (%q, %v)", url, ok)
	}
}
