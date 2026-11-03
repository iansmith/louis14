package graphics

// AntiAliasingMode selects whether a GraphicsContext clip anti-aliases its
// edge. Mirrors Blink's AntiAliasingMode
// (platform/graphics/graphics_context_types.h:43 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type AntiAliasingMode int

const (
	NotAntiAliased AntiAliasingMode = iota
	AntiAliased
)
