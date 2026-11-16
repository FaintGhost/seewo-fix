package pptx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strings"
)

// Fixing the .enbx 希沃 made.
//
// Some things are lost in 希沃's PPTX converter and cannot be carried through
// the pptx at all: text highlight (<a:highlight>, the yellow marker) comes out
// transparent although 希沃's text runs have a background of their own. So
// after importing, the teacher exports the .enbx, and this pass writes what
// was lost straight into it, matched by the shape id the converter recorded.

// EnbxOptions selects what PatchEnbx does.
type EnbxOptions struct {
	// Highlights restores <a:highlight> as the text runs' background.
	Highlights bool
	// FontMap renames font families, e.g. 楷体 → Kaiti SC where the
	// Windows name does not resolve to a font with a bold face.
	FontMap map[string]string
}

// EnbxStats says what PatchEnbx did.
type EnbxStats struct {
	Highlights int // runs (or parts of runs) given a background
	Fonts      int // font references renamed
	Skipped    int // highlighted paragraphs whose text did not match
}

type hlRange struct {
	a, b  int // byte offsets in the paragraph text
	color string
}

type deckPara struct {
	text string
	hls  []hlRange
	ok   bool // only runs (and fields): text comparable with 希沃's
}

var (
	reSpAny     = regexp.MustCompile(`(?s)<p:sp>.*?</p:sp>`)
	reCNvPrID   = regexp.MustCompile(`<p:cNvPr\b[^>]*?\bid="(\d+)"`)
	reParaItems = regexp.MustCompile(`(?s)<a:r>.*?</a:r>|<a:fld\b.*?</a:fld>|<a:br\b[^>]*/>|<a:br>.*?</a:br>`)
	reHighlight = regexp.MustCompile(`(?s)<a:highlight>\s*<a:srgbClr val="([0-9A-Fa-f]{6})"`)

	reRichText  = regexp.MustCompile(`(?s)<RichText>.*?</RichText>`)
	reOriginID  = regexp.MustCompile(`<OriginPptxId>(\d+)</OriginPptxId>`)
	reTextLine  = regexp.MustCompile(`(?s)<TextLine>.*?</TextLine>`)
	reTextRuns  = regexp.MustCompile(`(?s)<TextRuns>.*?</TextRuns>`)
	reTextRun   = regexp.MustCompile(`(?s)<TextRun>.*?</TextRun>`)
	reRunTextE  = regexp.MustCompile(`(?s)<Text>(.*?)</Text>`)
	reRunBg     = regexp.MustCompile(`(?s)(<Background>\s*<ColorBrush>)[^<]*(</ColorBrush>)`)
	reFontSrc   = regexp.MustCompile(`<Source>([^<]*)</Source>`)
	reEnbxSlide = regexp.MustCompile(`^Slides/Slide_(\d+)\.xml$`)
)

// deckParagraphs reads every text box's paragraphs, by page and shape id.
func deckParagraphs(deck []byte) (map[int]map[string][]deckPara, error) {
	zr, err := zip.NewReader(bytes.NewReader(deck), int64(len(deck)))
	if err != nil {
		return nil, fmt.Errorf("open pptx: %w", err)
	}
	var names []string
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		if slidePattern.MatchString(f.Name) {
			names = append(names, f.Name)
			files[f.Name] = f
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	out := map[int]map[string][]deckPara{}
	for page, name := range names {
		rc, err := files[name].Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		out[page] = map[string][]deckPara{}
		for _, sp := range reSpAny.FindAllString(string(body), -1) {
			idm := reCNvPrID.FindStringSubmatch(sp)
			tb := reTxBody.FindString(sp)
			if idm == nil || tb == "" {
				continue
			}
			var paras []deckPara
			for _, p := range reParaAll.FindAllString(tb, -1) {
				dp := deckPara{ok: true}
				for _, it := range reParaItems.FindAllString(p, -1) {
					if strings.HasPrefix(it, "<a:br") {
						dp.ok = false
						continue
					}
					t := runText(it)
					if hm := reHighlight.FindStringSubmatch(it); hm != nil && t != "" {
						dp.hls = append(dp.hls, hlRange{len(dp.text), len(dp.text) + len(t), strings.ToUpper(hm[1])})
					}
					dp.text += t
				}
				paras = append(paras, dp)
			}
			out[page][idm[1]] = paras
		}
	}
	return out, nil
}

func enbxEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#xD;")
	return r.Replace(s)
}

// patchTextLine gives the runs of one 希沃 paragraph the deck paragraph's
// highlights, splitting runs where a highlight starts or ends inside them.
func patchTextLine(line string, dp deckPara, st *EnbxStats) string {
	loc := reTextRuns.FindStringIndex(line)
	if loc == nil {
		return line
	}
	block := line[loc[0]:loc[1]]
	runs := reTextRun.FindAllStringIndex(block, -1)
	var texts []string
	full := ""
	for _, r := range runs {
		m := reRunTextE.FindStringSubmatch(block[r[0]:r[1]])
		t := ""
		if m != nil {
			t = html.UnescapeString(m[1])
		}
		texts = append(texts, t)
		full += t
	}
	if strings.TrimRight(full, "\r\n") != strings.TrimRight(dp.text, "\r\n") {
		st.Skipped++
		return line
	}
	var b strings.Builder
	prev := 0
	pos := 0
	for i, r := range runs {
		b.WriteString(block[prev:r[0]])
		prev = r[1]
		run := block[r[0]:r[1]]
		t := texts[i]
		a, z := pos, pos+len(t)
		pos = z
		// cut points inside this run
		cuts := []int{a}
		for _, h := range dp.hls {
			for _, c := range []int{h.a, h.b} {
				if c > a && c < z {
					cuts = append(cuts, c)
				}
			}
		}
		cuts = append(cuts, z)
		sort.Ints(cuts)
		for j := 0; j+1 < len(cuts); j++ {
			s, e := cuts[j], cuts[j+1]
			if s == e {
				continue
			}
			piece := run
			if len(cuts) > 2 {
				tl := reRunTextE.FindStringIndex(piece)
				piece = piece[:tl[0]] + "<Text>" + enbxEscape(t[s-a:e-a]) + "</Text>" + piece[tl[1]:]
			}
			for _, h := range dp.hls {
				if s >= h.a && e <= h.b {
					piece = reRunBg.ReplaceAllString(piece, "${1}#FF"+h.color+"${2}")
					st.Highlights++
					break
				}
			}
			b.WriteString(piece)
		}
	}
	b.WriteString(block[prev:])
	return line[:loc[0]] + b.String() + line[loc[1]:]
}

func patchSlide(body string, paras map[string][]deckPara, opt EnbxOptions, st *EnbxStats) string {
	if opt.Highlights && paras != nil {
		blocks := reRichText.FindAllStringIndex(body, -1)
		var b strings.Builder
		prev := 0
		for _, bl := range blocks {
			b.WriteString(body[prev:bl[0]])
			prev = bl[1]
			rt := body[bl[0]:bl[1]]
			om := reOriginID.FindStringSubmatch(body[bl[1]:])
			dps := []deckPara(nil)
			if om != nil {
				dps = paras[om[1]]
			}
			has := false
			for _, dp := range dps {
				has = has || len(dp.hls) > 0
			}
			lines := reTextLine.FindAllStringIndex(rt, -1)
			if !has || len(lines) != len(dps) {
				if has {
					st.Skipped++
				}
				b.WriteString(rt)
				continue
			}
			var nb strings.Builder
			lp := 0
			for i, l := range lines {
				nb.WriteString(rt[lp:l[0]])
				lp = l[1]
				if len(dps[i].hls) > 0 && dps[i].ok {
					nb.WriteString(patchTextLine(rt[l[0]:l[1]], dps[i], st))
				} else {
					nb.WriteString(rt[l[0]:l[1]])
				}
			}
			nb.WriteString(rt[lp:])
			b.WriteString(nb.String())
		}
		b.WriteString(body[prev:])
		body = b.String()
	}
	if len(opt.FontMap) > 0 {
		body = reFontSrc.ReplaceAllStringFunc(body, func(s string) string {
			name := reFontSrc.FindStringSubmatch(s)[1]
			if to, ok := opt.FontMap[name]; ok {
				st.Fonts++
				return "<Source>" + to + "</Source>"
			}
			return s
		})
	}
	return body
}

// PatchEnbx writes into an .enbx what 希沃's converter dropped from the deck
// it was made from.
func PatchEnbx(deck, enbxData []byte, opt EnbxOptions) ([]byte, EnbxStats, error) {
	var st EnbxStats
	paras, err := deckParagraphs(deck)
	if err != nil {
		return nil, st, err
	}
	zr, err := zip.NewReader(bytes.NewReader(enbxData), int64(len(enbxData)))
	if err != nil {
		return nil, st, fmt.Errorf("open enbx: %w", err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, st, err
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, st, err
		}
		if m := reEnbxSlide.FindStringSubmatch(f.Name); m != nil {
			var page int
			fmt.Sscan(m[1], &page)
			bom := bytes.HasPrefix(body, []byte("\xef\xbb\xbf"))
			s := patchSlide(string(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))), paras[page], opt, &st)
			body = []byte(s)
			if bom {
				body = append([]byte("\xef\xbb\xbf"), body...)
			}
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: f.Method, Modified: f.Modified})
		if err != nil {
			return nil, st, err
		}
		if _, err := w.Write(body); err != nil {
			return nil, st, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, st, err
	}
	return out.Bytes(), st, nil
}

// EnbxOutName is where a patched copy of an .enbx goes: 原名.修复.enbx.
func EnbxOutName(src string) string {
	base := strings.TrimSuffix(src, ".enbx")
	base = strings.TrimSuffix(base, ".修复")
	return base + ".修复.enbx"
}
