# Go 主项目

项目用法、字体要求、支持范围和已知局限见 [仓库首页](../README.md)。本目录包含当前维护的 GUI、CLI 和转换引擎。

## 从本目录运行

工具链由上级目录的 `mise.toml` 指定。

```bash
mise exec -- go test -race ./...
mise exec -- go vet ./...

mkdir -p build
mise exec -- go build -o build/seewo-fix ./cmd/seewo-fix
./build/seewo-fix convert '/path/to/课件.pptx' --kaiti-sc

mise exec -- go tool mygo build -skip-dmg
```

GUI 默认输出 `原文件名（希沃）.enbx`，CLI 默认输出 `原文件名.enbx`。Mac 字体映射在 GUI 中默认开启，CLI 通过 `--kaiti-sc` 开启。

## 模块职责

| 路径 | 职责 |
|---|---|
| `main.go` | 拖放、应用状态、后台转换、对账与应用入口 |
| `view.go` | 原生界面、文件选择、操作反馈和转换结果展示 |
| `cmd/seewo-fix/main.go` | `convert`、`report`、`fix`、`verify`、`calibrate`、`enbx` 命令 |
| `internal/enbxgen/deck.go` | 读取 PPTX 包、关系、主题及基础单位 |
| `internal/enbxgen/convert.go` | 元素转换、资源打包、动画顺序及段落副本 |
| `internal/enbxgen/text.go` | 文本属性、换行、字体回退、行距和高亮条 |
| `internal/enbxgen/table.go` | 表格、合并单元格、边框、边距及行高 |
| `internal/enbxgen/preset.go` | DrawingML 预设形状公式及路径计算 |
| `internal/enbxgen/emit.go` | ENBX XML 序列化 |
| `internal/pptx/` | 组合坐标归一、叠加文字、线条、字体度量和旧流程补丁 |
| `internal/enbx/` | ENBX 结构读取与几何对账 |

## 修改时需要保留的约定

- PPTX 修补采用定点 XML 修改，避免重写整个文档导致命名空间或未知扩展丢失。
- 字体度量和换行计算使用 EMU；ENBX 输出位置、边距及字号使用 px，换算为 `1 px = 9525 EMU`。
- 希沃 Text 有固定 10 px 内边距，不要将它误判为需要整体平移的坐标错误。
- ZIP 必须显式写入 `Slides/` 和 `Resources/` 目录；只在标准 ZIP 读取器里验证成功不足以证明希沃能加载图片。
- Mac 客户端的段落属性复制不能可靠保留 `FixedLineSpacing`；当前实现通过相对行距处理指定行高。
- 行距变化应回查长列表及按段落出现动画，换行变化应回查手工叠加的标注。
- 单元测试使用合成课件及固定字体度量；不要把本地真实课件变成测试的隐含依赖。

已有实机验证与复测清单见 [验证说明](../docs/VALIDATION.md)。
