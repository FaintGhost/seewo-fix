package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"seewo-fix/internal/pptx"
)

// Dropping a deck is the whole workflow, and it does not move any text: 希沃
// already puts text where PowerPoint draws it.
func TestDefaultOptionsDoNotShiftText(t *testing.T) {
	a := newApp()
	o := a.options()
	if !o.Groups {
		t.Error("groups are not canonicalised by default")
	}
	if o.TextDX != 0 || o.TextDY != 0 || o.TextDW != 0 || o.TextDH != 0 {
		t.Errorf("text boxes shifted by default: %+v", o)
	}
	if o.Ungroup || o.NoAutofit || o.Insets || o.DX != 0 || o.DY != 0 {
		t.Errorf("advanced options on by default: %+v", o)
	}
}

func TestViewIsOneStep(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 560, 560)
	for _, want := range []string{"希沃偏移修复", "把课件拖到这里", "高级"} {
		if !tt.HasText(want) {
			t.Errorf("window is missing %q; texts %q", want, tt.Texts())
		}
	}
	// No strategy picker to get wrong any more.
	for _, gone := range []string{"保守", "标准", "激进"} {
		if tt.HasText(gone) {
			t.Errorf("strategy %q is still offered", gone)
		}
	}
	// Advanced is folded away until asked for.
	if tt.HasText("彻底取消组合") {
		t.Error("advanced options show before asking")
	}
	if err := tt.Click("高级"); err != nil {
		t.Fatal(err)
	}
	if !a.advanced {
		t.Fatal("advanced did not open")
	}
	if !tt.HasText("彻底取消组合") {
		t.Errorf("advanced options missing: %q", tt.Texts())
	}
}

func TestCalibrationNeverChangesOptions(t *testing.T) {
	a := newApp()
	a.enbx = "/tmp/export.enbx"
	a.calib = &pptx.Calibration{Text: &pptx.ClassDelta{
		Class: "Text", N: 318, DX: -0.4, DY: -5.2, DW: 20, DH: 20}}
	if o := a.options(); o.TextDX != 0 || o.TextDY != 0 {
		t.Errorf("a measurement switched compensation on: %+v", o)
	}
	a.textOn, a.textDX, a.textDY = true, 1.5, -2
	if o := a.options(); o.TextDX != 1.5 || o.TextDY != -2 {
		t.Errorf("manual offset ignored: %+v", o)
	}
}

// A deck that is already a patched copy is refused instead of patched twice.
func TestRefusesAlreadyPatchedCopy(t *testing.T) {
	a := newApp()
	a.process("/tmp/课件.seewo.pptx")
	if a.busy {
		t.Error("a job started for an already patched copy")
	}
	if len(a.lines) == 0 || !strings.Contains(a.lines[0], "已经是修复后的副本") {
		t.Errorf("lines %q", a.lines)
	}
}

// Dropping a 希沃 export after the deck writes 原名.修复.enbx with the
// highlight put back, and keeps the deck's output.
func TestDroppedEnbxIsPatched(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, parts map[string]string) string {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, n := range []string{"ppt/slides/slide1.xml", "Slides/Slide_0.xml"} {
			if body, ok := parts[n]; ok {
				w, _ := zw.Create(n)
				w.Write([]byte(body))
			}
		}
		zw.Close()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	deck := write("课件.seewo.pptx", map[string]string{"ppt/slides/slide1.xml": `<p:sld xmlns:a="a" xmlns:p="p"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="8" name="文本框 7"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="100" cy="100"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:p><a:r><a:rPr lang="zh-CN"><a:highlight><a:srgbClr val="FFFF00"/></a:highlight></a:rPr><a:t>修饰</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`})
	en := write("导出.enbx", map[string]string{"Slides/Slide_0.xml": "\xef\xbb\xbf<Slide><Width>1280</Width><Height>720</Height><Elements><Text><RichText><TextLines><TextLine><TextRuns><TextRun><Text>修饰</Text><Background><ColorBrush>#00FFFFFF</ColorBrush></Background></TextRun></TextRuns></TextLine></TextLines></RichText><X>0</X><Y>0</Y><Width>10</Width><Height>10</Height><ElementPptxOriginId><OriginPptxId>8</OriginPptxId></ElementPptxOriginId></Text></Elements></Slide>"})
	a := newApp()
	a.path, a.out = deck, filepath.Join(dir, "课件（希沃）.enbx")
	a.measure(en)
	waitForJob(t, a)
	got, err := os.ReadFile(filepath.Join(dir, "导出.修复.enbx"))
	if err != nil {
		t.Fatalf("no patched enbx: %v; lines %q", err, a.lines)
	}
	zr, _ := zip.NewReader(bytes.NewReader(got), int64(len(got)))
	rc, _ := zr.File[0].Open()
	body, _ := io.ReadAll(rc)
	if !strings.Contains(string(body), "#FFFFFF00") {
		t.Errorf("highlight not restored: %s", body)
	}
	if a.out != filepath.Join(dir, "课件（希沃）.enbx") {
		t.Errorf("deck output lost: %q", a.out)
	}
}

// waitForJob allows the first conversion to index the machine's fonts. A
// timeout must stop the test before it reads state still owned by the worker.
func waitForJob(t *testing.T, a *app) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		a.mu.Lock()
		busy := a.busy
		lines := strings.Join(a.lines, "\n")
		a.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not finish within 30s: %s", lines)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Dropping a deck writes 原文件名（希沃）.enbx next to it.
func TestDroppedDeckIsConverted(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"ppt/presentation.xml":            `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="slide" Target="slides/slide1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":           `<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/><p:sp><p:nvSpPr><p:cNvPr id="2" name="文本框 1"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="4762500" cy="476250"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr/><a:p><a:r><a:rPr lang="en-US" sz="3200"><a:latin typeface="Times New Roman"/></a:rPr><a:t>Hello</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	deck := filepath.Join(dir, "课件.pptx")
	if err := os.WriteFile(deck, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.process(deck)
	waitForJob(t, a)
	out := filepath.Join(dir, "课件（希沃）.enbx")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("no %s: %v; lines %q", out, err, a.lines)
	}
	if a.out != out {
		t.Errorf("out %q", a.out)
	}
}
