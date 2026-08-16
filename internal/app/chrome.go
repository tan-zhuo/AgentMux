package app

import "github.com/wailsapp/wails/v3/pkg/application"

// Chrome is the native window styling for a theme: the colours Wails can only
// apply when the window is created, before any JavaScript has run.
//
// Everything inside the webview is styled from CSS custom properties and
// switches live; these three colours cannot, because Windows resolves the
// system border once at window creation. Reading the persisted theme at startup
// is what keeps the frame from disagreeing with the UI it surrounds.
type Chrome struct {
	Background     application.RGBA
	Border         *uint32
	BorderInactive *uint32
	Light          bool
}

// chromeByTheme mirrors the `window` and border colours of the themes in
// frontend/src/lib/themes.ts. Adding a theme means adding a row here too;
// an unknown id falls back to Midnight rather than failing to start.
func chromeByTheme(id string) Chrome {
	switch id {
	case "graphite":
		return Chrome{
			Background:     application.NewRGB(0x10, 0x10, 0x10),
			Border:         application.NewRGBPtr(0x2c, 0x2c, 0x2c),
			BorderInactive: application.NewRGBPtr(0x1c, 0x1c, 0x1c),
		}
	case "nord":
		return Chrome{
			Background:     application.NewRGB(0x2e, 0x34, 0x40),
			Border:         application.NewRGBPtr(0x4c, 0x56, 0x6a),
			BorderInactive: application.NewRGBPtr(0x3b, 0x42, 0x52),
		}
	case "solarized-dark":
		return Chrome{
			Background:     application.NewRGB(0x00, 0x2b, 0x36),
			Border:         application.NewRGBPtr(0x14, 0x50, 0x5f),
			BorderInactive: application.NewRGBPtr(0x07, 0x36, 0x42),
		}
	case "gruvbox-dark":
		return Chrome{
			Background:     application.NewRGB(0x28, 0x28, 0x28),
			Border:         application.NewRGBPtr(0x50, 0x49, 0x45),
			BorderInactive: application.NewRGBPtr(0x3c, 0x38, 0x36),
		}
	case "daylight":
		return Chrome{
			Background:     application.NewRGB(0xf7, 0xf8, 0xfa),
			Border:         application.NewRGBPtr(0xd5, 0xda, 0xe2),
			BorderInactive: application.NewRGBPtr(0xe4, 0xe7, 0xec),
			Light:          true,
		}
	default: // midnight
		return Chrome{
			Background:     application.NewRGB(0x0b, 0x0d, 0x12),
			Border:         application.NewRGBPtr(0x23, 0x29, 0x36),
			BorderInactive: application.NewRGBPtr(0x15, 0x19, 0x22),
		}
	}
}

// WindowChrome returns the native styling for the user's saved theme.
func (c *Core) WindowChrome() Chrome {
	return chromeByTheme(c.Store.GetSetting("theme", "midnight"))
}
