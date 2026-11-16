package pptx

import (
	"strings"
	"testing"
)

// slideWithOverlay is page 3 of the real deck, reduced: a black sentence, a
// red copy of its clause laid over it, and a red answer "定语" over the blank
// of a line that also starts with "定语" — which must be left alone.
const slideWithOverlay = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>` +
	// base sentence
	`<p:sp><p:nvSpPr><p:cNvPr id="25" name="文本框 24"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="943610" y="3175000"/><a:ext cx="10233025" cy="645160"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square" rtlCol="0"><a:spAutoFit/></a:bodyPr><a:p><a:r><a:rPr lang="zh-CN" sz="3600" b="1"><a:latin typeface="楷体"/></a:rPr><a:t>例：</a:t></a:r><a:r><a:rPr lang="en-US" sz="3600" b="1" u="sng"><a:latin typeface="Times New Roman"/></a:rPr><a:t>I like the dress</a:t></a:r><a:r><a:rPr lang="en-US" sz="3600" b="1"/><a:t> </a:t></a:r><a:r><a:rPr lang="en-US" sz="3600" b="1" u="sng"><a:highlight><a:srgbClr val="DAE3F3"/></a:highlight><a:latin typeface="Times New Roman"/></a:rPr><a:t>that my mother bought for me</a:t></a:r><a:r><a:rPr lang="en-US" sz="3600" b="1"/><a:t>.</a:t></a:r><a:endParaRPr lang="en-US" sz="3600" b="1"/></a:p></p:txBody></p:sp>` +
	// red overlay, aligned by eye in PowerPoint and narrower than the base
	`<p:sp><p:nvSpPr><p:cNvPr id="43" name="文本框 42"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="4824730" y="3178810"/><a:ext cx="6096000" cy="645160"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square" rtlCol="0" anchor="t"><a:spAutoFit/></a:bodyPr><a:p><a:r><a:rPr lang="en-US" sz="3600" b="1" u="sng"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill><a:latin typeface="Times New Roman"/></a:rPr><a:t>that my mother bought for me</a:t></a:r></a:p></p:txBody></p:sp>` +
	// "I like cute dogs." with a red "cute" on top (narrow letters before it)
	`<p:sp><p:nvSpPr><p:cNvPr id="22" name="文本框 21"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="6275705" y="427990"/><a:ext cx="3803650" cy="645160"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr wrap="square" rtlCol="0" anchor="t"/><a:p><a:r><a:rPr lang="en-US" sz="3600" b="1"/><a:t>I like </a:t></a:r><a:r><a:rPr lang="en-US" sz="3600" b="1" u="sng"/><a:t>cute dogs</a:t></a:r><a:r><a:rPr lang="en-US" sz="3600" b="1"/><a:t>.</a:t></a:r></a:p></p:txBody></p:sp>` +
	`<p:sp><p:nvSpPr><p:cNvPr id="23" name="文本框 22"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="7394575" y="427990"/><a:ext cx="1003300" cy="645160"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr wrap="square" rtlCol="0" anchor="t"/><a:p><a:r><a:rPr lang="en-US" sz="3600" b="1"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></a:rPr><a:t>cute</a:t></a:r></a:p></p:txBody></p:sp>` +
	// a line starting with 定语 and a red answer 定语 over its blank, far right
	`<p:sp><p:nvSpPr><p:cNvPr id="9" name="文本框 8"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="2000000" y="1900000"/><a:ext cx="8000000" cy="600000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr wrap="square"/><a:p><a:r><a:rPr lang="zh-CN" sz="3600" b="1"/><a:t>定语从句：在复合句中，充当______ 的分句。</a:t></a:r></a:p></p:txBody></p:sp>` +
	`<p:sp><p:nvSpPr><p:cNvPr id="2" name="文本框 1"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="7300000" y="1900000"/><a:ext cx="1000000" cy="600000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr wrap="square"/><a:p><a:r><a:rPr lang="zh-CN" sz="3600" b="1"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></a:rPr><a:t>定语</a:t></a:r></a:p></p:txBody></p:sp>` +
	`</p:spTree></p:cSld><p:timing><p:tnLst><p:par><p:cTn id="1"><p:childTnLst><p:par><p:cTn id="2"><p:stCondLst><p:cond><p:tgtEl><p:spTgt spid="43"/></p:tgtEl></p:cond></p:stCondLst></p:cTn></p:par></p:childTnLst></p:cTn></p:par></p:tnLst></p:timing></p:sld>`

func spOf(t *testing.T, part []byte, id string) string {
	t.Helper()
	s := string(part)
	i := strings.Index(s, `<p:cNvPr id="`+id+`"`)
	if i < 0 {
		t.Fatalf("shape %s missing", id)
	}
	start := strings.LastIndex(s[:i], "<p:sp>")
	end := strings.Index(s[i:], "</p:sp>") + i + len("</p:sp>")
	return s[start:end]
}

func TestOverlayIsRebuiltOverItsBase(t *testing.T) {
	out, ids, _, err := FixOverlays([]byte(slideWithOverlay), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "43,23" {
		t.Fatalf("rebuilt %v, want the red clause (43) and the red word (23)", ids)
	}
	red := spOf(t, out, "43")
	// same frame and body as the base: both boxes now lay out the same line
	if !strings.Contains(red, `<a:off x="943610" y="3175000"/><a:ext cx="10233025" cy="645160"/>`) {
		t.Errorf("overlay did not take the base frame: %s", red)
	}
	if !strings.Contains(red, `<a:bodyPr wrap="square" rtlCol="0"><a:spAutoFit/></a:bodyPr>`) {
		t.Errorf("overlay did not take the base body: %s", red)
	}
	// the whole line is there, only the red clause is visible
	var all, visible string
	for _, r := range reRun.FindAllString(red, -1) {
		all += runText(r)
		if !strings.Contains(r, TransparentFill) {
			visible += runText(r)
			if !strings.Contains(r, `FF0000`) {
				t.Errorf("visible run lost its colour: %s", r)
			}
		} else if strings.Contains(r, "highlight") || strings.Contains(r, `FF0000`) {
			t.Errorf("hidden run still paints something: %s", r)
		}
	}
	if all != "例：I like the dress that my mother bought for me." {
		t.Errorf("overlay text %q", all)
	}
	if visible != "that my mother bought for me" {
		t.Errorf("visible text %q", visible)
	}
	// untouched: base, the answer over a blank, and the animation target
	if spOf(t, out, "25") != spOf(t, []byte(slideWithOverlay), "25") {
		t.Error("base sentence changed")
	}
	if spOf(t, out, "2") != spOf(t, []byte(slideWithOverlay), "2") {
		t.Error("answer over a blank was taken for an overlay")
	}
	if !strings.Contains(string(out), `<p:spTgt spid="43"/>`) {
		t.Error("animation target lost")
	}
	// idempotent
	again, ids2, _, err := FixOverlays(out, nil)
	if err != nil || len(ids2) != 0 || string(again) != string(out) {
		t.Errorf("second pass changed things: %v %v", ids2, err)
	}
}

func TestOverlayFixVerifies(t *testing.T) {
	data := buildDeck(t, slideWithOverlay)
	fixed, st, err := Fix(data, Options{Overlays: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.Overlays != 2 {
		t.Fatalf("overlays %d", st.Overlays)
	}
	res, err := Verify(data, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if res.GeomDriftEMU > 2 || len(res.TextDiffs) > 0 || len(res.EntryDiffs) > 0 {
		t.Errorf("verify: %+v", res)
	}
}

// fixedWidth measures every character as 0.5 em.
type fixedWidth struct{}

func (fixedWidth) Width(t, _ string, _, _ bool, size float64) (float64, bool) {
	return float64(len([]rune(t))) * size / 2, true
}

// PowerPoint pushes the red clause's last word onto a second line, under the
// next row; on the first line the base's black word shows. The rebuilt overlay
// keeps that: the wrapped word stays transparent.
func TestOverlayKeepsWordsPowerPointWrappedAway(t *testing.T) {
	o := &textBox{cx: 6096000, lIns: defaultLIns, rIns: defaultRIns,
		text: "that my mother bought for me",
		runs: []string{`<a:r><a:rPr lang="en-US" sz="3600" b="1"><a:latin typeface="X"/></a:rPr><a:t>that my mother bought for me</a:t></a:r>`}}
	// 0.5 em at 36 pt = 228600 EMU a character; 5913120 EMU fit 25 of them:
	// "that my mother bought for " is 26 with its space, "me" goes.
	cut := firstLineCut(o, fixedWidth{})
	if o.text[:cut] != "that my mother bought for " {
		t.Fatalf("first line %q", o.text[:cut])
	}
	b := &textBox{text: "例：I like the dress that my mother bought for me.", para: `<a:p><a:r><a:rPr lang="en-US" sz="3600" b="1"/><a:t>例：I like the dress that my mother bought for me.</a:t></a:r></a:p>`,
		runs: []string{`<a:r><a:rPr lang="en-US" sz="3600" b="1"/><a:t>例：I like the dress that my mother bought for me.</a:t></a:r>`}}
	o.xml = `<p:sp><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1" cy="1"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/>` + `<a:p>` + o.runs[0] + `</a:p></p:txBody></p:sp>`
	o.para = `<a:p>` + o.runs[0] + `</a:p>`
	b.xml = `<p:sp><p:spPr><a:xfrm><a:off x="9" y="9"/><a:ext cx="9" cy="9"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/>` + b.para + `</p:txBody></p:sp>`
	out := rebuildOverlay(o, b, strings.Index(b.text, o.text), cut)
	var visible string
	for _, r := range reRun.FindAllString(out, -1) {
		if !strings.Contains(r, TransparentFill) {
			visible += runText(r)
		}
	}
	if visible != "that my mother bought for " {
		t.Errorf("visible %q", visible)
	}
}
