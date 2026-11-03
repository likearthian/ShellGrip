package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/font/sfnt"
)

const defaultFontFamily = "JetBrains Mono"

func (a *App) fontSelector(c *ui.Context, k *colors) {
	selectFont := ui.SelectBase(c, &a.settingsFont)
	selectFont.Trigger.Label("Choose terminal font").Height(32).Padding(6, 10).Gap(8).AlignItems(ui.Center).
		Radius(6).Background(k.hover).Border(1, k.panelBorder).Children(func() {
		ui.Text(c, a.settingsFont).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(13).TextColor(k.text)
		ui.Icon(c, icon("chevron-down")).Size(14, 14).TextColor(k.iconMuted)
	})
	selectFont.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(8).Background(k.panel).Border(1, k.panelBorder).Shadow(0, 8, 20, 0, k.shadowFocused)
		ui.Scroll(c).Height(float32(min(len(a.fontFamilies)*30, 210))).Children(func() {
			for _, family := range a.fontFamilies {
				item := selectFont.Item(family).Label(family).Height(30).Shrink(0).Padding(6, 10).Radius(5)
				if item.Highlighted() {
					item.Background(k.panelSel)
					if a.fontHighlight != family {
						a.fontHighlight = family
						item.ScrollIntoView()
					}
				}
				item.Children(func() {
					ui.Text(c, family).FontSize(13).TextColor(k.text).SingleLine()
				})
			}
		})
	})
}

func terminalFontFamily(family string) string {
	if family == "" || family == defaultFontFamily {
		return defaultFontFamily + ", monospace"
	}
	return family + ", " + defaultFontFamily + ", monospace"
}

func discoverFontFamilies() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		if root := os.Getenv("SystemRoot"); root != "" {
			dirs = append(dirs, filepath.Join(root, "Fonts"))
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Microsoft", "Windows", "Fonts"))
		}
	case "darwin":
		dirs = []string{"/System/Library/Fonts", "/Library/Fonts", filepath.Join(home, "Library", "Fonts")}
	default:
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		dirs = []string{filepath.Join(data, "fonts"), filepath.Join(home, ".fonts")}
		dataDirs := os.Getenv("XDG_DATA_DIRS")
		if dataDirs == "" {
			dataDirs = "/usr/local/share:/usr/share"
		}
		for _, dir := range filepath.SplitList(dataDirs) {
			dirs = append(dirs, filepath.Join(dir, "fonts"))
		}
	}
	return scanFontFamilies(dirs)
}

func scanFontFamilies(dirs []string) []string {
	families := map[string]string{strings.ToLower(defaultFontFamily): defaultFontFamily}
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".ttf", ".otf", ".ttc", ".otc":
			default:
				return nil
			}
			for _, name := range monospaceFamilies(path) {
				families[strings.ToLower(name)] = name
			}
			return nil
		})
	}
	result := []string{defaultFontFamily}
	for key, name := range families {
		if key != strings.ToLower(defaultFontFamily) {
			result = append(result, name)
		}
	}
	slices.SortFunc(result[1:], func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return result
}

func monospaceFamilies(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	collection, err := sfnt.ParseCollectionReaderAt(file)
	if err != nil {
		return nil
	}
	var names []string
	for i := 0; i < collection.NumFonts(); i++ {
		font, err := collection.Font(i)
		if err != nil || font.PostTable() == nil || !font.PostTable().IsFixedPitch {
			continue
		}
		var buffer sfnt.Buffer
		name, _ := font.Name(&buffer, sfnt.NameIDTypographicFamily)
		if name == "" {
			name, _ = font.Name(&buffer, sfnt.NameIDFamily)
		}
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}
