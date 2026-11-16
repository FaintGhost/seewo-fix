# 早期 Python 原型

本目录保留最初的 PPTX 组合坐标修补工具。当前维护的实现为 [Go 主项目](../seewo-fix/README.md)，推荐使用其直接生成 ENBX 的流程。

Python 原型使用标准库，不需要安装 pip 依赖：

```bash
# 从仓库根目录执行
python3 seewo-pptx-fix/seewo_pptx_fix.py report 课件.pptx
python3 seewo-pptx-fix/seewo_pptx_fix.py fix 课件.pptx -o 课件.seewo.pptx
python3 seewo-pptx-fix/seewo_pptx_fix.py verify 课件.pptx 课件.seewo.pptx
```

它只修补 PPTX，不会生成 ENBX，不包含 Go 版本后来加入的文字叠加、高亮、表格、资源打包和动画转换。

[REPORT.md](REPORT.md) 是历史调查记录。其前部的组合变换根因假设已被第 3.5 节的实际对账推翻；当前结论以 [验证说明](../docs/VALIDATION.md) 和 Go 代码为准。

旧的 AppleScript app 与本地构建目录不随源码仓库提交。
