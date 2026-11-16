package pptx

import (
	"strings"
	"testing"
)

const slideWithLines = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>` +
	// page 3's underline: 375.7 x 1.5 px
	`<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="46" name="直接连接符 45"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr><p:spPr><a:xfrm><a:off x="5612765" y="5533390"/><a:ext cx="3578225" cy="14605"/></a:xfrm><a:prstGeom prst="line"><a:avLst/></a:prstGeom></p:spPr></p:cxnSp>` +
	// a real diagonal stays
	`<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="47" name="斜线"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="952500" cy="190500"/></a:xfrm><a:prstGeom prst="line"><a:avLst/></a:prstGeom></p:spPr></p:cxnSp>` +
	`</p:spTree></p:cSld></p:sld>`

func TestNearlyLevelLineIsMadeLevel(t *testing.T) {
	out, ids, err := FixLines([]byte(slideWithLines))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "46" {
		t.Fatalf("straightened %v", ids)
	}
	if !strings.Contains(string(out), `<a:off x="5612765" y="5540692"/><a:ext cx="3578225" cy="0"/>`) {
		t.Errorf("underline not level around its middle: %s", out)
	}
	if !strings.Contains(string(out), `<a:off x="0" y="0"/><a:ext cx="952500" cy="190500"/>`) {
		t.Error("a real diagonal was changed")
	}
	data := buildDeck(t, slideWithLines)
	fixed, _, err := Fix(data, Options{Lines: true})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := Verify(data, fixed); err != nil || res.GeomDriftEMU > 2 || len(res.TextDiffs) > 0 {
		t.Errorf("verify %+v %v", res, err)
	}
}
