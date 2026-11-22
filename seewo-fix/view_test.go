package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBusyJobKeepsSourceAndOptions(t *testing.T) {
	a := newApp()
	a.path, a.busy = "/tmp/original.pptx", true
	a.process("/tmp/next.pptx")
	a.measure("/tmp/next.enbx")
	if a.path != "/tmp/original.pptx" || a.enbx != "" {
		t.Fatal("a second drop replaced the running job")
	}
	tt := ui.NewTester(a.view, 620, 720)
	_ = tt.Click("在 Mac 上授课")
	if !a.kaitiSC {
		t.Fatal("font options changed while converting")
	}
}

func TestRevealActionRunsOutsideViewLock(t *testing.T) {
	a := newApp()
	a.path, a.out = "/tmp/deck.pptx", "/tmp/deck.enbx"
	var shown string
	a.showOutput = func(path string) error {
		if !a.mu.TryLock() {
			t.Fatal("output action ran while the view held the app lock")
		}
		a.mu.Unlock()
		shown = path
		return nil
	}
	tt := ui.NewTester(a.view, 620, 720)
	if err := tt.Click("显示输出"); err != nil {
		t.Fatal(err)
	}
	if shown != a.out {
		t.Fatalf("revealed %q, want %q", shown, a.out)
	}
}

func TestAdvancedControlsCanBeReachedInSmallWindow(t *testing.T) {
	a := newApp()
	a.advanced = true
	tt := ui.NewTester(a.view, 540, 560)
	tt.Scroll(270, 300, 0, 1000)
	if err := tt.Click("写出显式内边距"); err != nil {
		t.Fatal(err)
	}
	if !a.insets {
		t.Fatal("advanced controls could not be reached by scrolling")
	}
}

func TestResultFeedback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		out   string
		want  string
	}{
		{"success", []string{"已生成 课件（希沃）.enbx"}, "/tmp/result.enbx", "转换完成"},
		{"warning", []string{"以下内容没有完全转换：", "图表未转换"}, "/tmp/result.enbx", "转换完成 · 有内容需检查"},
		{"failure", []string{"读取失败: 文件不存在"}, "", "需要处理"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp()
			a.path = "/tmp/课件.pptx"
			a.done(tc.lines[0], tc.out)
			a.lines = tc.lines
			tt := ui.NewTester(a.view, 620, 720)
			if !tt.HasText(tc.want) {
				t.Fatalf("missing %q", tc.want)
			}
		})
	}
}

// Optional local review artifacts; these contain synthetic filenames only.
func TestRenderUIStates(t *testing.T) {
	dir := os.Getenv("SEEWOFIX_UI_SHOTS")
	if dir == "" {
		t.Skip("set SEEWOFIX_UI_SHOTS to render review images")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"empty", "busy", "success", "warning", "failure", "advanced", "dark"} {
		a := newApp()
		if state != "empty" && state != "dark" && state != "advanced" {
			a.path = "/课件/Unit 7 语法课.pptx"
			a.diag.slides = 31
		}
		switch state {
		case "busy":
			a.busy = true
		case "success":
			a.done("已生成 Unit 7 语法课（希沃）.enbx，用希沃白板打开它\n· 对齐叠加文字 6 处", "/课件/Unit 7 语法课（希沃）.enbx")
		case "warning":
			a.done("以下内容没有完全转换：\n图表对象暂不支持，请检查第 8 页。", "/课件/result.enbx")
		case "failure":
			a.done("读取失败: 文件不存在，请重新选择课件。", "")
		case "advanced":
			a.advanced = true
		}
		view := func(c *ui.Context) {
			if state == "dark" {
				c.SetTheme(ui.DarkTheme())
			}
			a.view(c)
		}
		image := ui.Render(view, 620, 720, 2)
		file, err := os.Create(filepath.Join(dir, state+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, image)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
