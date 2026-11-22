package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

var deckIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="13" rx="2"/><path d="M12 16v5m-4 0h8M8 8h8m-8 4h5"/></svg>`))

// Native controls keep keyboard focus and disabled behavior. Press feedback is
// immediate, with no entrance or theme-switch animation.
func pressFeedback(button ui.Element) ui.Element {
	if button.Pressed() {
		button.Scale(0.96, 0.96)
	}
	return button
}

func (a *app) chooseFile() {
	a.mu.Lock()
	if a.busy || a.picking {
		a.mu.Unlock()
		return
	}
	a.picking = true
	parent := a.win
	a.mu.Unlock()
	a.refresh()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: parent, Title: "选择要转换的课件", ButtonLabel: "转换课件",
			Filters: []mygo.FileFilter{{Name: "PowerPoint 课件", Extensions: []string{"pptx"}}},
		})
		a.mu.Lock()
		a.picking = false
		a.mu.Unlock()
		if err != nil {
			a.note("选择文件失败：" + err.Error())
		} else if len(paths) > 0 {
			a.process(paths[0])
		}
		a.refresh()
	}()
}

func (a *app) retry() {
	a.mu.Lock()
	path := a.path
	a.mu.Unlock()
	if path != "" {
		a.process(path)
	}
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	a.mu.Lock()
	defer a.mu.Unlock()

	ui.Column(c).Fill().Padding(24).Gap(20).Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, "课件转换").FontSize(24).Bold()
			ui.Text(c, "PowerPoint / WPS  →  希沃白板").FontSize(13).TextColor(t.TextMuted)
		})

		ui.Scroll(c.Key("workspace")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(16).Children(func() {
				ui.Column(c.Key("drop")).FillWidth().Padding(24).Gap(12).
					Radius(20).Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, deckIcon).Size(36, 36).TextColor(t.Accent)
					if a.path == "" {
						ui.Text(c, "把课件拖到这里").FontSize(20).Bold()
						ui.Text(c, "选择 .pptx，转换后的 .enbx 会保存在原课件旁。").FontSize(13).TextColor(t.TextMuted)
					} else {
						ui.Text(c, filepath.Base(a.path)).FontSize(17).Bold().MaxLines(2)
						label := "输出到原文件所在文件夹"
						if a.diag.slides > 0 {
							ui.Textf(c, "%d 页课件 · %s", a.diag.slides, label).FontSize(12).TextColor(t.TextMuted)
						} else {
							ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
						}
					}
					label := "选择课件"
					if a.path != "" {
						label = "选择其他课件"
					}
					pressFeedback(ui.PrimaryButton(c, label).Height(38).PaddingX(20).
						Disabled(a.busy || a.picking)).OnClick(a.chooseFile)
					ui.Text(c, "保留原始 PPTX · 同名输出会覆盖").FontSize(11).TextColor(t.TextMuted)
				})

				if a.busy || a.failed || a.out != "" || a.enbx != "" {
					a.resultView(c, t)
				}
				ui.Column(c.Key("font-settings")).Gap(6).Padding(16).Radius(12).Background(t.Surface).Children(func() {
					ui.Checkbox(c, &a.kaitiSC, "在 Mac 上授课").Disabled(a.busy)
					ui.Text(c, "将楷体映射为 Kaiti SC。使用 Windows 一体机授课时请关闭。").FontSize(12).TextColor(t.TextMuted)
				})

				ui.Checkbox(c, &a.advanced, "高级").Disabled(a.busy)
				if a.advanced {
					ui.Column(c).Disabled(a.busy).FillWidth().Children(func() { a.advancedView(c, t) })
				}
			})
		})
		ui.Divider(c)
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "希沃偏移修复  "+mygo.App.Version()).Grow(1).FontSize(12).TextColor(t.TextMuted)
			pressFeedback(ui.Button(c, "检查更新").Height(32).Disabled(a.busy)).OnClick(updater.CheckForUpdates)
		})
	})
}

func (a *app) resultView(c *ui.Context, t *ui.Theme) {
	title, color := "转换完成", t.Success
	if a.busy {
		title, color = "正在转换…", t.Accent
		if a.enbx != "" {
			title = "正在对账…"
		}
	} else if a.failed {
		title, color = "需要处理", t.Danger
	} else if a.enbx != "" {
		title, color = "对账结果", t.Text
	}
	for _, line := range a.lines {
		if !a.busy && !a.failed && strings.Contains(line, "没有完全转换") {
			title, color = "转换完成 · 有内容需检查", t.Warning
		}
	}
	ui.Column(c.Key("result")).FillWidth().Gap(12).Padding(20).Radius(16).Background(t.Surface).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if a.busy {
				ui.Spinner(c).Size(16, 16)
			}
			ui.Text(c, title).FontSize(15).Bold().TextColor(color)
		})
		if a.busy {
			ui.Text(c, "正在整理文字、图片与动画。首次转换需要读取本机字体。").FontSize(12).TextColor(t.TextMuted)
			return
		}
		ui.Row(c).Gap(10).Children(func() {
			if a.out != "" {
				pressFeedback(ui.PrimaryButton(c, "显示输出").Height(36)).OnClick(a.reveal)
			}
			if a.path != "" {
				pressFeedback(ui.Button(c, "重新转换").Height(36)).OnClick(a.retry)
			}
		})
		for _, line := range a.lines {
			ui.Text(c, line).FontSize(12).TextColor(t.TextMuted)
		}
	})
}
