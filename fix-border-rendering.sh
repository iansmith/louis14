#!/bin/bash
set -e

FILE="pkg/render/render.go"

# Fix background width/height (line 281-282)
sed -i '' '281s|bgWidth := box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right|// Background covers border box (Box.Width/Height are border-box dimensions)\
		bgWidth := box.Width|' "$FILE"

sed -i '' '282s|bgHeight := box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom|bgHeight := box.Height|' "$FILE"

# Fix second background calculation (line 367-368)
sed -i '' '368s|bgWidth := box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right|// Background covers border box\
			bgWidth := box.Width|' "$FILE"

sed -i '' '369s|bgHeight := box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom|bgHeight := box.Height|' "$FILE"

# Fix rounded border (line 462-463)
sed -i '' 's|borderWidth := box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right - box.Border.Left|borderWidth := box.Width - box.Border.Left|' "$FILE"
sed -i '' 's|borderHeight := box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom - box.Border.Top|borderHeight := box.Height - box.Border.Top|' "$FILE"

# Fix border box coordinates (line 473-478)
sed -i '' 's|outerRight := box.X + box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right|outerRight := box.X + box.Width|' "$FILE"
sed -i '' 's|outerBottom := effectiveY + box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom|outerBottom := effectiveY + box.Height|' "$FILE"
sed -i '' 's|innerRight := box.X + box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right|innerRight := box.X + box.Width - box.Border.Right|' "$FILE"
sed -i '' 's|innerBottom := effectiveY + box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom|innerBottom := effectiveY + box.Height - box.Border.Bottom|' "$FILE"

# Fix box shadow (line 554-555)
sed -i '' 's|boxWidth := box.Width + box.Padding.Left + box.Padding.Right|// Box shadow applies to padding box (border-box minus borders)\
	boxWidth := box.Width - box.Border.Left - box.Border.Right|' "$FILE"
sed -i '' 's|boxHeight := box.Height + box.Padding.Top + box.Padding.Bottom|boxHeight := box.Height - box.Border.Top - box.Border.Bottom|' "$FILE"

# Fix background image (line 761-762)
sed -i '' 's|bgWidth := box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right|// Background image covers border box\
	bgWidth := box.Width|g' "$FILE"
sed -i '' 's|bgHeight := box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom|bgHeight := box.Height|g' "$FILE"

echo "Fixed all border box dimension calculations in render.go"
