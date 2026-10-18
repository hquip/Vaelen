# JDK 下载加速

Issue [#12](https://github.com/hquip/Vaelen/issues/12) 反馈 JDK 下载缓慢。

原实现使用 Adoptium `binary/latest` 跳转地址，镜像重写发生在跳转之前，所以 JDK 没有命中 GitHub 镜像。HEAD 探测失败也会关闭分块下载。

现在从 Adoptium 官方 API 一次解析具体文件链接、大小、SHA256；下载和校验固定同一文件。`mirror cn` 直接使用清华 Adoptium 镜像；自定义 `java` 源优先，通用 GitHub 自定义代理仍可使用。镜像缺包、响应错误或摘要不匹配时回退固定的官方链接，校验值始终取自官方。

```bash
vsm mirror cn
vsm install java 21 --require-checksum
vsm mirror set java https://mirror.example/Adoptium/
vsm mirror off
```

自定义 Java 镜像根目录下为 `<major>/jdk/<arch>/<os>/<filename>`，例如 `21/jdk/x64/windows/OpenJDK21U-jdk_x64_windows_hotspot_21.0.12.1_1.zip`。参数中的示例镜像域名需要替换为实际服务。

下载器使用 4 连接并行；HEAD 不支持或漏报 Range 时用 1 字节请求验证能力。没有强 ETag 但存在官方 SHA256 时也可以并行，拼接后必须通过完整摘要校验。断流续传当前失败分块的剩余区间，不重复下载已完成分块；每次响应严格检查 Content-Range、长度、ETag 与编码。一块不可恢复地失败后会取消其余连接，再决定回退。

该续传只在同一次运行中生效，不保留跨进程续传状态。网络、源站限速和同步延迟仍会影响速度；不会承诺固定提速倍数。国内镜像没有同步到新包时会回退官方链路。官方 API 元数据仍需联网。

macOS Temurin 的 `Contents/Home/bin` 和 `JAVA_HOME` 也同步修正，避免包下载完成后找不到 Java。
