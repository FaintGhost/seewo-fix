# 第三方资源

## DrawingML 预设形状表

文件：`seewo-fix/internal/enbxgen/presetShapeDefinitions.xml`

该资源用于计算 DrawingML 预设形状的几何路径。现有代码注释记录其来自 [Apache POI](https://github.com/apache/poi)，对应 ECMA-376 Part 1 Annex D 的形状定义，适用 Apache License 2.0。

随仓库附带：

- [Apache License 2.0](licenses/Apache-2.0.txt)
- [Apache POI 上游 NOTICE](licenses/Apache-POI-NOTICE.txt)

NOTICE 保留上游全文，其中部分条目涉及 POI 的其他组成部分，并不表示本仓库包含那些组件。

## Go 模块依赖

Go 依赖及固定版本记录在 `seewo-fix/go.mod` 和 `seewo-fix/go.sum` 中。依赖源码未 vendoring 到本仓库，许可信息以各上游模块为准。

## 验证材料

本仓库不包含验证用课件、PowerPoint/希沃截图、商业字体或希沃客户端解包代码。`SaveInfoMetadataFile.xml` 是转换器输出 ENBX 时使用的元素类型元数据，不包含客户端可执行代码。
