package enbxgen

import (
	"cmp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/beevik/etree"
)

// run is one stretch of text in one font, as 希沃 stores it.
type run struct {
	text      string
	sizePt    float64
	bold      bool
	italic    bool
	underline bool
	font      string // as written in the deck (楷体, Times New Roman)
	color     string // #AARRGGBB
	highlight string // #AARRGGBB, "" for none
	baseline  int
}

type para struct {
	runs    []run
	def     run // properties for an empty paragraph / DefaultRunProperty
	align   string
	lnPct   float64 // line spacing, 1 = single
	lnPts   float64 // exact line spacing in pt, 0 = none
	marL    float64 // px
	indent  float64 // px
	spcBef  float64 // px
	spcAft  float64 // px
	lines   []lineSpan
	justify bool
	bullet  bool
	buChar  string
	buFont  string
	warning string
}

// lineHeight is PowerPoint's line box in px for this paragraph. Explicit
// point spacing overrides the normal 1.2-em box.
func (p para) lineHeight() float64 {
	if p.lnPts > 0 {
		return p.lnPts * 4 / 3
	}
	size := p.def.sizePt
	for _, r := range p.runs {
		size = max(size, r.sizePt)
	}
	return size * 4 / 3 * 1.2 * p.lnPct
}

type lineSpan struct{ start, length int } // in UTF-16 units of the paragraph text

type body struct {
	paras                  []para
	lIns, tIns, rIns, bIns int64
	wrap                   bool
	anchor                 string
}

func isCJK(r rune) bool {
	return r >= 0x2E80 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0xFF00 && r <= 0xFFEF ||
		r >= 0x3000 && r <= 0x303F || r >= 0x20000 && r <= 0x2FFFF
}

// runProps reads a:rPr (or defRPr/endParaRPr) over a base.
func (c *conv) runProps(rPr *etree.Element, base run) (run, string, string) {
	r := base
	latin, ea := "", ""
	if rPr == nil {
		return r, latin, ea
	}
	if v := attrInt(rPr, "sz"); v > 0 {
		r.sizePt = float64(v) / 100
	}
	if a := rPr.SelectAttr("b"); a != nil {
		r.bold = a.Value == "1" || a.Value == "true"
	}
	if a := rPr.SelectAttr("i"); a != nil {
		r.italic = a.Value == "1" || a.Value == "true"
	}
	if a := rPr.SelectAttr("u"); a != nil {
		r.underline = a.Value != "none"
	}
	if a := rPr.SelectAttr("baseline"); a != nil {
		r.baseline = int(attrInt(rPr, "baseline"))
	}
	if col, ok := c.d.theme.color(rPr.FindElement("./solidFill")); ok {
		r.color = col
	}
	if h, ok := c.d.theme.color(rPr.FindElement("./highlight")); ok && c.opt.Highlights {
		r.highlight = h
	}
	if e := rPr.FindElement("./latin"); e != nil {
		latin = c.d.theme.font(e.SelectAttrValue("typeface", ""))
	}
	if e := rPr.FindElement("./ea"); e != nil {
		ea = c.d.theme.font(e.SelectAttrValue("typeface", ""))
	}
	return r, latin, ea
}

// readBody turns a p:txBody into paragraphs of 希沃 runs, line breaks
// included, for a box whose text area is availPx wide.
func (c *conv) readBody(tb *etree.Element, availEMU int64, fontColor string) body {
	return c.readStyledBody(tb, availEMU, run{color: fontColor})
}

func (c *conv) readStyledBody(tb *etree.Element, availEMU int64, defaults run) body {
	b := body{lIns: 91440, tIns: 45720, rIns: 91440, bIns: 45720, wrap: true, anchor: "t"}
	if bp := tb.FindElement("./bodyPr"); bp != nil {
		for k, p := range map[string]*int64{"lIns": &b.lIns, "tIns": &b.tIns, "rIns": &b.rIns, "bIns": &b.bIns} {
			if a := bp.SelectAttr(k); a != nil {
				*p = attrInt(bp, k)
			}
		}
		b.wrap = bp.SelectAttrValue("wrap", "square") != "none"
		b.anchor = bp.SelectAttrValue("anchor", "t")
	}
	if c.cellInsets != nil {
		b.lIns, b.tIns, b.rIns, b.bIns = c.cellInsets[0], c.cellInsets[1], c.cellInsets[2], c.cellInsets[3]
	}
	avail := float64(availEMU - b.lIns - b.rIns)
	base := defaults
	base.sizePt = float64(c.d.defSz) / 100
	base.color = cmp.Or(base.color, "#FF"+cmp.Or(c.d.theme.colors[c.d.theme.clrMap["tx1"]], "000000"))
	ps := tb.FindElements("./p")
	for pi, p := range ps {
		pr := para{align: "Left", lnPct: 1}
		ppr := p.FindElement("./pPr")
		if ppr != nil {
			switch ppr.SelectAttrValue("algn", "l") {
			case "ctr":
				pr.align = "Center"
			case "r":
				pr.align = "Right"
			case "just":
				pr.justify = true
			}
			pr.marL = px(attrInt(ppr, "marL"))
			pr.indent = px(attrInt(ppr, "indent"))
			if v := ppr.FindElement("./lnSpc/spcPct"); v != nil {
				pr.lnPct = float64(attrInt(v, "val")) / 100000
			}
			if v := ppr.FindElement("./lnSpc/spcPts"); v != nil {
				pr.lnPts = float64(attrInt(v, "val")) / 100
			}
			if v := ppr.FindElement("./spcBef/spcPts"); v != nil {
				pr.spcBef = float64(attrInt(v, "val")) / 100 * 4 / 3
			}
			if v := ppr.FindElement("./spcAft/spcPts"); v != nil {
				pr.spcAft = float64(attrInt(v, "val")) / 100 * 4 / 3
			}
			if bc := ppr.FindElement("./buChar"); bc != nil {
				pr.bullet = true
				pr.buChar = bc.SelectAttrValue("char", "•")
				if bf := ppr.FindElement("./buFont"); bf != nil {
					pr.buFont = bf.SelectAttrValue("typeface", "")
				}
			}
			if ppr.FindElement("./buAutoNum") != nil {
				pr.warning = "自动编号"
			}
		}
		level := int64(0)
		if ppr != nil {
			level = attrInt(ppr, "lvl")
		}
		paragraphDefault, latinDefault, eaDefault := c.runProps(tb.FindElement("./lstStyle/lvl"+strconv.FormatInt(level+1, 10)+"pPr/defRPr"), base)
		if ppr != nil {
			var latin, ea string
			paragraphDefault, latin, ea = c.runProps(ppr.SelectElement("defRPr"), paragraphDefault)
			latinDefault = cmp.Or(latin, latinDefault)
			eaDefault = cmp.Or(ea, eaDefault)
		}
		type item struct {
			r         run
			latin, ea string
			text      string
		}
		var items []item
		for _, ch := range p.ChildElements() {
			switch ch.Tag {
			case "r", "fld":
				r, latin, ea := c.runProps(ch.FindElement("./rPr"), paragraphDefault)
				t := ""
				if te := ch.FindElement("./t"); te != nil {
					t = te.Text()
				}
				items = append(items, item{r: r, latin: latin, ea: ea, text: t})
			case "br":
				r, latin, ea := c.runProps(ch.FindElement("./rPr"), paragraphDefault)
				items = append(items, item{r: r, latin: latin, ea: ea, text: "\v"})
			}
		}
		end, el, ee := c.runProps(p.FindElement("./endParaRPr"), paragraphDefault)
		el, ee = cmp.Or(el, latinDefault), cmp.Or(ee, eaDefault)
		// split into 希沃 runs: one font each
		for _, it := range items {
			latin := cmp.Or(it.latin, latinDefault)
			if latin == "" {
				latin = c.d.theme.minor["latin"]
			}
			ea := cmp.Or(it.ea, eaDefault)
			if ea == "" {
				ea = c.d.theme.minor["ea"]
			}
			if ea == "" {
				ea = latin
			}
			var cur strings.Builder
			curCJK := false
			flush := func() {
				if cur.Len() == 0 {
					return
				}
				r := it.r
				r.text = cur.String()
				r.font = latin
				if curCJK {
					r.font = ea
				}
				pr.runs = append(pr.runs, r)
				cur.Reset()
			}
			for i, ch := range it.text {
				cjk := isCJK(ch)
				if ch == ' ' && i > 0 {
					cjk = curCJK // spaces stay with what they follow
				}
				if i > 0 && cjk != curCJK {
					flush()
				}
				curCJK = cjk
				cur.WriteRune(ch)
			}
			flush()
		}
		pr.def = end
		if len(pr.runs) > 0 {
			pr.def = pr.runs[0]
			pr.def.text = ""
		} else {
			pr.def.font = el
			if pr.def.font == "" {
				pr.def.font = ee
			}
			if pr.def.font == "" {
				pr.def.font = c.d.theme.minor["latin"]
			}
		}
		// paragraph separator, as 希沃 writes it: on the last run
		if pi < len(ps)-1 {
			if len(pr.runs) == 0 {
				r := pr.def
				r.text = "\r\n"
				pr.runs = append(pr.runs, r)
			} else {
				pr.runs[len(pr.runs)-1].text += "\r\n"
			}
		}
		// Geometry and measured advances are in EMU; paragraph properties
		// are stored in px for the ENBX writer.
		availPara := avail - pr.marL*emuPerPx
		firstIndent := pr.indent * emuPerPx
		if pr.bullet {
			firstIndent = 0 // the bullet hangs in the indent; text starts at marL
		}
		pr.lines = c.breakLines(pr.runs, availPara, b.wrap, firstIndent)
		b.paras = append(b.paras, pr)
	}
	return b
}

func u16len(s string) int { return len(utf16.Encode([]rune(s))) }

// noLineStart are characters a line must not begin with (kinsoku).
const noLineStart = "，。、；：！？）》」』】〕〉,.;:!?)]}%’”"

// breakLines finds where PowerPoint ends each line of a paragraph.
func (c *conv) breakLines(runs []run, avail float64, wrap bool, indent float64) []lineSpan {
	type ch struct {
		r rune
		w float64
	}
	var chars []ch
	for _, r := range runs {
		size := r.sizePt * 12700
		for _, x := range r.text {
			if x == '\r' || x == '\n' {
				chars = append(chars, ch{x, 0})
				continue
			}
			w, ok := c.width(string(x), r, size)
			if !ok {
				if isCJK(x) {
					w = size
				} else {
					w = size / 2
					c.warnFont(r.font)
				}
			}
			chars = append(chars, ch{x, w})
		}
	}
	var lines []lineSpan
	pos16 := 0 // UTF-16 offset of the current line start
	start := 0 // index into chars
	first := true
	for start < len(chars) {
		limit := avail
		if first {
			limit -= indent
		}
		x := 0.0
		brk := -1 // index after which a break is allowed
		end := len(chars)
		for i := start; i < len(chars); i++ {
			r := chars[i].r
			if r == '\v' {
				end = i + 1
				break
			}
			if r == ' ' {
				brk = i
				x += chars[i].w
				continue
			}
			if isCJK(r) && i > start && !strings.ContainsRune(noLineStart, r) {
				brk = i - 1
			}
			x += chars[i].w
			// PowerPoint lets closing CJK punctuation and Latin periods or
			// commas hang past the margin. Moving the whole final word
			// instead changes table heights and annotated paragraph lines.
			if strings.ContainsRune(noLineStart, r) && x-chars[i].w <= limit &&
				(strings.ContainsRune(".,", r) || isCJK(r) || i > start && isCJK(chars[i-1].r)) {
				continue
			}
			if wrap && x > limit && i > start {
				if brk >= start {
					end = brk + 1
				} else {
					end = i
				}
				// trailing spaces hang on this line
				for end < len(chars) && chars[end].r == ' ' {
					end++
				}
				// a line may not start with closing punctuation
				for end < len(chars) && strings.ContainsRune(noLineStart, chars[end].r) && end-1 > start {
					end--
				}
				break
			}
		}
		// the paragraph's own \r\n stays on its last line
		for end < len(chars) && (chars[end].r == '\r' || chars[end].r == '\n') {
			end++
		}
		n, sep := 0, 0
		for _, q := range chars[start:end] {
			if q.r == '\r' || q.r == '\n' {
				sep++ // the paragraph separator is not part of a line
				continue
			}
			n += u16len(string(q.r))
		}
		lines = append(lines, lineSpan{pos16, n})
		pos16 += n + sep
		start = end
		first = false
	}
	if len(lines) == 0 {
		lines = []lineSpan{{0, 0}}
	}
	return lines
}

// lineSpacing is 希沃's LineSpacing for a paragraph, from its first run's
// font: c·p − 8.97043, with c per font, measured from 希沃's own output.
func lineSpacing(font string, pct float64) (float64, bool) {
	base, ok := lineBase[font]
	if !ok {
		return 0, false
	}
	return (base+lineK)*pct - lineK, true
}

// renderLineSpacing follows the shipped Mac renderer's text-height and
// baseline tables. Unknown fonts (including Kaiti SC) use 1.18 em height
// and the Microsoft YaHei baseline. Mixing them with Times New Roman
// therefore makes the natural line taller than either font's own box.
func (c *conv) renderLineSpacing(p para) float64 {
	runs := p.runs
	if len(runs) == 0 {
		runs = []run{p.def}
	}
	var ascent, descent, dominantSize, dominantHeight float64
	for _, r := range runs {
		height, baseline := 1.18, 1.05810546875
		switch c.mapFont(r).font {
		case "Times New Roman":
			height, baseline = 1.14990234375, 0.912353515625
		case "楷体", "KaiTi":
			height, baseline = 1.140625, 0.9296875
		}
		size := r.sizePt * 4 / 3
		ascent = max(ascent, size*baseline)
		descent = max(descent, size*(height-baseline))
		// The renderer uses the first largest run to compute spacing.
		if size > dominantSize {
			dominantSize, dominantHeight = size, size*height
		}
	}
	if dominantHeight == 0 {
		return 1
	}
	adjustment := p.lineHeight() - ascent - descent
	if adjustment < 0 {
		// The renderer retains 20% of negative leading on the first line
		// of each paragraph. Compensate so numbered lists do not accrue
		// about 0.8 px per row at 32 pt (verified on page 30).
		adjustment /= 0.8
	}
	return 1 + 10*adjustment/dominantHeight
}

const lineK = 8.970432

var lineBase = map[string]float64{
	"Times New Roman": 1.4808900212314229,
	"楷体":              1.5661369863013703,
	"Cooper Black":    0.13151905290418087,
}

// macFonts are the fonts PowerPoint on a Mac lays Windows Chinese fonts out
// with, when the deck's own font is not installed.
var macFonts = map[string][]string{
	"楷体":              {"Kaiti SC", "STKaiti"},
	"KaiTi":           {"Kaiti SC", "STKaiti"},
	"楷体_GB2312":       {"Kaiti SC", "STKaiti"},
	"宋体":              {"Songti SC", "STSong"},
	"SimSun":          {"Songti SC", "STSong"},
	"黑体":              {"Heiti SC", "STHeiti"},
	"SimHei":          {"Heiti SC", "STHeiti"},
	"微软雅黑":            {"PingFang SC"},
	"Microsoft YaHei": {"PingFang SC"},
}

// width measures one character the way PowerPoint on this machine does:
// the run's font, its regular face, then its Mac stand-in.
func (c *conv) width(s string, r run, size float64) (float64, bool) {
	for _, face := range append([]string{r.font}, macFonts[r.font]...) {
		if w, ok := c.m.Width(s, face, r.bold, r.italic, size); ok {
			return w, true
		}
		if w, ok := c.m.Width(s, face, false, false, size); ok {
			return w, true
		}
	}
	return 0, false
}

type band struct {
	color  string
	rects  [][4]float64 // x, y, w, h in px, relative to the text box
	colors []string     // per rect, when they differ
}

// highlightBand turns a body highlighted throughout in one colour into one
// band per line, as PowerPoint draws it, and takes the highlight off the
// runs: 希沃 paints a run's background as high as that run's own font, so
// mixed Chinese and Latin text gets a ragged band.
func (c *conv) highlightBand(b *body, boxEMU int64) (band, bool) {
	color := ""
	for _, p := range b.paras {
		for _, r := range p.runs {
			if strings.TrimSpace(strings.Trim(r.text, "\r\n")) == "" {
				continue
			}
			if r.highlight == "" || color != "" && r.highlight != color {
				return band{}, false
			}
			color = r.highlight
		}
	}
	if color == "" {
		return band{}, false
	}
	avail := px(boxEMU - b.lIns - b.rIns)
	type ln struct{ w, h float64 }
	var lines []ln
	var aligns []string
	for _, p := range b.paras {
		// the text of each line, measured as PowerPoint does
		text := ""
		var props []run
		for _, r := range p.runs {
			for _, ch := range r.text {
				text += string(ch)
				props = append(props, r)
			}
		}
		u16 := 0
		idx := 0
		chars := []rune(text)
		for _, l := range p.lines {
			w, maxSize := 0.0, 0.0
			for u16 < l.start+l.length && idx < len(chars) {
				ch := chars[idx]
				r := props[idx]
				size := r.sizePt * 12700
				if ch != '\r' && ch != '\n' {
					cw, ok := c.width(string(ch), r, size)
					if !ok {
						cw = size / 2
						if isCJK(ch) {
							cw = size
						}
					}
					w += cw
				}
				if r.sizePt > maxSize {
					maxSize = r.sizePt
				}
				u16 += u16len(string(ch))
				idx++
			}
			// trailing spaces are not highlighted
			for k := idx - 1; k >= 0 && chars[k] == ' '; k-- {
				cw, _ := c.width(" ", props[k], props[k].sizePt*12700)
				w -= cw
			}
			for idx < len(chars) && (chars[idx] == '\r' || chars[idx] == '\n') {
				u16++
				idx++
			}
			// 希沃 paints a Chinese run's background from 0.066 em below the
			// content top, 1.29 em high, and a Latin run's 0.19 em lower: the
			// band covers both (measured at 32 pt on page 3).
			h := maxSize * 4 / 3 * bandEm * p.lnPct
			lines = append(lines, ln{px(int64(w)), h})
			aligns = append(aligns, p.align)
		}
		for i := range p.runs {
			p.runs[i].highlight = ""
		}
	}
	for i := range b.paras {
		b.paras[i].def.highlight = ""
		for j := range b.paras[i].runs {
			b.paras[i].runs[j].highlight = ""
		}
	}
	bd := band{color: color}
	y := px(b.tIns)
	if len(b.paras) > 0 && len(b.paras[0].runs) > 0 {
		y += b.paras[0].runs[0].sizePt * 4 / 3 * bandTopEm
	}
	for i, l := range lines {
		x := px(b.lIns)
		switch aligns[i] {
		case "Center":
			x += (avail - l.w) / 2
		case "Right":
			x += avail - l.w
		}
		bd.rects = append(bd.rects, [4]float64{x, y, l.w, l.h})
		y += l.h
	}
	return bd, true
}

const (
	bandEm    = 1.46
	bandTopEm = 0.066
)

// partialBands handles a one-line box where a highlighted stretch mixes
// Chinese and Latin text: 希沃 would paint the two fonts' backgrounds at
// different heights, so the stretch gets one band, measured as PowerPoint
// lays it out, and its runs lose their background. Stretches in one font
// keep their run background, which 希沃 draws evenly.
func (c *conv) partialBands(b *body, boxEMU int64) (band, bool) {
	if len(b.paras) != 1 || len(b.paras[0].lines) != 1 {
		return band{}, false
	}
	p := &b.paras[0]
	type ch struct {
		w     float64
		ri    int
		space bool
	}
	var chars []ch
	total := 0.0
	maxSize := 0.0
	for ri, r := range p.runs {
		size := r.sizePt * 12700
		if r.sizePt > maxSize {
			maxSize = r.sizePt
		}
		for _, x := range r.text {
			if x == '\r' || x == '\n' {
				continue
			}
			w, ok := c.width(string(x), r, size)
			if !ok {
				w = size / 2
				if isCJK(x) {
					w = size
				}
			}
			chars = append(chars, ch{w, ri, x == ' '})
			total += w
		}
	}
	avail := float64(boxEMU - b.lIns - b.rIns)
	x0 := float64(b.lIns)
	switch p.align {
	case "Center":
		x0 += (avail - total) / 2
	case "Right":
		x0 += avail - total
	}
	var bd band
	clear := map[int]bool{}
	pos := x0
	for i := 0; i < len(chars); {
		r := p.runs[chars[i].ri]
		if r.highlight == "" {
			pos += chars[i].w
			i++
			continue
		}
		j, w := i, 0.0
		fonts := map[string]bool{}
		for j < len(chars) && p.runs[chars[j].ri].highlight == r.highlight {
			if !chars[j].space {
				fonts[p.runs[chars[j].ri].font] = true
			}
			w += chars[j].w
			j++
		}
		// trailing spaces are not highlighted
		for k := j - 1; k > i && chars[k].space; k-- {
			w -= chars[k].w
		}
		if len(fonts) > 1 {
			bd.rects = append(bd.rects, [4]float64{px(int64(pos)), px(b.tIns) + maxSize*4/3*bandTopEm, px(int64(w)), maxSize * 4 / 3 * bandEm * p.lnPct})
			bd.colors = append(bd.colors, r.highlight)
			for k := i; k < j; k++ {
				clear[chars[k].ri] = true
			}
		}
		for k := i; k < j; k++ {
			pos += chars[k].w
		}
		i = j
	}
	if len(bd.rects) == 0 {
		return band{}, false
	}
	for ri := range clear {
		p.runs[ri].highlight = ""
	}
	if clear[0] {
		p.def.highlight = ""
	}
	return bd, true
}
