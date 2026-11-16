package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"seewo-fix/internal/enbxgen"
	"seewo-fix/internal/pptx"
)

// app is the state the window shows. Dropping a deck repairs and converts it
// to ENBX in the background, preserving the source PPTX. Text boxes are not
// shifted by default (see pptx.DefaultTextDX).
type app struct {
	mu sync.Mutex

	path string
	out  string
	diag info

	advanced bool

	// Advanced knobs, all off by default.
	ungroup   bool
	noAutofit bool
	kaitiSC   bool // enbx: 楷体 → Kaiti SC, for showing on a Mac
	insets    bool
	dx, dy    float64

	// textDX/textDY are a manual text-box offset. Off by default: 希沃 already
	// puts text where PowerPoint draws it (see pptx.DefaultTextDX).
	textOn bool
	textDX float64
	textDY float64

	calib *pptx.Calibration
	enbx  string

	busy  bool
	lines []string
	win   *mygo.Window
}

// info is what a quick diagnosis of the dropped deck can tell the window.
type info struct {
	slides, groups, nonIdent int
	textBoxes                int
}

func newApp() *app {
	return &app{
		textDX:  pptx.DefaultTextDX,
		kaitiSC: true,
		textDY:  pptx.DefaultTextDY,
		lines:   []string{"把 .pptx 拖进窗口，生成 原文件名（希沃）.enbx"},
	}
}

func (a *app) options() pptx.Options {
	o := pptx.Options{
		Groups:    true,
		Overlays:  true,
		Lines:     true,
		Ungroup:   a.ungroup,
		NoAutofit: a.noAutofit,
		Insets:    a.insets,
		DX:        a.dx,
		DY:        a.dy,
	}
	if a.textOn {
		o.TextDX, o.TextDY = a.textDX, a.textDY
	}
	return o
}

func (a *app) refresh() {
	if a.win != nil {
		a.win.Update(func() {})
	}
}

func (a *app) note(lines ...string) {
	a.mu.Lock()
	a.lines = lines
	a.mu.Unlock()
	a.refresh()
}

// process runs the whole job for one dropped file, off the UI thread.
func (a *app) process(path string) {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".enbx"):
		a.measure(path)
		return
	case !strings.HasSuffix(lower, ".pptx"):
		a.note("只支持 .pptx（和可选的 .enbx 实测文件）")
		return
	case strings.HasSuffix(lower, ".seewo.pptx"):
		a.note("这个文件已经是修复后的副本了，拖原始 .pptx 即可")
		return
	}

	a.mu.Lock()
	a.path = path
	a.out = ""
	a.busy = true
	a.lines = []string{"正在转换…"}
	opt := a.options()
	kaiti := a.kaitiSC
	a.mu.Unlock()
	a.refresh()

	go func() {
		data, err := os.ReadFile(path)
		if err != nil {
			a.done("读取失败: "+err.Error(), "")
			return
		}
		if an, err := pptx.Analyze(data); err == nil {
			a.mu.Lock()
			a.diag = info{slides: an.Slides, groups: an.Groups, nonIdent: an.NonIdent}
			a.mu.Unlock()
		}
		out := EnbxName(path)
		lines, err := convertTo(path, out, data, opt, kaiti)
		if err != nil {
			a.done("转换失败: "+err.Error(), "")
			return
		}
		a.done(strings.Join(lines, "\n"), out)
	}()
}

func (a *app) done(text, out string) {
	a.mu.Lock()
	a.busy = false
	a.out = out
	a.lines = strings.Split(text, "\n")
	if out != "" {
		a.diag.textBoxes = 0
	}
	a.mu.Unlock()
	a.refresh()
}

// measure compares the deck with a 希沃 export, reports whether the conversion
// kept every element in place, and writes 原名.修复.enbx with what the
// converter dropped (text highlight) put back.
func (a *app) measure(path string) {
	a.mu.Lock()
	src := a.path // the deck the teacher dropped; a.out is the generated .enbx
	keep := a.out
	a.enbx = path
	a.busy = true
	a.lines = []string{"正在对账…"}
	a.mu.Unlock()
	a.refresh()

	go func() {
		if src == "" {
			a.done("先拖入对应的 .pptx，再拖 .enbx 才能对账", keep)
			return
		}
		deck, err := os.ReadFile(src)
		if err != nil {
			a.done("读取课件失败: "+err.Error(), keep)
			return
		}
		en, err := os.ReadFile(path)
		if err != nil {
			a.done("读取 enbx 失败: "+err.Error(), keep)
			return
		}
		c, err := pptx.Calibrate(deck, en)
		if err != nil {
			a.done("对账失败: "+err.Error(), keep)
			return
		}
		lines := []string{"实测对账（希沃实际把它放到了哪里）："}
		for _, cl := range c.Classes {
			lines = append(lines, fmt.Sprintf("  %-8s %3d 个   Δx=%+.2f Δy=%+.2f Δw=%+.2f Δh=%+.2f px",
				cl.Class, cl.N, cl.DX, cl.DY, cl.DW, cl.DH))
		}
		if c.Mismatch {
			lines = append(lines, "这个 enbx 和课件对不上（多半是从另一个版本导出的），结论不可用")
		} else {
			lines = append(lines, "希沃的转换与课件一致：文字位置不需要补偿")
		}
		a.mu.Lock()
		a.calib = c
		opt := pptx.EnbxOptions{Highlights: true}
		if a.kaitiSC {
			opt.FontMap = map[string]string{"楷体": "Kaiti SC"}
		}
		a.mu.Unlock()
		fixed, st, err := pptx.PatchEnbx(deck, en, opt)
		if err != nil {
			lines = append(lines, "补回荧光笔失败: "+err.Error())
		} else {
			dst := pptx.EnbxOutName(path)
			if err := os.WriteFile(dst, fixed, 0o644); err != nil {
				lines = append(lines, "写入失败: "+err.Error())
			} else {
				lines = append(lines, fmt.Sprintf("已生成 %s：补回荧光笔 %d 段，用希沃白板打开它", filepath.Base(dst), st.Highlights))
				if st.Skipped > 0 {
					lines = append(lines, fmt.Sprintf("  ! %d 段文字和课件对不上，已跳过", st.Skipped))
				}
			}
		}
		a.done(strings.Join(lines, "\n"), keep)
	}()
}

func (a *app) reveal() {
	a.mu.Lock()
	out := a.out
	a.mu.Unlock()
	if out != "" {
		_ = exec.Command("open", "-R", out).Start()
	}
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	a.mu.Lock()
	defer a.mu.Unlock()

	ui.Column(c).Fill().Padding(18).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "希沃偏移修复").FontSize(20).Bold()
			if a.busy {
				ui.Badge(c, "处理中…")
			}
		})
		ui.Text(c, "拖入 .pptx，直接生成希沃课件 原文件名（希沃）.enbx，用希沃白板打开即可").
			FontSize(12).TextColor(t.TextMuted)

		ui.Column(c).Key("drop").FillWidth().Padding(14).Gap(4).
			Radius(10).Border(1, t.Border).Children(func() {
			if a.path == "" {
				ui.Text(c, "把课件拖到这里").FontSize(14).TextColor(t.TextMuted)
				ui.Text(c, "组合坐标系归一 + 叠加文字对齐，其余保持不变").
					FontSize(12).TextColor(t.TextMuted)
				return
			}
			ui.Text(c, filepath.Base(a.path)).FontSize(14).Bold().MaxLines(1)
			if a.diag.slides > 0 {
				ui.Textf(c, "%d 页 · %d 个组合（%d 个非单位变换）",
					a.diag.slides, a.diag.groups, a.diag.nonIdent).
					FontSize(12).TextColor(t.TextMuted)
			}
		})

		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			again := ui.PrimaryButton(c, "重新修复")
			if a.path == "" || a.busy {
				again.Disabled(true)
			}
			if again.Clicked() {
				a.mu.Unlock()
				a.process(a.path)
				a.mu.Lock()
			}
			open := ui.Button(c, "显示输出")
			if a.out == "" {
				open.Disabled(true)
			}
			if open.Clicked() {
				a.reveal()
			}
			ui.Checkbox(c, &a.advanced, "高级")
		})

		if a.advanced {
			a.advancedView(c, t)
		}

		ui.Divider(c)
		ui.List(c, nil, len(a.lines), func(i int) {
			ui.Text(c, a.lines[i]).FontSize(12).Padding(4, 6)
		}).Grow(1)
	})
}

func (a *app) advancedView(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(6).Padding(10).Radius(10).
		Background(t.Surface).Children(func() {
		ui.Text(c, "手动微调（默认全关，一般不用动）").
			FontSize(12).TextColor(t.TextMuted)
		ui.Checkbox(c, &a.textOn, "文本框手动位移")
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "文字 X").FontSize(12).Width(52)
			ui.StepSlider(c, &a.textDX, -30, 30, 0.5).Grow(1)
			ui.Textf(c, "%+.1f", a.textDX).Width(44).FontSize(12)
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "文字 Y").FontSize(12).Width(52)
			ui.StepSlider(c, &a.textDY, -30, 30, 0.5).Grow(1)
			ui.Textf(c, "%+.1f", a.textDY).Width(44).FontSize(12)
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "整页 X").FontSize(12).Width(52)
			ui.StepSlider(c, &a.dx, -30, 30, 0.5).Grow(1)
			ui.Textf(c, "%+.1f", a.dx).Width(44).FontSize(12)
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "整页 Y").FontSize(12).Width(52)
			ui.StepSlider(c, &a.dy, -30, 30, 0.5).Grow(1)
			ui.Textf(c, "%+.1f", a.dy).Width(44).FontSize(12)
		})
		ui.Checkbox(c, &a.ungroup, "彻底取消组合（组合内容仍错时再用）")
		ui.Checkbox(c, &a.noAutofit, "冻结文本自动调整")
		ui.Checkbox(c, &a.kaitiSC, "Mac 放映：楷体 换成 Kaiti SC（粗体才会生效；Windows 一体机上放映时取消）")
		ui.Checkbox(c, &a.insets, "写出显式内边距")
		ui.Divider(c)
		if a.enbx == "" {
			ui.Text(c, "可选：把希沃里导入后导出的 .enbx 拖进来，按实测校准").
				FontSize(12).TextColor(t.TextMuted)
		} else {
			ui.Text(c, "实测："+filepath.Base(a.enbx)).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

func main() {
	a := newApp()

	mygo.App.OnOpenFile(func(path string) {
		a.process(path)
	})

	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "希沃偏移修复",
			Width:     560,
			Height:    560,
			MinWidth:  460,
			MinHeight: 420,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		a.win.OnFileDrop(func(e *mygo.FileDropEvent) {
			for _, p := range e.Paths {
				a.process(p)
				return
			}
		})
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// EnbxName is where a converted deck goes: 原文件名（希沃）.enbx, which no
// export from 希沃 is called.
func EnbxName(src string) string {
	return strings.TrimSuffix(src, filepath.Ext(src)) + "（希沃）.enbx"
}

// convertTo fixes the deck in memory and converts it to an .enbx.
func convertTo(src, dst string, data []byte, opt pptx.Options, kaiti bool) ([]string, error) {
	fixed, st, err := pptx.Fix(data, opt)
	if err != nil {
		return nil, err
	}
	eo := enbxgen.Options{Name: strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)), Highlights: true}
	if kaiti {
		eo.FontMap = map[string]string{"楷体": "Kaiti SC"}
	}
	res, err := enbxgen.Convert(fixed, eo)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(dst, res.Enbx, 0o644); err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf("已生成 %s，用希沃白板打开它", filepath.Base(dst))}
	if st.Overlays > 0 {
		lines = append(lines, fmt.Sprintf("· 对齐叠加文字 %d 处", st.Overlays))
	}
	if st.Lines > 0 {
		lines = append(lines, fmt.Sprintf("· 拉直线条 %d 条", st.Lines))
	}
	if len(res.Warnings) > 0 {
		lines = append(lines, "以下内容没有完全转换：")
		for _, w := range res.Warnings {
			lines = append(lines, "  ! "+w)
		}
	}
	return lines, nil
}
