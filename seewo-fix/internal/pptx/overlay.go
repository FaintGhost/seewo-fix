package pptx

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Overlay text boxes.
//
// Teachers often reveal part of a sentence by laying a second, coloured text
// box over the words and animating it in: "例：I like the dress that my mother
// bought for me." in black, and "that my mother bought for me" in red on top.
// The red box is aligned by eye to where PowerPoint happens to draw those
// words. 希沃 lays text out with slightly different advances, so the black
// words drift away from the red copy and the two print over each other.
//
// No offset fixes that: the drift depends on every glyph before the overlay.
// What does fix it is to make both boxes lay out the same string. The overlay
// box takes the base box's frame, body properties and whole paragraph; the
// part before and after its own words is written in fully transparent ink, so
// whatever the renderer's advances, its words land exactly on the base's.
// The overlay keeps its shape id, so its animations are untouched.

// TransparentFill is the run fill used for the hidden part of an overlay.
const TransparentFill = `<a:solidFill><a:srgbClr val="FFFFFF"><a:alpha val="0"/></a:srgbClr></a:solidFill>`

var (
	reCNvPr    = regexp.MustCompile(`<p:cNvPr\b[^>]*?\bid="(\d+)"`)
	reXfrm     = regexp.MustCompile(`(?s)<a:xfrm\b[^>]*?(?:/>|>.*?</a:xfrm>)`)
	reOffExt   = regexp.MustCompile(`(?s)<a:off x="(-?\d+)" y="(-?\d+)"\s*/>\s*<a:ext cx="(\d+)" cy="(\d+)"\s*/>`)
	reBodyPr   = regexp.MustCompile(`(?s)<a:bodyPr\b[^>]*?(?:/>|>.*?</a:bodyPr>)`)
	reTxBody   = regexp.MustCompile(`(?s)<p:txBody>.*?</p:txBody>`)
	reParaAll  = regexp.MustCompile(`(?s)<a:p>.*?</a:p>|<a:p/>`)
	reRun      = regexp.MustCompile(`(?s)<a:r>.*?</a:r>`)
	reRunText  = regexp.MustCompile(`(?s)<a:t>(.*?)</a:t>|<a:t/>`)
	rePPr      = regexp.MustCompile(`(?s)^<a:p>(<a:pPr\b[^>]*?(?:/>|>.*?</a:pPr>))?`)
	reEndPara  = regexp.MustCompile(`(?s)<a:endParaRPr\b[^>]*?(?:/>|>.*?</a:endParaRPr>)</a:p>$`)
	reSz       = regexp.MustCompile(`<a:rPr\b[^>]*?\bsz="(\d+)"`)
	reInsAttr  = regexp.MustCompile(`\b([ltr])Ins="(\d+)"`)
	reWrapNone = regexp.MustCompile(`<a:bodyPr\b[^>]*\bwrap="none"`)
	reBold     = regexp.MustCompile(`<a:rPr\b[^>]*\bb="1"`)
	reItalic   = regexp.MustCompile(`<a:rPr\b[^>]*\bi="1"`)
	reLatin    = regexp.MustCompile(`<a:latin typeface="([^"]*)"`)
	reEA       = regexp.MustCompile(`<a:ea typeface="([^"]*)"`)
	reRotFlip  = regexp.MustCompile(`<a:xfrm\b[^>]*\b(?:rot|flipH|flipV)="`)
	reRPr      = regexp.MustCompile(`(?s)<a:rPr\b[^>]*?(?:/>|>.*?</a:rPr>)`)
	reLn       = regexp.MustCompile(`(?s)<a:ln\b[^>]*?(?:/>|>.*?</a:ln>)`)
	reRPrFills = regexp.MustCompile(`(?s)<a:(?:solidFill|gradFill|pattFill|blipFill|highlight|effectLst|effectDag|uFill)\b[^>]*?(?:/>|>.*?</a:(?:solidFill|gradFill|pattFill|blipFill|highlight|effectLst|effectDag|uFill)>)|<a:(?:noFill|grpFill)/>`)
	// Paragraph content other than runs we can carry over safely.
	reOtherPara = regexp.MustCompile(`<a:(?:br|fld)\b`)
)

// textBox is one top-level text box as the overlay pass sees it.
type textBox struct {
	start, end int // byte span of <p:sp>…</p:sp>
	xml        string
	id         string
	x, y, cx   int64
	lIns, tIns int64
	rIns       int64
	noWrap     bool
	para       string   // its only paragraph
	runs       []string // runs of that paragraph
	text       string   // unescaped text of the paragraph
	sz         int      // size of its first run, 1/100 pt; 1800 when implicit
}

func parseTextBox(data []byte, s *Shape) (*textBox, bool) {
	if s.Kind != "p:sp" || s.Xfrm == nil {
		return nil, false
	}
	x := string(data[s.TagStart:s.EndEnd])
	if reRotFlip.MatchString(x) {
		return nil, false
	}
	b := &textBox{start: s.TagStart, end: s.EndEnd, xml: x, id: s.ID, sz: 1800,
		lIns: defaultLIns, tIns: defaultTIns, rIns: defaultRIns}
	m := reOffExt.FindStringSubmatch(x)
	if m == nil {
		return nil, false
	}
	b.x, _ = strconv.ParseInt(m[1], 10, 64)
	b.y, _ = strconv.ParseInt(m[2], 10, 64)
	b.cx, _ = strconv.ParseInt(m[3], 10, 64)
	tb := reTxBody.FindString(x)
	if tb == "" {
		return nil, false
	}
	if bp := reBodyPr.FindString(tb); bp != "" {
		tag := bp[:strings.IndexByte(bp, '>')]
		b.noWrap = reWrapNone.MatchString(tag)
		for _, im := range reInsAttr.FindAllStringSubmatch(tag, -1) {
			v, _ := strconv.ParseInt(im[2], 10, 64)
			switch im[1] {
			case "l":
				b.lIns = v
			case "t":
				b.tIns = v
			case "r":
				b.rIns = v
			}
		}
	}
	paras := reParaAll.FindAllString(tb, -1)
	if len(paras) != 1 || reOtherPara.MatchString(paras[0]) {
		return nil, false
	}
	b.para = paras[0]
	b.runs = reRun.FindAllString(b.para, -1)
	if len(b.runs) == 0 {
		return nil, false
	}
	for _, r := range b.runs {
		b.text += runText(r)
	}
	if sm := reSz.FindStringSubmatch(b.runs[0]); sm != nil {
		b.sz, _ = strconv.Atoi(sm[1])
	}
	return b, true
}

func runText(r string) string {
	m := reRunText.FindStringSubmatch(r)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

func withText(r, t string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(t))
	esc := strings.ReplaceAll(buf.String(), "&#34;", "&quot;")
	esc = strings.ReplaceAll(esc, "&#39;", "&apos;")
	loc := reRunText.FindStringIndex(r)
	return r[:loc[0]] + "<a:t>" + esc + "</a:t>" + r[loc[1]:]
}

// transparentRun rewrites a run so it takes space but draws nothing: every
// fill, highlight and effect of its properties goes, and a fully transparent
// fill takes their place. An outline (<a:ln>) keeps its own fill and position.
func transparentRun(r string) string {
	loc := reRPr.FindStringIndex(r)
	if loc == nil {
		i := strings.Index(r, "<a:r>") + len("<a:r>")
		return r[:i] + "<a:rPr>" + TransparentFill + "</a:rPr>" + r[i:]
	}
	rpr := r[loc[0]:loc[1]]
	if strings.HasSuffix(rpr, "/>") {
		rpr = strings.TrimSuffix(rpr, "/>") + ">" + TransparentFill + "</a:rPr>"
		return r[:loc[0]] + rpr + r[loc[1]:]
	}
	open := rpr[:strings.IndexByte(rpr, '>')+1]
	inner := strings.TrimSuffix(rpr[len(open):], "</a:rPr>")
	ln := reLn.FindString(inner)
	if ln != "" {
		inner = strings.Replace(inner, ln, "", 1)
	}
	inner = reRPrFills.ReplaceAllString(inner, "")
	return r[:loc[0]] + open + ln + TransparentFill + inner + "</a:rPr>" + r[loc[1]:]
}

// emWidth is a coarse advance estimate in ems, good enough to tell "these words
// sit where the overlay is" from "the same words elsewhere in the line".
func emWidth(s string) float64 {
	w := 0.0
	for _, c := range s {
		switch {
		case c == ' ':
			w += 0.25
		case c > 0x2E80 || unicode.Is(unicode.Han, c) || (c >= 0xFF00 && c <= 0xFFEF):
			w += 1
		case strings.ContainsRune("ijlftrI'.,:;!|", c):
			w += 0.3
		case strings.ContainsRune("mwMW", c):
			w += 0.8
		case unicode.IsUpper(c):
			w += 0.7
		case unicode.IsLower(c) || unicode.IsDigit(c):
			w += 0.5
		default:
			w += 0.3
		}
	}
	return w
}

// findOverlay returns where o's words start in b's text, when o is a copy of
// part of b's line laid over the same words.
func findOverlay(o, b *textBox) (int, bool) {
	if o == b || utf8.RuneCountInString(strings.TrimSpace(o.text)) < 2 || o.text == b.text || o.sz != b.sz {
		return 0, false
	}
	k := strings.Index(b.text, o.text)
	if k < 0 || strings.Index(b.text[k+1:], o.text) >= 0 {
		return 0, false
	}
	em := float64(b.sz) * 127 // EMU per em
	// same line: content tops within half an em
	if math.Abs(float64((o.y+o.tIns)-(b.y+b.tIns))) > em/2 {
		return 0, false
	}
	est := emWidth(b.text[:k]) * em
	pred := float64(b.x+b.lIns) + est
	act := float64(o.x + o.lIns)
	if math.Abs(pred-act) > math.Max(0.6*em, 0.2*est) {
		return 0, false
	}
	// still on the first line of the base box
	if est > float64(b.cx) {
		return 0, false
	}
	return k, true
}

// rebuildOverlay writes o as a full copy of b's paragraph in which only o's
// own runs are visible.
func rebuildOverlay(o, b *textBox, k, cut int) string {
	// o's own runs, visible up to cut: what PowerPoint drew on o's first
	// line. The rest wrapped onto a second line there, away from the base.
	var own []string
	pos := 0
	for _, r := range o.runs {
		t := runText(r)
		a, z := pos, pos+len(t)
		pos = z
		switch {
		case z <= cut:
			own = append(own, r)
		case a >= cut:
			own = append(own, transparentRun(r))
		default:
			own = append(own, withText(r, t[:cut-a]), transparentRun(withText(r, t[cut-a:])))
		}
	}
	var runs []string
	pos, end := 0, k+len(o.text)
	mid := false
	for _, r := range b.runs {
		t := runText(r)
		a, z := pos, pos+len(t)
		pos = z
		if a < k {
			if pre := t[:min(len(t), k-a)]; pre != "" {
				runs = append(runs, transparentRun(withText(r, pre)))
			}
		}
		if a < end && z > k && !mid {
			runs = append(runs, own...)
			mid = true
		}
		if z > end {
			if post := t[max(0, end-a):]; post != "" {
				runs = append(runs, transparentRun(withText(r, post)))
			}
		}
	}
	para := "<a:p>"
	if m := rePPr.FindStringSubmatch(b.para); m != nil {
		para += m[1]
	}
	para += strings.Join(runs, "")
	if ep := reEndPara.FindString(b.para); ep != "" {
		para += ep
	} else {
		para += "</a:p>"
	}

	out := o.xml
	out = strings.Replace(out, reXfrm.FindString(out), reXfrm.FindString(b.xml), 1)
	ob, bb := reTxBody.FindString(out), reTxBody.FindString(b.xml)
	nb := ob
	if ob2, bb2 := reBodyPr.FindString(ob), reBodyPr.FindString(bb); ob2 != "" && bb2 != "" {
		nb = strings.Replace(nb, ob2, bb2, 1)
	}
	nb = strings.Replace(nb, o.para, para, 1)
	return strings.Replace(out, ob, nb, 1)
}

// FixOverlays rebuilds every overlay text box of a slide part. It returns the
// new part, the ids of the rebuilt boxes and a description of each.
func FixOverlays(data []byte, m Measurer) ([]byte, []string, []string, error) {
	doc, err := Scan(data)
	if err != nil {
		return nil, nil, nil, err
	}
	var boxes []*textBox
	for _, s := range doc.Roots {
		if b, ok := parseTextBox(data, s); ok {
			boxes = append(boxes, b)
		}
	}
	type pair struct {
		o, b *textBox
		k    int
	}
	var pairs []pair
	used := map[*textBox]bool{}
	for _, o := range boxes {
		for _, b := range boxes {
			if k, ok := findOverlay(o, b); ok && !used[o] {
				pairs = append(pairs, pair{o, b, k})
				used[o] = true
			}
		}
	}
	// an overlay is never also somebody's base: keep the outermost relation
	var ids, notes []string
	var edits []Edit
	for _, p := range pairs {
		if used[p.b] {
			continue
		}
		cut := firstLineCut(p.o, m)
		if cut == 0 {
			continue
		}
		edits = append(edits, Edit{p.o.start, p.o.end, rebuildOverlay(p.o, p.b, p.k, cut)})
		ids = append(ids, p.o.id)
		note := fmt.Sprintf("「%s」对齐到「%s」", p.o.text, p.b.text)
		if cut < len(p.o.text) {
			note += fmt.Sprintf("（PowerPoint 里「%s」换到了下一行，已隐藏）", strings.TrimSpace(p.o.text[cut:]))
		}
		notes = append(notes, note)
	}
	if len(edits) == 0 {
		return data, nil, nil, nil
	}
	SortEdits(edits)
	return Apply(data, edits), ids, notes, nil
}

// firstLineCut is where PowerPoint ends the first line of o, as a byte offset
// into o.text: len(o.text) when everything fits. Without the fonts to measure
// with, it answers len(o.text) and changes nothing.
func firstLineCut(o *textBox, m Measurer) int {
	if o.noWrap || m == nil {
		return len(o.text)
	}
	avail := float64(o.cx - o.lIns - o.rIns)
	type ch struct {
		at int
		r  rune
		w  float64
	}
	var chars []ch
	pos := 0
	for _, r := range o.runs {
		t := runText(r)
		size := float64(1800) * 127
		if sm := reSz.FindStringSubmatch(r); sm != nil {
			v, _ := strconv.Atoi(sm[1])
			size = float64(v) * 127
		}
		bold, italic := reBold.MatchString(r), reItalic.MatchString(r)
		latin, ea := "", ""
		if lm := reLatin.FindStringSubmatch(r); lm != nil {
			latin = lm[1]
		}
		if em := reEA.FindStringSubmatch(r); em != nil {
			ea = em[1]
		}
		for i, c := range t {
			cjk := c > 0x2E80
			face := latin
			if cjk {
				face = ea
			}
			w, ok := m.Width(string(c), face, bold, italic, size)
			if !ok && cjk {
				w, ok = size, true // CJK glyphs are one em wide
			}
			if !ok {
				return len(o.text)
			}
			chars = append(chars, ch{pos + i, c, w})
		}
		pos += len(t)
	}
	x, lastBreak := 0.0, 0
	for i, c := range chars {
		if c.r == ' ' {
			x += c.w
			lastBreak = c.at + 1
			continue
		}
		if c.r > 0x2E80 && i > 0 {
			lastBreak = c.at
		}
		x += c.w
		if x > avail {
			if lastBreak > 0 {
				return lastBreak
			}
			return c.at
		}
	}
	return len(o.text)
}
