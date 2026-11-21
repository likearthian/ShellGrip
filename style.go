package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal"
)

// colors are the app's own, in light and dark windows.
type colors struct {
	// the window chrome stays flat around the inset workspace panels.
	background, accent ui.Color

	card, cardFocused, cardBorder, cardBorderFocused ui.Color
	shadow, shadowFocused                            ui.Color

	text, textMuted, textFaint, iconMuted ui.Color
	hover, pressed                        ui.Color

	track, tabActive, tabSep ui.Color
	tileRim                  ui.Color

	busy, attention, exited ui.Color

	panel, panelBorder, panelSel, backdrop ui.Color
}

var lightColors = colors{
	background: ui.Hex("#e9eaec"),
	accent:     ui.Hex("#4678c7"),

	card:              ui.Hex("#f2f3f5"),
	cardFocused:       ui.Hex("#ffffff"),
	cardBorder:        ui.Hex("#d9dce1"),
	cardBorderFocused: ui.Hex("#b6becb"),
	shadow:            ui.RGBA(30, 35, 45, 0.04),
	shadowFocused:     ui.RGBA(30, 35, 45, 0.08),

	text:      ui.Hex("#24272c"),
	textMuted: ui.Hex("#616771"),
	textFaint: ui.Hex("#717782"),
	iconMuted: ui.Hex("#777d87"),
	hover:     ui.Hex("#e5e7eb"),
	pressed:   ui.Hex("#d9dde4"),

	track:     ui.Hex("#e5e7eb"),
	tabActive: ui.Hex("#f7f8fa"),
	tabSep:    ui.Hex("#d0d4db"),
	tileRim:   ui.Hex("#d9dce1"),

	busy:      ui.Hex("#348753"),
	attention: ui.Hex("#b57b25"),
	exited:    ui.Hex("#858b95"),

	panel:       ui.Hex("#f7f8fa"),
	panelBorder: ui.Hex("#d9dce1"),
	panelSel:    ui.Hex("#d4e2fa"),
	backdrop:    ui.RGBA(24, 26, 30, 0.14),
}

var darkColors = colors{
	background: ui.Hex("#252628"),
	accent:     ui.Hex("#6195ef"),

	card:              ui.Hex("#1e1f21"),
	cardFocused:       ui.Hex("#191a1c"),
	cardBorder:        ui.Hex("#2c2e31"),
	cardBorderFocused: ui.Hex("#3b3e43"),
	shadow:            ui.RGBA(12, 14, 18, 0.05),
	shadowFocused:     ui.RGBA(12, 14, 18, 0.10),

	text:      ui.Hex("#d7dae0"),
	textMuted: ui.Hex("#a3a8b2"),
	textFaint: ui.Hex("#9399a4"),
	iconMuted: ui.Hex("#8c919b"),
	hover:     ui.Hex("#292b2f"),
	pressed:   ui.Hex("#32353b"),

	track:     ui.Hex("#222326"),
	tabActive: ui.Hex("#252628"),
	tabSep:    ui.Hex("#36383c"),
	tileRim:   ui.Hex("#454950"),

	busy:      ui.Hex("#73bd91"),
	attention: ui.Hex("#d8b16b"),
	exited:    ui.Hex("#737983"),

	panel:       ui.Hex("#191a1c"),
	panelBorder: ui.Hex("#303236"),
	panelSel:    ui.Hex("#2d4672"),
	backdrop:    ui.RGBA(10, 12, 16, 0.40),
}

func colorsOf(c *ui.Context) *colors {
	if c.Theme().Dark {
		return &darkColors
	}
	return &lightColors
}

// applyUITheme keeps built-in controls in step with the app's surfaces.
func applyUITheme(c *ui.Context, k *colors) {
	theme := *c.Theme()
	theme.Font, theme.Radius = uiFontFamily, 6
	theme.Background = k.background
	theme.Surface, theme.SurfaceHover, theme.SurfacePressed = k.tabActive, k.hover, k.pressed
	theme.Border, theme.Text, theme.TextMuted = k.panelBorder, k.text, k.textMuted
	theme.Accent, theme.AccentHover, theme.AccentPressed = k.accent, k.accent.Mix(k.text, 0.12), k.accent.Mix(k.background, 0.15)
	theme.AccentText = k.background
	theme.Selection, theme.Focus = k.panelSel, k.accent.Alpha(0.7)
	theme.Scrollbar = k.iconMuted.Alpha(0.5)
	theme.Inverse, theme.InverseText = k.pressed, k.text
	c.SetTheme(&theme)
}

//go:embed assets/fonts/JetBrainsMono-Regular.ttf
var fontRegular []byte

//go:embed assets/fonts/Inter-Regular.otf
var uiFontRegular []byte

//go:embed assets/fonts/Inter-SemiBold.otf
var uiFontSemibold []byte

const uiFontFamily = "Inter, sans-serif"

//go:embed assets/fonts/JetBrainsMono-Bold.ttf
var fontBold []byte

//go:embed assets/fonts/JetBrainsMono-Italic.ttf
var fontItalic []byte

//go:embed assets/fonts/JetBrainsMono-BoldItalic.ttf
var fontBoldItalic []byte

func registerFonts() {
	for _, f := range [][]byte{uiFontRegular, uiFontSemibold} {
		if err := ui.RegisterFont(f, "Inter"); err != nil {
			panic(err)
		}
	}
	for _, f := range [][]byte{fontRegular, fontBold, fontItalic, fontBoldItalic} {
		ui.RegisterFont(f, "JetBrains Mono")
	}
}

var termFont = terminal.Font{Family: terminalFontFamily(defaultFontFamily), Size: 11.6, LineHeight: 1.0}

// the default terminal colors share the workspace surfaces.
var lightTerm = &terminal.Theme{
	Foreground: ui.Hex("#2a2d31"),
	Background: ui.Hex("#ffffff"),
	Cursor:     ui.Hex("#3a3d42"),
	Selection:  ui.RGBA(46, 111, 208, 0.2),
	Palette: [16]ui.Color{
		ui.Hex("#2a2d31"), ui.Hex("#c2465a"), ui.Hex("#4e8e5f"), ui.Hex("#b07a1e"),
		ui.Hex("#2e6fd0"), ui.Hex("#9050c8"), ui.Hex("#1e8c9c"), ui.Hex("#8a8d93"),
		ui.Hex("#6e7178"), ui.Hex("#d9566b"), ui.Hex("#5ba671"), ui.Hex("#c89026"),
		ui.Hex("#4a8fe0"), ui.Hex("#a56bd8"), ui.Hex("#2ba2b3"), ui.Hex("#b5b8be"),
	},
}

var darkTerm = &terminal.Theme{
	Foreground: ui.Hex("#d7dae0"),
	Background: ui.Hex("#191a1c"),
	Cursor:     ui.Hex("#d7dae0"),
	Selection:  ui.Hex("#2d4672"),
	Palette: [16]ui.Color{
		ui.Hex("#2c2e33"), ui.Hex("#db656c"), ui.Hex("#36b66b"), ui.Hex("#c59b48"),
		ui.Hex("#6195ef"), ui.Hex("#aa80c8"), ui.Hex("#19adb8"), ui.Hex("#cecdcf"),
		ui.Hex("#8c919b"), ui.Hex("#eb8487"), ui.Hex("#69c788"), ui.Hex("#ddbc75"),
		ui.Hex("#85aff5"), ui.Hex("#c3a1d9"), ui.Hex("#60c8cd"), ui.Hex("#eeecee"),
	},
}
