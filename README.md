# 希沃偏移修复 · seewo-fix

将 PowerPoint / WPS 的 `.pptx` 直接转换为希沃白板 `.enbx`，尽量保留文字位置、换行、图片、形状、表格、高亮和动画。提供 Go 命令行工具与基于 [mygo](https://github.com/egoist/mygo) 的原生桌面界面。

当前主要在 **macOS Apple Silicon** 上开发和验证。转换在本地完成，不调用希沃云端 PPTX 转换服务；生成的文件仍需用希沃白板打开。**希沃客户端打开文件后可能同步到登录账号的云空间。**

这是非官方转换工具，格式兼容性依据样本和客户端行为验证。它不是 PowerPoint 的完整排版引擎，也不保证所有课件逐像素一致。

## 快速开始

需要 [mise](https://mise.jdx.dev/)、仓库指定的 Go **1.27.4**，以及课件使用的字体。首次安装 Go 工具链和模块依赖需要联网。

```bash
git clone https://github.com/FaintGhost/seewo-fix.git
cd seewo-fix
mise trust
mise install
make cli
```

仓库根目录与 Go 模块目录不同：Go 模块位于 `seewo-fix/`，根目录的 Makefile 已处理工作目录。

```bash
# 在 Mac 上授课：把楷体映射为 Kaiti SC
./seewo-fix/build/seewo-fix convert '/path/to/课件.pptx' --kaiti-sc

# 指定输出位置
./seewo-fix/build/seewo-fix convert '/path/to/课件.pptx' \
  --kaiti-sc -o '/path/to/课件（希沃）.enbx'

# 只转换指定页，页码从 1 开始，以逗号分隔
./seewo-fix/build/seewo-fix convert '/path/to/课件.pptx' --pages 3,8,24
```

CLI 默认写入源文件旁的 `课件.enbx`。源 PPTX 不会被修改，**同名输出文件会被覆盖**；需要保留历史结果时请使用 `-o`。

## 桌面应用

```bash
make app

# Apple Silicon 构建产物
open 'seewo-fix/build/darwin-arm64/希沃偏移修复.app'
```

将原始 `.pptx` 拖入窗口，应用自动生成 `原文件名（希沃）.enbx`，再用希沃白板打开。拖入多个文件时当前只处理第一个；处理完成后可点击「显示输出」在 Finder 定位文件。

GUI 默认开启 Mac 字体映射。准备在 Windows 一体机上放映时，在「高级」中取消「楷体换成 Kaiti SC」，并确保目标电脑具有所需字体。高级选项还提供组合归一相关设置、自动调整冻结及手动位移。

也可先拖入 PPTX，再拖入与之对应的希沃导出 ENBX，进行位置对账和高亮修补。这是辅助诊断流程，日常转换只需拖入 PPTX。

## 已实现的转换

| 内容 | 实现情况 |
|---|---|
| 文字 | 字体、字号、颜色、粗斜体、下划线、段落、缩进、显式换行和项目符号 |
| 排版 | 使用本机字体度量估算 PowerPoint 换行，处理中文禁则及英文句号、逗号悬挂 |
| 形状 | DrawingML 预设几何、调整参数、线条、箭头和组合坐标 |
| 图片 | 资源打包、裁剪参数、边框和阴影 |
| 表格 | 行列、合并单元格、边框、部分样式及文字撑高行高 |
| 高亮 | 保留文字背景；部分混排高亮改为统一高度的底色条 |
| 动画 | 百叶窗、出现、淡入淡出、路径和按段落出现；保留已支持的触发顺序 |
| 输入修复 | 组合归一、彩色叠加文字对齐、近水平/垂直线条拉直 |

转换器会对已识别但尚未支持的内容输出警告。无警告不等于视觉完全一致，授课前仍应在目标电脑上检查重点页面。

## 字体与精度

- **楷体 / Kaiti SC**：`--kaiti-sc` 用于 Mac，GUI 默认开启。转换器针对 Kaiti SC 和 Times New Roman 混排校准了行距，避免长列表逐行下移。
- **Office 私有字体**：PowerPoint 能使用安装包内的字体，希沃不一定能看到。若课件使用 Cooper Black 等字体，需要让目标系统也能使用它们。工具不会自动安装或附带商业字体。
- **10 px 内边距**：希沃 Text 自带内边距。其存储坐标与 PPTX 不同，并不意味着需要给文字整体增加偏移；默认不做这类补偿。
- **图片资源**：ENBX 包含显式 `Slides/`、`Resources/` 目录条目，避免希沃 Mac 导入后图片延迟加载时出现 404。

以下内容仍有局限：

- 英文宽度、字距、两端对齐的空格分配可能与 PowerPoint 不同；两端对齐目前按左对齐输出。
- 多行或中英文混排的局部高亮，边缘及高度仍可能不一致。
- 图表、SmartArt、自定义几何、组合旋转/翻转、未映射动画和其他未实现样式不能完整还原。
- 行高与字体规则主要依据一份 31 页课件校准，复杂纵向合并、不同字号混排和其他字体仍需更多样本。
- 部分无效命令行页码会被忽略；请使用有效的 `--pages 1,3,5`，不要使用范围写法。

详细证据和复测步骤见 [验证说明](docs/VALIDATION.md)。

## 其他命令

```bash
# 诊断组合与预计位移
./seewo-fix/build/seewo-fix report 课件.pptx

# 旧流程：输出修复后的 PPTX，之后仍需交给希沃导入
./seewo-fix/build/seewo-fix fix 课件.pptx -o 课件.seewo.pptx

# 校验 PPTX 修补前后的结构、几何与文本变化（不是视觉渲染比较）
./seewo-fix/build/seewo-fix verify 课件.pptx 课件.seewo.pptx

# 与同一份 PPTX 对应的希沃导出文件进行几何对账
./seewo-fix/build/seewo-fix calibrate 课件.pptx 希沃导出.enbx

# 修补既有 ENBX 的文字高亮，并可选映射字体
./seewo-fix/build/seewo-fix enbx 课件.pptx 希沃导出.enbx --kaiti-sc

# 跳过输入修复和高亮恢复，便于诊断原始转换行为
./seewo-fix/build/seewo-fix convert 课件.pptx --raw -o 原始转换.enbx
```

`verify` 不是截图比较工具；叠加文字修复会有意改变文本结构，因此应结合所启用的修复选项解释检查结果。各子命令可用 `-h` 查看参数。

## 开发与构建

```bash
make test       # go test -race ./...
make vet        # go vet ./...
make fmt        # 格式化 Go 源码
make cli        # 命令行程序
make app        # 原生 app，跳过 DMG
```

不使用 mise 时，也可以在安装相同 Go 版本后进入 `seewo-fix/`，直接运行对应的 `go` 命令。

```text
.
├── README.md                    项目入口与用法
├── Makefile                     常用开发命令
├── mise.toml                    Go 工具链版本
├── docs/VALIDATION.md            验证范围、关键约定与已知边界
├── licenses/                    随附资源的第三方许可
├── THIRD_PARTY_NOTICES.md        第三方资源来源
├── seewo-fix/                   当前 Go 模块
│   ├── main.go                  mygo GUI 与转换工作流
│   ├── cmd/seewo-fix/            CLI
│   ├── internal/enbxgen/         PPTX → ENBX 转换与测试
│   ├── internal/pptx/            PPTX 修补、字体度量、几何检查
│   ├── internal/enbx/            ENBX 读取与对账模型
│   └── resources/               应用图标
└── seewo-pptx-fix/               早期 Python 原型与历史调查记录
```

仓库保存源码、测试、构建配置和文档。课件、Office 锁文件、生成的 ENBX、`.app`、截图、备份以及客户端解包代码均由 `.gitignore` 排除；本地 `verification/` 为独立验证资料目录，不随 Git 同步。
