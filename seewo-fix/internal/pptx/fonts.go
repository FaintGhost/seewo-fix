package pptx

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Measurer gives the advance width of a string set in one font, in EMU.
// ok is false when the font is not available: callers must then not act on a
// guess.
type Measurer interface {
	Width(text, typeface string, bold, italic bool, sizeEMU float64) (w float64, ok bool)
}

// SystemFonts measures with the fonts installed on this machine, which are the
// fonts PowerPoint lays the deck out with here.
var SystemFonts Measurer = &sysFonts{}

type fontRef struct {
	path  string
	index int
}

type sysFonts struct {
	once  sync.Once
	index map[string]fontRef // "family|bold|italic", lower case
	mu    sync.Mutex
	open  map[fontRef]*sfnt.Font
}

func fontDirs() []string {
	dirs := []string{"/System/Library/Fonts", "/System/Library/Fonts/Supplemental", "/Library/Fonts"}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(h, "Library", "Fonts"))
	}
	if w := os.Getenv("WINDIR"); w != "" {
		dirs = append(dirs, filepath.Join(w, "Fonts"))
	}
	// fonts Microsoft Office ships inside its apps (Cooper Black, …), which
	// PowerPoint lays decks out with
	if m, err := filepath.Glob("/Applications/Microsoft *.app/Contents/Resources/DFonts"); err == nil {
		dirs = append(dirs, m...)
	}
	// fonts macOS downloads on demand (Kaiti SC, Songti SC, …)
	for _, pat := range []string{
		"/System/Library/AssetsV2/com_apple_MobileAsset_Font*/*/AssetData",
		"/System/Library/Assets*/com_apple_MobileAsset_Font*/*/AssetData",
	} {
		if m, err := filepath.Glob(pat); err == nil {
			dirs = append(dirs, m...)
		}
	}
	return dirs
}

func styleKey(family string, bold, italic bool) string {
	k := strings.ToLower(strings.TrimSpace(family)) + "|"
	if bold {
		k += "b"
	}
	k += "|"
	if italic {
		k += "i"
	}
	return k
}

func (s *sysFonts) build() {
	s.index = map[string]fontRef{}
	s.open = map[fontRef]*sfnt.Font{}
	var buf sfnt.Buffer
	add := func(path string, i int, f *sfnt.Font) {
		sub, _ := f.Name(&buf, sfnt.NameIDSubfamily)
		sub = strings.ToLower(sub)
		bold := strings.Contains(sub, "bold")
		italic := strings.Contains(sub, "italic") || strings.Contains(sub, "oblique")
		for _, id := range []sfnt.NameID{sfnt.NameIDFamily, sfnt.NameIDTypographicFamily} {
			if fam, err := f.Name(&buf, id); err == nil && fam != "" {
				k := styleKey(fam, bold, italic)
				if _, dup := s.index[k]; !dup {
					s.index[k] = fontRef{path, i}
				}
			}
		}
	}
	for _, dir := range fontDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".ttf" && ext != ".otf" && ext != ".ttc" {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if ext == ".ttc" {
				c, err := sfnt.ParseCollection(data)
				if err != nil {
					continue
				}
				for i := 0; i < c.NumFonts(); i++ {
					if f, err := c.Font(i); err == nil {
						add(path, i, f)
					}
				}
				continue
			}
			if f, err := sfnt.Parse(data); err == nil {
				add(path, 0, f)
			}
		}
	}
}

func (s *sysFonts) font(typeface string, bold, italic bool) *sfnt.Font {
	s.once.Do(s.build)
	ref, ok := s.index[styleKey(typeface, bold, italic)]
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.open[ref]; ok {
		return f
	}
	data, err := os.ReadFile(ref.path)
	if err != nil {
		return nil
	}
	var f *sfnt.Font
	if strings.EqualFold(filepath.Ext(ref.path), ".ttc") {
		c, err := sfnt.ParseCollection(data)
		if err != nil {
			return nil
		}
		f, err = c.Font(ref.index)
		if err != nil {
			return nil
		}
	} else if f, err = sfnt.Parse(data); err != nil {
		return nil
	}
	s.open[ref] = f
	return f
}

func (s *sysFonts) Width(text, typeface string, bold, italic bool, sizeEMU float64) (float64, bool) {
	f := s.font(typeface, bold, italic)
	if f == nil {
		return 0, false
	}
	var buf sfnt.Buffer
	upm := fixed.Int26_6(f.UnitsPerEm())
	total := 0.0
	for _, r := range text {
		gi, err := f.GlyphIndex(&buf, r)
		if err != nil || gi == 0 {
			return 0, false
		}
		adv, err := f.GlyphAdvance(&buf, gi, upm, font.HintingNone)
		if err != nil {
			return 0, false
		}
		total += float64(adv) / float64(upm)
	}
	return total * sizeEMU, true
}
