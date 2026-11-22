package enbxgen

import "strings"

// For justified Latin paragraphs, place editable words at their PowerPoint
// advances. The group stays the animation target for the original text box.
type positionedWord struct {
	runs        []run
	x, y, width float64
	gap         float64 // width of the preceding inter-word space
}

type kernedMeasurer interface {
	KernedWidth(string, string, bool, bool, float64) (float64, bool)
}

func (c *conv) justifiedWords(b body, width float64) []positionedWord {
	if len(b.paras) != 1 || b.anchor != "t" {
		return nil
	}
	p := b.paras[0]
	if !p.justify || len(p.lines) < 2 || p.bullet || !b.wrap {
		return nil
	}
	var words []positionedWord
	var gap float64
	kerned, _ := c.m.(kernedMeasurer)
	for _, r := range p.runs {
		if r.baseline != 0 || r.sizePt != p.def.sizePt {
			return nil
		}
		for _, ch := range r.text {
			if isCJK(ch) || ch < ' ' {
				return nil
			}
		}
		remaining := r.text
		for remaining != "" {
			part, space, found := strings.Cut(remaining, " ")
			if part != "" {
				advance, ok := c.width(part, r, r.sizePt*12700)
				if !ok {
					return nil
				}
				if kerned != nil {
					if w, ok := kerned.KernedWidth(part, r.font, r.bold, r.italic, r.sizePt*12700); ok {
						advance = w
					}
				}
				if len(words) == 0 || gap > 0 {
					words = append(words, positionedWord{gap: gap})
					gap = 0
				}
				word := &words[len(words)-1]
				piece := r
				piece.text = part
				word.runs = append(word.runs, piece)
				word.width += advance / emuPerPx
			}
			if found {
				// Preserve intentional leading or repeated spaces with native text.
				if len(words) == 0 || gap != 0 {
					return nil
				}
				w, ok := c.width(" ", r, r.sizePt*12700)
				if !ok {
					return nil
				}
				gap = w / emuPerPx
			}
			remaining = space
		}
	}
	left := px(b.lIns) + p.marL
	right := width - px(b.rIns)
	y := px(b.tIns) + p.spcBef
	for start := 0; start < len(words); {
		x := left
		if start == 0 {
			x += p.indent
		}
		end := start + 1
		natural := words[start].width
		if natural > right-x {
			return nil
		}
		for end < len(words) && natural+words[end].gap+words[end].width <= right-x {
			natural += words[end].gap + words[end].width
			end++
		}
		var extra float64
		if end < len(words) && end-start > 1 {
			extra = (right - x - natural) / float64(end-start-1)
		}
		for j := start; j < end; j++ {
			if j > start {
				x += words[j].gap + extra
			}
			words[j].x = x
			words[j].y = y
			x += words[j].width
		}
		y += p.lineHeight()
		start = end
	}
	return words
}

func (c *conv) justifiedText(o *w, words []positionedWord, p para, x, y, width, height, rot float64, org *origin) {
	o.open("Group")
	o.open("Elements")
	for _, word := range words {
		length := 0
		for _, r := range word.runs {
			length += u16len(r.text)
		}
		line := para{runs: word.runs, def: p.def, lnPct: p.lnPct, lnPts: p.lnPts, align: "Left", lines: []lineSpan{{0, length}}}
		c.text(o, body{paras: []para{line}, anchor: "t"}, word.x, word.y, word.width+1, p.lineHeight(), 0, nil, 0)
	}
	o.close("Elements")
	c.tail(o, c.newID("group"), x, y, width, height, rot, org, "", nil)
	o.close("Group")
}
