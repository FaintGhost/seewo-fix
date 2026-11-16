# 希沃白板 PPTX 导入偏移：根因分析与补丁

**对象**：`U7 4a-4d 语法课.pptx`（31 页，16:9，12192000×6858000 EMU）
**目标应用**：希沃白板 macOS 版 `com.seewo.easinote5.mac`，CFBundleShortVersionString 0.1.0，`app.asar` 54,998,577 B，sha256 前缀 `33314eab...`
**工具**：REA（`npx -y rea-agents@6.5.0 analyze-javascript-application`）+ 纯标准库几何复算
**产出**：
- `seewo-fix/` —— Go + mygo 桌面应用「希沃偏移修复」（拖入 pptx → 修复；拖入 enbx → 实测对账）
- `seewo-pptx-fix/` —— 最初的 Python 工具与本文分析记录
- `U7 4a-4d 语法课.seewo.pptx` —— 已按 enbx 实测补偿的课件

---

## 0. 一句话结论

这份课件不是 PowerPoint 生成的，而是 **WPS 演示** 生成的。WPS 把每个 `<p:grpSp>` 的**子坐标系写成了 twips（1/20 pt）**，于是这 10 个组合的**组变换全部不是单位变换**（9 个是 635:1，1 个是非均匀的 1.036×1.124×）。希沃的 PPTX→enbx 转换器对这种"非单位组变换"处理不正确，导致组合内的元素整体位移（最大可达 **800 pt ≈ 28 cm**）。

补丁把这 10 个组合的变换**归一化成单位变换**（把组变换烘焙进子元素的绝对坐标），几何完全等价，转换器即使完全忽略 `chOff/chExt` 也不会再偏移。

---

## 1. 目标分析（REA）

```
npx -y rea-agents@6.5.0 analyze-javascript-application /tmp/rea-ses/seewo-app --json
→ format: asar-tree, 5772 个源文件被解析，产出 320 MB 静态应用图
```

关键发现（`raw_result.normalized_result.graph` 索引 + 交叉 grep 验证）：

| 发现 | 证据 |
|---|---|
| 应用是 Electron（AtomApplication），业务代码全在 `app.asar` | `Info.plist`：`NSPrincipalClass=AtomApplication`、`CFBundleDocumentTypes=enbx` |
| **App 内没有任何 OOXML/PPTX 解析代码** | 在解包后的 81 MB 代码中，`sldSz` / `p:spTree` / `presentationml` / `chExt` / `a:xfrm` **零命中**（含 `.node`、`.dylib`、`public/lib/*.so`） |
| PPTX 导入 = 上传到云端转换 | `Resources/public/statics/index-BO7T2Ipg.js`：`constructFile()` → `uploadFile()` → `startTransferPPT()` |
| 转换接口是云端 API 常量 | `cloud-courseware-modes-BZ7yHm4L.js`：`transformPPT → POST_PPT_TRANSFORM`、`fetchPPTTransformResult → POST_PPT_TASK_LIST`、`getCoursewareDetail → GET_COURSEWARE_DETAIL` |
| 路由入口 | `index-DdhFZJEz.js`：`generateLocalUrl()` → `/courseware/editing/transformPPT?path=...&fileName=...` |
| 本地只负责读字节 | `dist/window/windowMain.js` 只做文件对话框（filters 含 `pptx`）；`dist/utils/readCoursewareFile.js`、`newEnbx.js` 只处理 enbx |

**推论**：偏移发生在**希沃云端转换服务**里，客户端没有可打补丁的解析路径，也没有可热替换的本地转换器。唯一可控的输入面就是 **PPTX 文件本身**。因此补丁打在 PPTX 上——这是唯一能在本地闭环验证、且不破坏应用签名的修法。

---

## 2. 根因（实测，不是猜测）

### 2.1 作者工具不是 PowerPoint

```
docProps/app.xml:  <Application>WPS 演示</Application>
ppt/tags/tag*.xml: KSO_WM_DIAGRAM_VIRTUALLY_FRAME / KSO_WM_AICARD_INVALID / TABLE_ENDDRAG_RECT
```
（共 76 个 `ppt/tags/tag*.xml`，均为金山 Office 私有标记。）

### 2.2 组合的子坐标系用的是 twips，不是 EMU

OOXML 中 `<a:xfrm>` 的 `<a:off>/<a:ext>` 是 EMU，而 `<a:chOff>/<a:chExt>` 定义的是**子元素自己的坐标系**，渲染时按 `ext/chExt` 缩放回父坐标系。PowerPoint 建组时一般写 `chOff==off`、`chExt==ext`（比例 1:1），只有"组被缩放过"才出现比例 ≠ 1。

这份文件里 10 个组合的实测比例：

```
slides/slide14.xml  组合 24   scale 635.000 x 635.000    chExt 822.60 pt
slides/slide2.xml   组合 15   scale 635.000 x 635.000    chExt 167.45 pt
slides/slide2.xml   组合 7    scale 635.000 x 635.000    chExt 229.75 pt
slides/slide21.xml  组合 19   scale 635.000 x 635.000    chExt 949.50 pt
slides/slide23.xml  组合 30   scale 635.000 x 635.000    chExt 927.60 pt
slides/slide23.xml  组合 31   scale 635.000 x 635.000    chExt 927.60 pt
slides/slide24.xml  组合 12   scale 657.868 x 713.734
slides/slide24.xml  组合 24   scale 635.000 x 635.000    chExt 897.10 pt
slides/slide5.xml   组合 7    scale 635.000 x 635.000    chExt 282.65 pt
slides/slide9.xml   组合 15   scale 635.000 x 635.000    chExt 167.45 pt
```

**635 = 914400 / 1440**，即"每个 twip 对应多少 EMU"。验证：

| 组合 | `ext`(pt) | `chExt`÷20 (pt) | 是否相等 |
|---|---|---|---|
| slide14 组合 24 | 822.60 × 425.20 | 822.60 × 425.20 | ✅ |
| slide21 组合 19 | 949.50 × 471.40 | 949.50 × 471.40 | ✅ |
| slide24 组合 24 | 897.10 × 44.40 | 897.10 × 44.40 | ✅ |
| slide24 组合 12 | 917.70 × 97.45 | 885.80 × 86.70 | ❌ 组被拉伸过（657.868 / 713.734） |

也就是说：`chExt` 用 twips 写、`off/ext` 用 EMU 写，WPS 自身自洽，但**任何面向 PowerPoint 样本调过的转换器都会踩坑**——因为 PowerPoint 样本里这个比例恒等于 1。

### 2.3 偏移量（两种典型转换器缺陷的量化）

`report` 里实现了两个"坏转换器"模型并逐元素复算绝对坐标，全部 527 个 `sp` / 9 个 `pic` / 10 个 `grpSp` / 3 个表格：

| 缺陷模型 | 含义 | 最大位移 |
|---|---|---|
| `ignore` | 完全忽略 `chOff/chExt`，把子元素坐标当 EMU 绝对值用 | **799.98 pt = 28.22 cm**（slide2 文本框 9） |
| `noscale` | 应用了组平移但漏掉缩放（`abs = off + (child - chOff)`） | **580.09 pt = 20.46 cm**（slide24 直接连接符 21） |

典型个例：

```
ignore  slide2  文本框 9      dx = -799.98 pt  dy = -281.13 pt
ignore  slide14 文本框 16     dx = -358.88 pt  dy = -426.73 pt
noscale slide14 文本框 16     dx = -246.31 pt  dy = -378.70 pt
noscale slide23 矩形 29       dx = -443.60 pt  dy =    0.00 pt
```

这与"导入后元素位置错乱/整体偏移"的现象完全一致，而且**只出现在含组合的 7 页**（slide2/5/9/14/21/23/24），其余 24 页不受影响——可以用这个当判定依据：如果希沃里恰好只有这几页乱，根因就锁定在这里。

---

## 3. 补丁

### 3.1 做法

对每个 `<p:grpSp>`：

1. 取 `sx = ext.cx/chExt.cx`、`sy = ext.cy/chExt.cy`；
2. 把组变换**烘焙**进每个直接子元素：
   `child.off' = off + (child.off − chOff) × s`，`child.ext' = child.ext × s`；
   子元素是嵌套组则递归（本文件无嵌套组，代码仍支持）；
3. 把组自身重定基到自身：`chOff := off`、`chExt := ext`。

结果：每个组的变换都变成**单位变换**，也就是 PowerPoint 的标准写法；组合语义（可整体选中、层级、分组）完全保留，**不做 ungroup**。

渲染等价性依据：OOXML 中组缩放**不缩放文字字号**，所以把比例烘焙进 `ext` 与原本的渲染结果一致（`verify` 会逐元素证明这一点）。

### 3.2 为什么这是安全的最小改动

- 只改 `<a:off>/<a:ext>/<a:chOff>/<a:chExt>` 的**属性值**，不动元素顺序、不动结构、不动 `a:t` 文本、不动其它 194 个 zip part；
- 输出 zip 的条目列表、顺序、压缩方式、时间戳与原文件一致；
- 幂等：对已修复文件再跑一次 `fix`，0 个 part 变化；
- 附带 `--bake-autofit`（变体 B）：把 340 处 `<a:spAutoFit/>` 冻结为 `<a:noAutofit/>`。若希沃转换器会按自己的字体度量重算文本框尺寸，这是第二个可疑点；默认关闭，作为对照实验。

### 3.3 用法

```bash
python3 seewo-pptx-fix/seewo_pptx_fix.py report "U7 4a-4d 语法课.pptx"
python3 seewo-pptx-fix/seewo_pptx_fix.py fix    "U7 4a-4d 语法课.pptx" -o "U7 4a-4d 语法课.seewo.pptx"
python3 seewo-pptx-fix/seewo_pptx_fix.py verify "U7 4a-4d 语法课.pptx" "U7 4a-4d 语法课.seewo.pptx"
# 变体 B（同时冻结 spAutoFit）：
python3 seewo-pptx-fix/seewo_pptx_fix.py fix "U7 4a-4d 语法课.pptx" -o out.seewo.pptx --bake-autofit
```

---

## 3.5 用希沃自己导出的 enbx 对账（决定性证据）

把导入后希沃导出的 `U7 4a-4d 语法课.enbx` 解开（它也是 zip：`Document.xml` / `Reference.xml` / `Slides/Slide_N.xml`），
按每个元素里的 `ElementPptxOriginId/OriginPptxId`（= pptx 的 `cNvPr id`）与源文件逐元素比对，
页面坐标是 1280×720 px（96 dpi，1 px = 9525 EMU）：

```
类型            个数        dx       dy       dw       dh
Text          318     -0.40    -5.20   +20.00   +20.00
Shape         181      0.00     0.00     0.00     0.00
Group          10      0.00     0.00     0.00     0.00
Picture         9      0.00     0.00     0.00     0.00
Table           3      0.00     0.00     0.00     0.00
```

结论（推翻本文前面第 2 节的推断，以此为准）：

1. **形状 / 图片 / 表格 / 组合：希沃转换得完全正确（Δ 恒等于 0）**，包括 635:1 的 twips 组合坐标系。
   也就是说组合归一补丁对这份文件并非必需（留着无害，只作为保险）。
2. **文本框的常数偏差也不是错误**：`dx = lIns − 10`、`dy = tIns − 10`、`dw = dh = +20` px。
   希沃的文字元素固定有 10 px 内边距（`希沃白板.app/Contents/Resources/public/statics/enow-log.esm-*.js`
   中 `updateTextProperty(id, {richText, width, height, padding: 10})`），
   转换器把框向外扩 10 px 正是为了让文字落在 `off + lIns`——与 PowerPoint 一致。
   带边框/填充的文本框走 Shape/Group 路径，带 `TextMargin`，Δ 恒为 0。

### 更正（2026-11-16）

本节早先据此推出"文字偏左上 7.5 pt"，并在 `seewo-fix` 中默认把文本框右移、下移 7.5 pt。**这是错的**：

- 希沃 + PowerPoint 同页截图实测：带补偿的 `.seewo.pptx` 在希沃里文字整体右下偏约 10 px（标题左边距 34.5 px vs 23.3 px），
  与没有移动的形状/图片错位。
- 当时用来"验证"的 enbx（00:01 导出）早于被测的 `.seewo.pptx`（00:33 生成），两者不是同一份文件。

`seewo-fix` 已改为默认不做文本位移，`calibrate` 只报告扣除 10 px 内边距后的残差。

希沃与 PowerPoint 之间真正剩下的差异（改坐标解决不了）：英文排得更窄（单行实测约 5%，原因未明）；
文本内容区宽 `lIns+rIns`；换行时行尾空格被移到下一行行首（全课件 4 处）；文字高亮不渲染。

## 4. 验证结果（未使用 LibreOffice，全部由 OOXML 几何模型复算）

```
$ seewo_pptx_fix.py fix ...
  patched 7 part(s), 7 byte-different:
  slides/slide14.xml, slide2, slide21, slide23, slide24, slide5, slide9
  written: U7 4a-4d 语法课.seewo.pptx

$ seewo_pptx_fix.py verify ...
  == post-patch residual offset under a converter that ignores the group space ==
  max displacement after patch: 0.0000 pt
  RESULT: PASS - geometry preserved, group transforms canonicalised

$ seewo_pptx_fix.py report U7 4a-4d 语法课.seewo.pptx
  10 group(s), 0 with a non-identity transform
  model ignore   max displacement 0.00 pt (0.00 cm)
  model noscale  max displacement 0.00 pt (0.00 cm)
```

`verify` 的四项断言全部通过：

1. **渲染等价**：按规范语义复算每个形状的绝对矩形，前后逐元素差 ≤ 2 EMU（仅整数取整误差）；
2. **文本零改动**：所有 `<a:t>` 序列完全相同；
3. **组变换已归一**：10/10 组比例 1.000×1.000；
4. **坏转换器残差归零**：`ignore` / `noscale` 两种缺陷模型下位移均为 0.0000 pt。

其它完整性检查：201 个 zip 条目、顺序一致、`testzip()` 无损坏、除 7 个 slide part 外全部字节相同、XML 声明保持 `standalone="yes"`、命名空间前缀（含 `a14`）保留。

---

## 5. 在希沃白板里复测

1. 打开希沃白板 → 新建/导入 PPT → 选 `U7 4a-4d 语法课.seewo.pptx`（上传云端转换）。
2. 重点看 **第 2、5、9、14、21、23、24 页**（原文件含组合的页）。
3. 判定标准：与源文件对比，组合内的元素（slide14 的整页大括号+箭头组、slide23 的两条横组、slide24 的连接符组）不再整体位移。
4. 若仍有"文字在框内漂移"但几何位置正确，改用变体 B（`--bake-autofit`）再试一次；若变体 B 才修好，说明二次原因是转换器重算了 `spAutoFit` 文本度量。

---

## 6. 为什么没有直接给对方做"客户端补丁"

链路是 `读取本地字节 → 上传 → POST_PPT_TRANSFORM（云端）→ 轮询 → 拉 enbx`，`app.asar` 内不存在任何 OOXML 解析代码（第 1 节）。可选做法及其代价：

| 方案 | 可行性 | 结论 |
|---|---|---|
| 改云端转换器 | 不可达 | 只能向希沃提 bug（本文档第 2 节可作为复现材料） |
| 在客户端拦截上传、先归一化再传 | 技术可行：hook `dist/window/windowMain.js` 的读文件 IPC，或在 `index-BO7T2Ipg.js::constructFile` 前插入归一化 | 需重打包 55 MB `app.asar`，macOS 代码签名/asar integrity 有被破坏风险，且升级即失效；本期未实施 |
| **归一化输入 PPTX** | 已交付 | 零风险、幂等、可复算验证，且换台电脑/线上希沃同样有效 |

---

## 附录：证据与复现

```bash
# 1. 解包目标
npx -y @electron/asar extract "/Applications/希沃白板.app/Contents/Resources/app.asar" /tmp/rea-ses/seewo-app
# 2. REA 静态重建（5772 源文件）
npx -y rea-agents@6.5.0 analyze-javascript-application /tmp/rea-ses/seewo-app --json > /tmp/rea-ses/rea-app.json
# 3. 确认本地无 OOXML 解析
grep -rl --binary-files=binary -e sldSz -e p:spTree -e presentationml /tmp/rea-ses/seewo-app | grep -v node_modules   # 空
# 4. 课件侧实测
python3 seewo-pptx-fix/seewo_pptx_fix.py report "U7 4a-4d 语法课.pptx"
```

课件统计（slides 范围）：`sp` 527、`pic` 9、`grpSp` 10、`graphicFrame` 3、`spAutoFit` 340、`sldSz` 12192000×6858000、页面无占位符（`p:ph` 仅存在于 layout/master）。
