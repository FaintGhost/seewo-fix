# 希沃偏移修复

把 PowerPoint 或 WPS 的 `.pptx` 转成可在希沃白板中打开的 `.enbx`。课件在本机处理，文字、表格和受支持的动画仍可编辑。桌面应用面向 Apple Silicon Mac（M 系列芯片）。这是独立开发的非官方工具。

**[下载最新 macOS 安装包](https://github.com/FaintGhost/seewo-fix/releases/latest)** · [转换效果与验证方法](docs/VALIDATION.md) · [命令行与源码结构](seewo-fix/README.md)

## 转换一份课件

1. 在下载页选择以 `macos-arm64.dmg` 结尾的文件。打开镜像，把「希沃偏移修复」拖入「应用程序」再启动。
2. 选择授课设备。用 Mac 放映时保留「在 Mac 上授课」，应用会用 Kaiti SC 显示楷体。在 Windows 一体机上放映时关闭该项。
3. 把原始 `.pptx` 拖入窗口，或点击「选择课件」。完成后点击「显示输出」，用希沃白板打开同目录的 `原文件名（希沃）.enbx`。再次转换会覆盖同名输出，原稿会保留。

首次转换需要读取本机字体。窗口会列出转换警告和对应页码。希沃白板打开生成的文件后，可能会同步到当前登录账号的云空间。

## 哪些内容能保留

| 课件内容 | 已支持 | 授课前值得核对 |
| --- | --- | --- |
| 文字与高亮 | 字体、段落、缩进、显式换行、项目符号、局部高亮和部分两端对齐 | 目标电脑缺少字体时的换行、混排高亮边缘 |
| 图片与形状 | 内嵌图片、裁剪、预设几何、组合坐标和部分外阴影 | 自定义路径、组合旋转与翻转 |
| 表格 | 行列、合并单元格、内嵌样式、主题色、边框和行高 | 复杂合并、渐变及不同字号混排 |
| 课堂交互 | 受支持的出现、淡入淡出、路径和按段落动画，内嵌音频点播 | 开场答案遮挡、逐次点击顺序和听力播放 |

已在 PowerPoint 和 Mac 希沃白板上用一份 31 页语法课、一份 18 页听说课及两份独立生成的样例对照。18 页样例在 0.1.3 的平均边缘匹配 F1 为 95.58；该指标只衡量画布边缘位置。动画和音频另行实机播放验证。图表、SmartArt 及部分动画尚未完整支持。[逐项验证记录](docs/VALIDATION.md)

## 应用更新

从 0.1.4 开始，安装在可写目录中的应用会在后台检查稳定版，也可在窗口底部点击「检查更新」。有匹配的增量包时优先下载差异，校验失败或缺少适用的补丁时下载完整更新。应用使用内置公钥验证下载内容，安装后可选择重新启动。0.1.3 及更早版本需要先手动安装一次新版。[版本与安装包](https://github.com/FaintGhost/seewo-fix/releases)

## 命令行

安装 [mise](https://mise.jdx.dev/) 后，从仓库根目录构建 CLI。`mise.toml` 固定 Go 版本，Go 模块位于 `seewo-fix/`。

```sh
git clone https://github.com/FaintGhost/seewo-fix.git
cd seewo-fix
mise trust
mise install
mise run cli
./seewo-fix/build/seewo-fix convert '语法课.pptx' --kaiti-sc -o '语法课（希沃）.enbx'
```

CLI 默认生成 `原文件名.enbx`。`--pages 3,8,24` 可只转换指定页。要检查已有课件，可用 `report` 分析 PPTX，用 `calibrate` 对照同一原稿的希沃导出文件。各子命令支持 `-h`。

## 开发、验证与发布

| 在根目录执行 | 用途 |
| --- | --- |
| `mise run test` | 运行 Go 测试并检查数据竞争 |
| `mise run vet` | 检查 Go 代码和 MyGo 界面生命周期 |
| `mise run app` / `mise run dmg` | 构建原生应用或只读 DMG |
| `mise run compare -- 原稿截图 旧版截图 新版截图 -o 报告.html` | 为同页截图生成可调透明度的叠图报告，需要 `uv` |

GitHub Actions 会为 `main` 和 PR 测试并构建 macOS 安装包。发布时更新 `seewo-fix/mygo.json` 的版本和同目录的 `CHANGELOG.md`，推送匹配的稳定版标签（例如 `v0.1.4`）。标签构建使用仓库 Actions secret `MYGO_UPDATER_PRIVATE_KEY` 签名，为最近至多三个有签名归档的版本生成更小的增量包，上传完成后公开 Release。首个签名版本提供后续增量的基线。

转换器位于 `seewo-fix/internal/enbxgen/`，PPTX 修补与字体度量位于 `seewo-fix/internal/pptx/`。测试使用合成数据，实机课件和商业字体仅保存在本地。[模块说明](seewo-fix/README.md) · [第三方资源与许可](THIRD_PARTY_NOTICES.md)
