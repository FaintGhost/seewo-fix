package enbxgen

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// fixed measures every character as 0.5 em, so tests do not depend on the
// fonts of the machine.
type fixed struct{}

func (fixed) Width(t, _ string, _, _ bool, size float64) (float64, bool) {
	return float64(len([]rune(t))) * size / 2, true
}

func miniDeck(t *testing.T) []byte {
	t.Helper()
	parts := map[string]string{
		"ppt/presentation.xml":            `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="slide" Target="slides/slide1.xml"/><Relationship Id="rId2" Type="theme" Target="theme/theme1.xml"/></Relationships>`,
		"ppt/theme/theme1.xml":            `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:clrScheme name="x"><a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1><a:accent1><a:srgbClr val="4874CB"/></a:accent1></a:clrScheme><a:fontScheme name="x"><a:minorFont><a:latin typeface="Times New Roman"/><a:ea typeface="楷体"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`,
		"ppt/slides/slide1.xml": `<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>` +
			`<p:sp><p:nvSpPr><p:cNvPr id="2" name="文本框 1"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="952500" y="952500"/><a:ext cx="4762500" cy="476250"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square"/><a:p><a:r><a:rPr lang="zh-CN" sz="3200" b="1"><a:latin typeface="楷体"/><a:ea typeface="楷体"/></a:rPr><a:t>修饰</a:t></a:r><a:r><a:rPr lang="en-US" sz="3200" b="1"><a:highlight><a:srgbClr val="FFFF00"/></a:highlight><a:latin typeface="Times New Roman"/></a:rPr><a:t> pron.</a:t></a:r></a:p></p:txBody></p:sp>` +
			`<p:sp><p:nvSpPr><p:cNvPr id="3" name="椭圆 2"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1863090" cy="469900"/></a:xfrm><a:prstGeom prst="ellipse"><a:avLst/></a:prstGeom><a:noFill/><a:ln w="38100"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></a:ln></p:spPr></p:sp>` +
			`</p:spTree></p:cSld><p:timing><p:tnLst><p:par><p:cTn id="1"><p:childTnLst><p:par><p:cTn id="2" presetID="3" presetClass="entr" presetSubtype="10" nodeType="clickEffect"><p:childTnLst><p:animEffect><p:cBhvr><p:cTn id="3" dur="500"/><p:tgtEl><p:spTgt spid="3"/></p:tgtEl></p:cBhvr></p:animEffect></p:childTnLst></p:cTn></p:par></p:childTnLst></p:cTn></p:par></p:tnLst></p:timing></p:sld>`,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, b := range parts {
		w, _ := zw.Create(n)
		w.Write([]byte(b))
	}
	zw.Close()
	return buf.Bytes()
}

func TestConvertMiniDeck(t *testing.T) {
	res, err := Convert(miniDeck(t), Options{Name: "测试", Highlights: true, Measurer: fixed{}, FontMap: map[string]string{"楷体": "Kaiti SC"}})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(res.Enbx), int64(len(res.Enbx)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
	}
	for _, n := range []string{"Document.xml", "Board.xml", "Reference.xml", "SaveInfoMetadataFile.xml", "Slides/Slide_0.xml"} {
		if !strings.HasPrefix(got[n], "\xef\xbb\xbf<") {
			t.Errorf("%s missing or without BOM", n)
		}
	}
	s := got["Slides/Slide_0.xml"]
	for _, want := range []string{
		"<Source>Kaiti SC</Source>", "<Source>Times New Roman</Source>",
		"<ColorBrush>#FFFFFF00</ColorBrush>", // the highlighted run
		"<GeometryType>Custom_PPTX_Ellipse</GeometryType>",
		"<Type>BlindIn</Type>", "<Duration>5000000</Duration>",
		"<AnimationOrders>", "<OriginPptxId>3</OriginPptxId>",
		"<SlidePptxOriginIdExtension>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("slide lacks %s", want)
		}
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings: %v", res.Warnings)
	}
	// stable: the same deck converts to the same bytes
	deck := miniDeck(t)
	again, _ := Convert(deck, Options{Name: "测试", Highlights: true, Measurer: fixed{}, FontMap: map[string]string{"楷体": "Kaiti SC"}, Now: zeroTime})
	again2, _ := Convert(deck, Options{Name: "测试", Highlights: true, Measurer: fixed{}, FontMap: map[string]string{"楷体": "Kaiti SC"}, Now: zeroTime})
	if !bytes.Equal(again.Enbx, again2.Enbx) {
		t.Error("conversion is not deterministic")
	}
}

// The Mac importer needs explicit directory entries: with only implicit
// parents it shows in-memory previews, but lazy image URLs later return 404.
func TestConvertIncludesZipDirectories(t *testing.T) {
	res, err := Convert(miniDeck(t), Options{Measurer: fixed{}})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(res.Enbx), int64(len(res.Enbx)))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Slides/", "Resources/"} {
		found := false
		for _, file := range zr.File {
			if file.Name == name {
				found = true
				if !file.FileInfo().IsDir() {
					t.Errorf("%s is not marked as a directory", name)
				}
				break
			}
		}
		if !found {
			t.Errorf("missing explicit directory entry %s", name)
		}
	}
}

var zeroTime = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
