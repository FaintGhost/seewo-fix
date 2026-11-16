// Command seewo-fix converts PPTX files directly to 希沃白板 courseware.
// It also provides PPTX repair and ENBX comparison tools for diagnostics.
//
//	seewo-fix convert deck.pptx --kaiti-sc -o deck.enbx
//	seewo-fix report deck.pptx
//	seewo-fix fix deck.pptx -o deck.seewo.pptx
//	seewo-fix calibrate deck.pptx exported.enbx
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"seewo-fix/internal/enbxgen"
	"seewo-fix/internal/pptx"
)

// reorder puts flags before positional arguments, because Go's flag package
// stops at the first positional one, while people write
// "seewo-fix fix deck.pptx -o out.pptx".
func reorder(args []string) []string {
	takesValue := map[string]bool{
		"-o": true, "--o": true, "-pages": true, "--pages": true, "-dx": true, "--dx": true, "-dy": true, "--dy": true,
		"-scale": true, "--scale": true, "-text-dx": true, "--text-dx": true,
		"-text-dy": true, "--text-dy": true, "-text-dw": true, "--text-dw": true,
		"-text-dh": true, "--text-dh": true, "-calibrate": true, "--calibrate": true,
		"-allow-emu": true, "--allow-emu": true,
	}
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
			if takesValue[a] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "report":
		report(os.Args[2:])
	case "fix":
		fix(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	case "calibrate":
		calibrate(os.Args[2:])
	case "enbx":
		fixEnbx(os.Args[2:])
	case "convert":
		convert(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `seewo-fix — PPTX 直接转换为希沃 ENBX，兼有修补与对账工具

  seewo-fix report deck.pptx                     诊断：组合变换与预计偏移
  seewo-fix fix    deck.pptx [-o out.pptx] [选项]  生成修复版
  seewo-fix verify before.pptx after.pptx        证明修复前后渲染一致
  seewo-fix calibrate deck.pptx deck.enbx        对账：希沃实际把每个元素放到了哪里
  seewo-fix enbx deck.pptx deck.enbx [-o out]    补回希沃导入时丢掉的荧光笔等（生成 *.修复.enbx）
  seewo-fix convert deck.pptx [-o out.enbx]      直接转成希沃课件，用希沃白板打开即可（不经希沃导入）

fix 选项:
  --groups       归一化组合变换（默认开）
  --overlays     对齐叠在句子上的彩色文字（默认开，=false 关闭）
  --lines        拉直近乎水平/垂直的线条（默认开，=false 关闭）
  --ungroup      彻底取消组合（最强，转换器完全不碰组合）
  --no-autofit   冻结文本自动调整（spAutoFit → noAutofit）
  --insets       写出显式文本内边距
  --dx / --dy    整体平移补偿，单位 pt（负值向左/向上）
  --scale        整体缩放补偿（1 = 不缩放，0.75 补偿被放大 4/3 的导入结果）
  --text-dx/dy   文本框位移补偿 (pt)
  --text-dw/dh   文本框尺寸补偿 (pt)
  --calibrate    先用希沃导出的 .enbx 核对转换（只检查，不补偿）
`)
	os.Exit(2)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}

func readDeck(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		die(err)
	}
	return b
}

func report(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	fs.Parse(reorder(args))
	if fs.NArg() < 1 {
		usage()
	}
	path := fs.Arg(0)
	a, err := pptx.Analyze(readDeck(path))
	if err != nil {
		die(err)
	}
	fmt.Printf("%s\n  %d 页, %d 个组合, %d 个组合变换不是单位变换\n\n",
		filepath.Base(path), a.Slides, a.Groups, a.NonIdent)

	fmt.Println("== 组合变换（单位变换 = 1.000）==")
	for _, p := range a.Parts {
		for _, g := range p.Groups {
			mark := ""
			if !g.Idt {
				mark = "  ← 非单位"
			}
			fmt.Printf("  %-24s #%-4s scale %.3f x %.3f%s\n",
				strings.TrimPrefix(p.Name, "ppt/"), g.ID, g.SX, g.SY, mark)
		}
	}

	fmt.Println("\n== 转换器若误读组合坐标系，会产生多大位移 ==")
	fmt.Printf("  忽略 chOff/chExt : 最大 %8.2f pt (%.2f cm)\n", a.WorstIgn, a.WorstIgn*2.54/72)
	fmt.Printf("  只平移不缩放     : 最大 %8.2f pt (%.2f cm)\n", a.WorstNoSc, a.WorstNoSc*2.54/72)
	for _, p := range a.Parts {
		if p.WorstIgn > 1 || p.WorstNoSc > 1 {
			fmt.Printf("    %-24s 忽略 %7.2f pt   只平移 %7.2f pt\n",
				strings.TrimPrefix(p.Name, "ppt/"), p.WorstIgn, p.WorstNoSc)
		}
	}
}

func fix(args []string) {
	fs := flag.NewFlagSet("fix", flag.ExitOnError)
	out := fs.String("o", "", "输出文件")
	groups := fs.Bool("groups", true, "归一化组合变换")
	overlays := fs.Bool("overlays", true, "对齐叠在句子上的彩色文字（动画揭示用）")
	lines := fs.Bool("lines", true, "把肉眼水平/垂直的线条拉直（希沃会把略斜的线画短）")
	ungroup := fs.Bool("ungroup", false, "彻底取消组合")
	noAutofit := fs.Bool("no-autofit", false, "冻结文本自动调整")
	insets := fs.Bool("insets", false, "写出显式内边距")
	dx := fs.Float64("dx", 0, "整体水平平移 (pt)")
	dy := fs.Float64("dy", 0, "整体垂直平移 (pt)")
	scale := fs.Float64("scale", 0, "整体缩放补偿（1 = 不缩放）")
	tdx := fs.Float64("text-dx", 0, "文本框水平补偿 (pt)")
	tdy := fs.Float64("text-dy", 0, "文本框垂直补偿 (pt)")
	tdw := fs.Float64("text-dw", 0, "文本框宽度补偿 (pt)")
	tdh := fs.Float64("text-dh", 0, "文本框高度补偿 (pt)")
	cal := fs.String("calibrate", "", "先用该 .enbx 核对希沃的转换（只检查，不补偿）")
	_ = fs.Bool("no-text-comp", false, "已废弃：文本框不再默认补偿")
	fs.Parse(reorder(args))
	if fs.NArg() < 1 {
		usage()
	}
	src := fs.Arg(0)
	dst := *out
	if dst == "" {
		dst = strings.TrimSuffix(src, filepath.Ext(src)) + ".seewo.pptx"
	}
	opt := pptx.Options{
		Groups: *groups, Overlays: *overlays, Lines: *lines, Ungroup: *ungroup, NoAutofit: *noAutofit,
		Insets: *insets, DX: *dx, DY: *dy, Scale: *scale,
		TextDX: *tdx, TextDY: *tdy, TextDW: *tdw, TextDH: *tdh,
	}
	// 希沃的文字元素自带 10 px 内边距，转换器已经把文字放回 PowerPoint 的位置，
	// 所以文本框默认不做任何位移补偿（见 pptx.DefaultTextDX 的说明）。
	if *cal != "" {
		calib, err := pptx.Calibrate(readDeck(src), readDeck(*cal))
		if err != nil {
			die(err)
		}
		if calib.Mismatch {
			fmt.Println("  ! 这个 enbx 和该 pptx 对不上（残差不为 0），可能是从另一个版本导出的，请确认后重试")
		} else {
			fmt.Println("  enbx 核对通过：希沃的转换与源文件一致，不需要位移补偿")
		}
	}
	if !opt.Any() {
		die(fmt.Errorf("没有选中任何修复项"))
	}
	data := readDeck(src)
	fixed, st, err := pptx.Fix(data, opt)
	if err != nil {
		die(err)
	}
	if err := os.WriteFile(dst, fixed, 0o644); err != nil {
		die(err)
	}
	fmt.Printf("  归一化组合 %d 个（含 %d 个非单位变换）\n", st.Baked, st.NonIdent)
	if st.Lines > 0 {
		fmt.Printf("  拉直线条 %d 条\n", st.Lines)
	}
	if st.Overlays > 0 {
		fmt.Printf("  对齐叠加文字 %d 处\n", st.Overlays)
		for _, n := range st.OverlayNotes {
			fmt.Println("    " + n)
		}
	}
	if st.Ungrouped > 0 {
		fmt.Printf("  取消组合   %d 个\n", st.Ungrouped)
	}
	if st.Autofits > 0 {
		fmt.Printf("  冻结自动调整 %d 处\n", st.Autofits)
	}
	if st.InsetBodies > 0 {
		fmt.Printf("  写出内边距 %d 处\n", st.InsetBodies)
	}
	if st.TextBoxesAdjusted > 0 {
		fmt.Printf("  补偿文本框 %d 个 (dx=%.2fpt dy=%.2fpt dw=%.2fpt dh=%.2fpt)\n",
			st.TextBoxesAdjusted, opt.TextDX, opt.TextDY, opt.TextDW, opt.TextDH)
	}
	if st.ShapesMoved > 0 {
		fmt.Printf("  平移顶层图形 %d 个 (dx=%.1fpt dy=%.1fpt)\n", st.ShapesMoved, *dx, *dy)
	}
	for _, w := range st.Warnings {
		fmt.Println("  ! " + w)
	}
	fmt.Println("  已写入:", dst)
}

// calibrate reports what 希沃 actually did with each element of a deck.
func calibrate(args []string) {
	fs := flag.NewFlagSet("calibrate", flag.ExitOnError)
	fs.Parse(reorder(args))
	if fs.NArg() < 2 {
		usage()
	}
	c, err := pptx.Calibrate(readDeck(fs.Arg(0)), readDeck(fs.Arg(1)))
	if err != nil {
		die(err)
	}
	fmt.Printf("对账：%d 页，按希沃记录的 OriginPptxId 逐元素比对（单位 px @96dpi）\n\n", c.Slides)
	fmt.Printf("  %-10s %5s  %8s %8s %8s %8s  %s\n", "类型", "个数", "dx", "dy", "dw", "dh", "残差 (px)")
	for _, cl := range c.Classes {
		fmt.Printf("  %-10s %5d  %8.2f %8.2f %8.2f %8.2f  %+.2f / %+.2f\n",
			cl.Class, cl.N, cl.DX, cl.DY, cl.DW, cl.DH, cl.Residual[0], cl.Residual[1])
	}
	if c.Text != nil {
		fmt.Printf("\n  文本类离散度：dx ∈ [%.2f, %.2f] px\n", c.Text.MinDX, c.Text.MaxDX)
		fmt.Printf("  说明：纯文本框的 (lIns-10, tIns-10, +20, +20) 是希沃 10 px 内边距的正常结果，已从残差中扣除\n")
	}
	if c.Mismatch {
		fmt.Printf("\n  ! 残差不为 0：enbx 和该 pptx 对不上（可能来自另一个版本，如旧的 .seewo.pptx），请先确认\n")
	} else {
		fmt.Printf("\n  结论：希沃的转换与源文件一致，位置不需要补偿\n")
	}
}

func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	allow := fs.Int64("allow-emu", 2, "允许的几何位移 (EMU)")
	fs.Parse(reorder(args))
	if fs.NArg() < 2 {
		usage()
	}
	res, err := pptx.VerifyAllowing(readDeck(fs.Arg(0)), readDeck(fs.Arg(1)), *allow)
	if err != nil {
		die(err)
	}
	ok := true
	if res.GeomDriftEMU > *allow {
		fmt.Printf("  FAIL 几何漂移 %d EMU\n", res.GeomDriftEMU)
		ok = false
	}
	if len(res.TextDiffs) > 0 {
		fmt.Printf("  FAIL 文本被改动: %v\n", res.TextDiffs)
		ok = false
	}
	if len(res.EntryDiffs) > 0 {
		fmt.Printf("  FAIL zip 结构变化: %v\n", res.EntryDiffs)
		ok = false
	}
	if len(res.OtherDiffs) > 0 {
		fmt.Printf("  FAIL 非页面部件被改动: %v\n", res.OtherDiffs)
		ok = false
	}
	fmt.Printf("  页面部件 %d 个, 几何漂移 %d EMU, 文本改动 %d, 剩余非单位组合 %d\n",
		res.Parts, res.GeomDriftEMU, len(res.TextDiffs), res.GroupsLeft)
	fmt.Printf("  修复后即使转换器忽略组合坐标系，残余位移 %.4f pt / %.4f pt\n",
		res.ResidualIgn, res.ResidualNoSc)
	if !ok {
		fmt.Println("  RESULT: FAIL")
		os.Exit(1)
	}
	fmt.Println("  RESULT: PASS — 渲染一致，组合变换已归一")
}

// fixEnbx writes into a 希沃 export what its PPTX converter dropped.
func fixEnbx(args []string) {
	fs := flag.NewFlagSet("enbx", flag.ExitOnError)
	out := fs.String("o", "", "输出文件（默认 原名.修复.enbx）")
	kaiti := fs.Bool("kaiti-sc", false, "把 楷体 换成 Kaiti SC（只在 Mac 上放映时用，粗体才会生效）")
	fs.Parse(reorder(args))
	if fs.NArg() < 2 {
		usage()
	}
	src := fs.Arg(1)
	dst := *out
	if dst == "" {
		dst = pptx.EnbxOutName(src)
	}
	opt := pptx.EnbxOptions{Highlights: true}
	if *kaiti {
		opt.FontMap = map[string]string{"楷体": "Kaiti SC"}
	}
	data, st, err := pptx.PatchEnbx(readDeck(fs.Arg(0)), readDeck(src), opt)
	if err != nil {
		die(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		die(err)
	}
	fmt.Printf("  补回荧光笔 %d 段\n", st.Highlights)
	if st.Fonts > 0 {
		fmt.Printf("  字体改名 %d 处\n", st.Fonts)
	}
	if st.Skipped > 0 {
		fmt.Printf("  ! %d 段文字和课件对不上，已跳过（enbx 是不是从这个 pptx 导出的？）\n", st.Skipped)
	}
	fmt.Printf("  已写入: %s（用希沃白板打开它）\n", dst)
}

// convert turns a deck straight into a 希沃 courseware file.
func convert(args []string) {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	out := fs.String("o", "", "输出文件（默认 原名.enbx）")
	pages := fs.String("pages", "", "只转这些页，如 3 或 1,3,5")
	kaiti := fs.Bool("kaiti-sc", false, "把 楷体 换成 Kaiti SC（在 Mac 上放映时用，粗体才会生效）")
	raw := fs.Bool("raw", false, "不做任何修复，只照原样转换（用于和希沃自己的转换对比）")
	fs.Parse(reorder(args))
	if fs.NArg() < 1 {
		usage()
	}
	src := fs.Arg(0)
	dst := *out
	if dst == "" {
		dst = strings.TrimSuffix(src, filepath.Ext(src)) + ".enbx"
	}
	data := readDeck(src)
	if !*raw {
		fixed, _, err := pptx.Fix(data, pptx.Options{Groups: true, Overlays: true, Lines: true})
		if err != nil {
			die(err)
		}
		data = fixed
	}
	opt := enbxgen.Options{Name: strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)), Highlights: !*raw}
	if *kaiti {
		opt.FontMap = map[string]string{"楷体": "Kaiti SC"}
	}
	for _, p := range strings.Split(*pages, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			opt.Pages = append(opt.Pages, n)
		}
	}
	res, err := enbxgen.Convert(data, opt)
	if err != nil {
		die(err)
	}
	if err := os.WriteFile(dst, res.Enbx, 0o644); err != nil {
		die(err)
	}
	for _, w := range res.Warnings {
		fmt.Println("  ! " + w)
	}
	fmt.Printf("  已写入: %s（用希沃白板打开它）\n", dst)
}
