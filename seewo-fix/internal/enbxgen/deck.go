package enbxgen

import (
	"archive/zip"
	"bytes"
	"cmp"
	"fmt"
	"io"
	"maps"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// EMU per px at 96 dpi, the unit of a 希沃 page.
const emuPerPx = 9525.0

func px(emu int64) float64 { return float64(emu) / emuPerPx }

// deck is what the converter needs from a .pptx.
type deck struct {
	files             map[string][]byte
	slides            []slideRef
	cx, cy            int64
	theme             theme
	links             map[string]partLinks
	masterThemes      map[string]theme
	tableStyles       map[string]*etree.Element
	defaultTableStyle string
	// defaults for text that sets nothing: presentation defaultTextStyle
	defSz int
}

type slideRef struct {
	id   string // sldId id, recorded as SlidePptxOriginIdExtension
	part string // ppt/slides/slideN.xml
}

type theme struct {
	colors map[string]string // dk1, lt1, accent1, … → RRGGBB
	clrMap map[string]string // bg1 → lt1, tx1 → dk1, …
	minor  map[string]string // latin, ea, cs
	major  map[string]string
}

func readDeck(data []byte) (*deck, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open pptx: %w", err)
	}
	d := &deck{files: map[string][]byte{}, defSz: 1800}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		d.files[f.Name] = b
	}
	pres, err := d.xml("ppt/presentation.xml")
	if err != nil {
		return nil, err
	}
	rels := d.rels("ppt/presentation.xml")
	root := pres.Root()
	if sz := root.FindElement("./sldSz"); sz != nil {
		d.cx = attrInt(sz, "cx")
		d.cy = attrInt(sz, "cy")
	}
	for _, s := range root.FindElements("./sldIdLst/sldId") {
		rid := s.SelectAttrValue("r:id", "")
		if t, ok := rels[rid]; ok {
			d.slides = append(d.slides, slideRef{id: s.SelectAttrValue("id", ""), part: t})
		}
	}
	if r := root.FindElement("./defaultTextStyle/lvl1pPr/defRPr"); r != nil {
		if v := attrInt(r, "sz"); v > 0 {
			d.defSz = int(v)
		}
	}
	d.theme = d.masterTheme("")
	part := cmp.Or(d.relationships("ppt/presentation.xml").byType["tableStyles"], "ppt/tableStyles.xml")
	if _, found := d.files[part]; found {
		styles, err := d.xml(part)
		if err != nil {
			return nil, err
		}
		d.defaultTableStyle = styles.Root().SelectAttrValue("def", "")
		d.tableStyles = make(map[string]*etree.Element)
		for _, style := range styles.Root().SelectElements("tblStyle") {
			d.tableStyles[strings.ToUpper(style.SelectAttrValue("styleId", ""))] = style
		}
	}
	return d, nil
}

func attrInt(e *etree.Element, name string) int64 {
	v, _ := strconv.ParseInt(e.SelectAttrValue(name, "0"), 10, 64)
	return v
}

func (d *deck) xml(name string) (*etree.Document, error) {
	b, ok := d.files[name]
	if !ok {
		return nil, fmt.Errorf("%s missing", name)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(b); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return doc, nil
}

// rels maps relationship ids of a part to the parts they point at.
type partLinks struct{ byID, byType map[string]string }

func (d *deck) rels(part string) map[string]string { return d.relationships(part).byID }

func (d *deck) relationships(part string) partLinks {
	if links, ok := d.links[part]; ok {
		return links
	}
	links := partLinks{byID: map[string]string{}, byType: map[string]string{}}
	if d.links == nil {
		d.links = make(map[string]partLinks)
	}
	d.links[part] = links
	if part == "" {
		return links
	}
	dir, file := path.Split(part)
	doc, err := d.xml(dir + "_rels/" + file + ".rels")
	if err != nil {
		return links
	}
	for _, r := range doc.Root().ChildElements() {
		target := r.SelectAttrValue("Target", "")
		if target == "" || r.SelectAttrValue("TargetMode", "") == "External" {
			continue
		}
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(path.Clean(target), "/")
		} else {
			target = path.Join(dir, target)
		}
		links.byID[r.SelectAttrValue("Id", "")] = target
		links.byType[path.Base(r.SelectAttrValue("Type", ""))] = target
	}
	return links
}

func (d *deck) slideTheme(part string, root *etree.Element) theme {
	layout := d.relationships(part).byType["slideLayout"]
	master := d.relationships(layout).byType["slideMaster"]
	th := d.masterTheme(master)
	override := root.SelectElement("clrMapOvr")
	if override == nil && layout != "" {
		if doc, err := d.xml(layout); err == nil {
			override = doc.Root().SelectElement("clrMapOvr")
		}
	}
	if override != nil {
		if mapping := override.SelectElement("overrideClrMapping"); mapping != nil {
			th.clrMap = maps.Clone(th.clrMap)
			for _, a := range mapping.Attr {
				th.clrMap[a.Key] = a.Value
			}
		}
	}
	return th
}

func (d *deck) masterTheme(master string) theme {
	if th, ok := d.masterThemes[master]; ok {
		return th
	}
	if d.masterThemes == nil {
		d.masterThemes = map[string]theme{}
	}
	th := d.readTheme(master)
	d.masterThemes[master] = th
	return th
}

func (d *deck) readTheme(master string) theme {
	th := theme{colors: map[string]string{}, minor: map[string]string{}, major: map[string]string{}, clrMap: map[string]string{"bg1": "lt1", "tx1": "dk1", "bg2": "lt2", "tx2": "dk2"}}
	if master != "" {
		if doc, err := d.xml(master); err == nil {
			if cm := doc.Root().SelectElement("clrMap"); cm != nil {
				for _, a := range cm.Attr {
					th.clrMap[a.Key] = a.Value
				}
			}
		}
	}
	themePart := cmp.Or(d.relationships(master).byType["theme"], d.relationships("ppt/presentation.xml").byType["theme"], "ppt/theme/theme1.xml")
	doc, err := d.xml(themePart)
	if err != nil {
		return th
	}
	if cs := doc.Root().FindElement(".//clrScheme"); cs != nil {
		for _, c := range cs.ChildElements() {
			if s := c.FindElement("./srgbClr"); s != nil {
				th.colors[c.Tag] = strings.ToUpper(s.SelectAttrValue("val", "000000"))
			} else if s := c.FindElement("./sysClr"); s != nil {
				th.colors[c.Tag] = strings.ToUpper(s.SelectAttrValue("lastClr", "000000"))
			}
		}
	}
	for which, m := range map[string]map[string]string{"minorFont": th.minor, "majorFont": th.major} {
		if f := doc.Root().FindElement(".//fontScheme/" + which); f != nil {
			for _, k := range []string{"latin", "ea", "cs"} {
				if e := f.FindElement("./" + k); e != nil {
					m[k] = e.SelectAttrValue("typeface", "")
				}
			}
		}
	}
	return th
}

// font resolves theme references such as +mn-lt and +mn-ea.
func (t theme) font(name string) string {
	switch name {
	case "+mn-lt":
		return t.minor["latin"]
	case "+mn-ea":
		return t.minor["ea"]
	case "+mn-cs":
		return t.minor["cs"]
	case "+mj-lt":
		return t.major["latin"]
	case "+mj-ea":
		return t.major["ea"]
	case "+mj-cs":
		return t.major["cs"]
	}
	return name
}

var presetColors = map[string]string{"black": "000000", "white": "FFFFFF", "red": "FF0000", "green": "008000",
	"blue": "0000FF", "yellow": "FFFF00", "gray": "808080", "grey": "808080"}

// color reads the colour child of a fill-like element as 希沃's #AARRGGBB.
// ok is false when the element names no colour.
func (t theme) color(parent *etree.Element) (string, bool) {
	if parent == nil {
		return "", false
	}
	for _, c := range parent.ChildElements() {
		rgb := ""
		switch c.Tag {
		case "srgbClr":
			rgb = strings.ToUpper(c.SelectAttrValue("val", "000000"))
		case "schemeClr":
			v := c.SelectAttrValue("val", "")
			if m, ok := t.clrMap[v]; ok {
				v = m
			}
			rgb = t.colors[v]
		case "prstClr":
			rgb = presetColors[c.SelectAttrValue("val", "")]
		case "sysClr":
			rgb = strings.ToUpper(c.SelectAttrValue("lastClr", "000000"))
		default:
			continue
		}
		if rgb == "" {
			rgb = "000000"
		}
		return applyMods(rgb, c), true
	}
	return "", false
}

func applyMods(rgb string, c *etree.Element) string {
	v, _ := strconv.ParseUint(rgb, 16, 32)
	r, g, b := float64(v>>16&255)/255, float64(v>>8&255)/255, float64(v&255)/255
	alpha := 1.0
	for _, m := range c.ChildElements() {
		val := float64(attrInt(m, "val")) / 100000
		switch m.Tag {
		case "alpha":
			alpha = val
		case "lumMod", "lumOff":
			h, s, l := rgbToHSL(r, g, b)
			if m.Tag == "lumMod" {
				l *= val
			} else {
				l += val
			}
			r, g, b = hslToRGB(h, s, math.Max(0, math.Min(1, l)))
		case "tint":
			r, g, b = 1-(1-r)*val, 1-(1-g)*val, 1-(1-b)*val
		case "shade":
			r, g, b = r*val, g*val, b*val
		}
	}
	to := func(f float64) int { return int(math.Round(math.Max(0, math.Min(1, f)) * 255)) }
	return fmt.Sprintf("#%02X%02X%02X%02X", to(alpha), to(r), to(g), to(b))
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, l
}

func hslToRGB(h, s, l float64) (float64, float64, float64) {
	if s == 0 {
		return l, l, l
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	f := func(t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return f(h + 1.0/3), f(h), f(h - 1.0/3)
}
