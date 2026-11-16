package pptx

import (
	"archive/zip"
	"bytes"
	"testing"
)

// slideWithGroup is the smallest deck that shows the bug: one group whose child
// coordinate space is in twips, exactly as WPS writes it (chExt*635 == ext),
// holding one 100x40 pt text box at 10,10 pt inside the group.
const slideWithGroup = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/><p:grpSp><p:nvGrpSpPr><p:cNvPr id="3" name="组合 2"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="2540000" y="1270000"/><a:ext cx="7620000" cy="2540000"/><a:chOff x="200" y="100"/><a:chExt cx="600" cy="200"/></a:xfrm></p:grpSpPr><p:sp><p:nvSpPr><p:cNvPr id="4" name="文本框 3"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="400" y="200"/><a:ext cx="200" cy="80"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr wrap="square"><a:noAutofit/></a:bodyPr><a:p><a:r><a:rPr lang="zh-CN" sz="1800"/><a:t>组合里的文字</a:t></a:r></a:p></p:txBody></p:sp></p:grpSp></p:spTree></p:cSld></p:sld>`

func buildDeck(t *testing.T, slide string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string]string{
		"[Content_Types].xml":   `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/><Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/></Types>`,
		"_rels/.rels":           `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/></Relationships>`,
		"ppt/presentation.xml":  `<?xml version="1.0" encoding="UTF-8"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:sldIdLst><p:sldId id="256" r:id="rId1" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"/></p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/></p:presentation>`,
		"ppt/slides/slide1.xml": slide,
	}
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGroupTransformIsNotIdentityBeforeFix(t *testing.T) {
	data := buildDeck(t, slideWithGroup)
	a, err := Analyze(data)
	if err != nil {
		t.Fatal(err)
	}
	if a.Groups != 1 || a.NonIdent != 1 {
		t.Fatalf("groups=%d nonIdentity=%d, want 1/1", a.Groups, a.NonIdent)
	}
	// chExt is in twips: 600 twips = 30 pt = 381000 EMU, so ext/chExt is 20.
	if got := a.Parts[0].WorstIgn; got < 10 {
		t.Fatalf("predicted offset %.2f pt, want the group's child space to matter", got)
	}
}

func TestFixMakesGroupTransformsIdentity(t *testing.T) {
	data := buildDeck(t, slideWithGroup)
	fixed, st, err := Fix(data, Options{Groups: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.Baked != 1 || st.NonIdent != 1 {
		t.Fatalf("baked=%d nonIdentity=%d, want 1/1", st.Baked, st.NonIdent)
	}
	res, err := Verify(data, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if res.GeomDriftEMU > 2 {
		t.Errorf("geometry drifted %d EMU", res.GeomDriftEMU)
	}
	if len(res.TextDiffs) != 0 {
		t.Errorf("text changed in %v", res.TextDiffs)
	}
	if res.GroupsLeft != 0 {
		t.Errorf("%d group(s) still non-identity", res.GroupsLeft)
	}
	if res.ResidualIgn > 0.01 || res.ResidualNoSc > 0.01 {
		t.Errorf("a broken reader still moves shapes by %.4f / %.4f pt", res.ResidualIgn, res.ResidualNoSc)
	}
}

func TestUngroupKeepsRendering(t *testing.T) {
	data := buildDeck(t, slideWithGroup)
	fixed, st, err := Fix(data, Options{Groups: true, Ungroup: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.Ungrouped != 1 {
		t.Fatalf("ungrouped %d, want 1", st.Ungrouped)
	}
	res, err := Verify(data, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if res.GeomDriftEMU > 2 || len(res.TextDiffs) != 0 {
		t.Errorf("drift %d EMU, text diffs %v", res.GeomDriftEMU, res.TextDiffs)
	}
	if res.GroupsLeft != 0 {
		t.Errorf("%d group(s) left", res.GroupsLeft)
	}
}

func TestOffsetShiftsTopLevelShapes(t *testing.T) {
	data := buildDeck(t, slideWithGroup)
	fixed, st, err := Fix(data, Options{Groups: true, DX: 10, DY: -5})
	if err != nil {
		t.Fatal(err)
	}
	if st.ShapesMoved != 1 {
		t.Fatalf("moved %d top-level shapes, want 1", st.ShapesMoved)
	}
	before, err := Analyze(data)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Analyze(fixed)
	if err != nil {
		t.Fatal(err)
	}
	_ = before
	_ = after
	zr, err := zip.NewReader(bytes.NewReader(fixed), int64(len(fixed)))
	if err != nil {
		t.Fatal(err)
	}
	var slideFile *zip.File
	for _, f := range zr.File {
		if f.Name == "ppt/slides/slide1.xml" {
			slideFile = f
		}
	}
	if slideFile == nil {
		t.Fatal("slide1.xml missing from the patched zip")
	}
	rc, err := slideFile.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		t.Fatal(err)
	}
	doc, err := Scan(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	g := Geoms(doc, Spec)
	if len(g) == 0 {
		t.Fatal("no geometry")
	}
	// The group started at 2,540,000 EMU; +10 pt = +127,000 EMU.
	if g[0].Rect.X != 2540000+127000 {
		t.Errorf("group x = %d, want %d", g[0].Rect.X, 2540000+127000)
	}
	if g[0].Rect.Y != 1270000-5*EMUPerPt {
		t.Errorf("group y = %d, want %d", g[0].Rect.Y, 1270000-5*EMUPerPt)
	}
}

func TestNoOptionsIsNoOp(t *testing.T) {
	data := buildDeck(t, slideWithGroup)
	out, st, err := Fix(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Baked != 0 {
		t.Errorf("baked %d groups with no options", st.Baked)
	}
	_ = out
}
