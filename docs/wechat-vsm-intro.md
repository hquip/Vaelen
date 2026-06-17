# 换工具就要把语言环境重装一遍?这个 Go 写的版本管理器能直接"接管"

> 说明:本文中所有命令输出均为**脱敏示例**(路径、用户名等已替换为通用占位),仅用于演示效果。

## 一、先说一个很烦的场景

你换了台新电脑,或者想从 `nvm` / `pyenv` 这种单语言工具迁到一个统一的版本管理器。结果第一件事是——**把你已经装好的语言,在新工具的体系里再装一遍**。

机器上明明躺着 JDK 8/11/17/21、两个版本的 Node、系统的 Python,但 `asdf` / `mise` 这些工具**根本不认它们**:要用,就得用它们的命令重新下载、重新编译一遍。已有的环境成了"工具看不见的存在"。

这件事的本质是:**大多数版本管理器的世界观是"一切从我装的开始"。**

`vsm`(Versatile Versions Manager,一个纯 Go、零 CGO 的多语言版本管理器)想反过来:**先盘清你机器的现状,再谈管理。**

## 二、核心能力:一条命令,盘清机器上所有语言

```bash
vsm detect
```

输出大致是这样(以下为脱敏示例路径):

```
go       1.26.1         C:\tools\go\1.26.1\bin\go.exe
go       1.25.11        C:\tools\go\1.25.11\bin\go.exe
java     21.0.1         C:\Java\jdk-21\bin\java.exe
java     17.0.12        C:\Java\jdk-17\bin\java.exe
java     11.0.22        C:\Java\jdk-11\bin\java.exe
java     1.8.0          C:\Java\jdk-8\bin\java.exe
node     25.8.0         C:\tools\node-v25\node.exe
node     20.17.0        C:\tools\node-v20\node.exe
python   3.10.9         C:\Python\python310\python.exe
ruby     3.2.3          C:\tools\ruby-3.2\bin\ruby.exe
dotnet   10.0.102       C:\Program Files\dotnet\dotnet.exe
```

注意几个细节,这是它和同类拉开差距的地方:

- **同一语言的多个版本全识别**:java 的 4 个版本(21/17/11/1.8)一个不落,不是只取 PATH 里第一个;
- **双路发现**:不仅遍历整个 `PATH`,还会从 `JAVA_HOME` / `GOROOT` / 各 `*_HOME` **环境变量直接定位**运行时——哪怕它根本没进 PATH;
- **会避坑**:自动跳过 Windows 那种 0 字节的"应用执行别名"(WindowsApps 下指向商店的假 `python.exe`),不会误报;
- **两路合并去重**:PATH 和环境变量发现同一个运行时,只算一次。

## 三、然后:一键"接管",不重装

发现只是第一步,关键是能直接用:

```bash
vsm adopt --all
```

它把检测到的系统版本**用链接纳管进 vsm**——**不重新下载、不动你的原始目录**。纳管后立刻就能切换、`use`、甚至卸载(卸载也只删链接,绝不动你系统里的真实目录)。

> 一句话:**别人从零开始装,vsm 从你机器的现状开始管。**

## 四、盘清之后,该有的也都有

接管完现状,日常管理是基本盘:

```bash
vsm install go 1.22.0      # 要装新版本也行(内置 73 种语言,17 种可直接装)
vsm use -g node 20         # 全局默认
vsm use go 1.22.0          # 当前目录(写 .tool-versions,兼容 asdf)
vsm current                # 各语言当前版本及来源
```

版本解析贴合真实工程习惯:**clone 一个老仓库、不写任何 vsm 配置,它也会照着 `go.mod` / `.nvmrc` / `.python-version` 自动选版本**(优先级:环境变量 > 目录向上找的版本文件 > 全局)。

激活上,进目录自动切换(shell hook)+ shim 兜底(应对 IDE / CI 这种没 hook 的环境)。

## 五、其它几个让它"省心"的点

- **真·单二进制,零运行时依赖**:CLI 纯标准库实现、零 CGO,一键交叉编译全平台。不需要 bash / git / curl,下载即用;
- **Windows 一等公民**:junction 隔离、`path-init` 把 shims 写入用户级 PATH 并广播(新终端即时生效),还有一个 Wails 桌面 GUI;
- **国内网络友好**:`vsm mirror cn` 一键国内镜像、`vsm proxy` 走 http / https / socks5 代理;
- **下载够硬**:HTTP Range 并行分块 + sha256 校验 + 失败重试;
- **加语言不写代码**:往 `~/.vsm/plugins/` 丢一个 JSON manifest 即可。

## 六、和主流工具对比

| 维度 | nvm/pyenv | asdf | mise | **vsm** |
|------|-----------|------|------|---------|
| 发现系统已装 | ✗ | ✗ | 部分 | **✓ PATH + 环境变量双路** |
| 同语言多版本识别 | 单语言 | ✗ | 部分 | **✓** |
| 一键纳管(不重装) | ✗ | ✗ | ✗ | **✓ adopt** |
| 运行依赖 | 各异 | bash/git/curl | 无 | **无(纯标准库)** |
| Windows | 各异 | 基本需 WSL | 支持 | **原生 + GUI** |
| 国内加速 | ✗ | ✗ | ✗ | **镜像 + 代理** |
| 配置格式 | 各自 | `.tool-versions` | `.tool-versions`/toml | **`.tool-versions`(兼容 asdf)** |

> 客观说:asdf 插件生态最成熟、mise 性能很强。vsm 的差异化集中在——**发现并接管已有环境**、零依赖单体、Windows 体验、国内网络。

## 七、谁最该试试

- 机器上**已经装了一堆语言**、不想为了换工具重装一遍的人;
- 在 **Windows** 上被 asdf 折磨过的人;
- 受够**国内下载源**反复卡顿的人。

## 八、结语

版本管理器卷了这么多年,大多在比"装得快不快、插件多不多"。vsm 换了个角度:**先承认你机器上已经有的一切,再帮你统一管起来。**

> 项目地址:<!-- 在此填入你的 GitHub 链接 -->　·　欢迎 Star / Issue / PR
