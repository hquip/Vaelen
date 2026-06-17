# vsm — 多语言版本管理器

`vsm`（Versatile Versions Manager）用一套工具统一管理 **Go / Node / Java / Python / Zig / Deno / Bun**
等多语言运行环境的安装、版本切换与激活。设计对标 asdf / mise / vfox，核心追求 **全链路零 CGO 的 Go 单体**：
一键交叉编译全平台、单二进制分发、内置版本切换。

> CLI 为纯标准库实现、**零外部依赖**，内置 **73 种语言/工具**：其中 17 种可直接安装
> （`go` `node` `java` `python` `zig` `deno` `bun` `gleam` `vlang` `kotlin` `tinygo` `nushell` `cmake` `ninja` `typst` `just` `uv`），
> 其余 50+ 种支持 **识别与版本切换**（涵盖 rust/dart/php/dotnet/elixir/swift/scala/elm/purescript/lean/solidity/maven/gradle/haskell/r/vala 等；
> 下载源可按需在 manifest 中补充即变为可安装）。另支持 `~/.vsm/plugins/*.json` 无限扩展、
> `vsm detect` 双路探测系统已装版本（PATH + 环境变量）、`vsm adopt` 一键纳管——不止管"自己装的"，更能接管机器上"已有的"。
> 打通 `install / use / list / current / which / exec` 主流程。
> 可选的桌面 GUI（`gui/`，Wails v2 + WebView2）：语言列表与安装/卸载、系统版本检测与一键纳管、
> 镜像切换、网络代理设置、安装进度条、当前目录 local 设置、一键写入用户级 PATH。

## 设计要点

- **三大职责**：install（装版本）/ resolve（选版本）/ activate（让命令生效）。
- **多版本隔离**：每个版本独立存放在 `~/.vsm/installs/<tool>/<version>`，切换只改“指针”，不重装。
- **版本解析优先级**（高 → 低）：
  1. 环境变量 `VSM_<TOOL>_VERSION`（如 `VSM_GO_VERSION=1.22`）
  2. 当前目录向上逐级查找的 `.tool-versions`，以及各生态**惯用版本文件**（同目录内 `.tool-versions` 优先）：
     `.nvmrc` / `.node-version`、`.python-version`、`.ruby-version`、`.java-version`、`.go-version`、
     `.sdkmanrc`，及构建文件 `go.mod`(`go x.y`)、`package.json`(`engines.node`)、
     `pyproject.toml`(`requires-python`)、`Gemfile`(`ruby`)、`runtime.txt`
  3. 全局 `~/.vsm/.tool-versions`
- **激活机制**：PATH 注入（shell hook）为主 + shim 兜底（IDE/CI 等无 hook 环境）。
- **配置格式**：兼容 asdf 的 `.tool-versions`。
- **插件化**：核心不含语言知识，新增语言只需实现 `plugin.Plugin` 接口。

## 目录结构

```
.
├── main.go                      # 入口
├── go.mod
└── internal/
    ├── cli/                     # 命令行界面与子命令分发
    │   ├── app.go
    │   └── commands.go
    ├── core/                    # 核心引擎（前端 CLI/TUI/GUI 共用）
    │   ├── paths.go             # ~/.vsm 磁盘布局
    │   ├── archive.go           # tar.gz / zip 解压（含 zip-slip 防护）
    │   ├── installer.go         # 下载（跟随重定向）+ 解压 + 安装
    │   ├── registry.go          # 已装版本索引（支持 junction/符号链接）
    │   ├── resolver.go          # 版本解析与匹配
    │   ├── activator.go         # which/exec、shim、shell hook
    │   └── detect.go            # 探测系统 PATH/环境变量已装版本
    ├── config/
    │   └── toolversions.go      # .tool-versions 读写
    └── plugin/                  # 语言插件（各实现 Plugin 接口）
        ├── plugin.go            # Plugin 接口
        ├── registry.go          # 插件注册表
        ├── util.go              # HTTP / GitHub releases / 版本比较工具
        ├── golang.go            # go     (go.dev/dl)
        ├── node.go              # node   (nodejs.org/dist)
        ├── java.go              # java   (Adoptium Temurin，端点 302 重定向)
        ├── python.go            # python (python-build-standalone，免编译)
        ├── zig.go               # zig    (ziglang.org/index.json)
        ├── deno.go              # deno   (GitHub releases)
        ├── bun.go               # bun    (GitHub releases)
        ├── manifest.go          # 声明式 manifest 引擎（数据驱动 + 外部加载）
        └── manifests/           # 内置 manifest（gleam / vlang / kotlin …）

gui/                             # 可选 Windows GUI（Wails v2 + WebView2）
├── main.go  app.go  wails.json
└── frontend/dist/{index.html, style.css, app.js}
```

## 构建

```bash
go build -o vsm .        # Linux/macOS
go build -o vsm.exe .    # Windows
```

CLI 无任何第三方依赖。交叉编译（得益于零 CGO，一键出全平台）：

```bash
GOOS=linux   GOARCH=amd64 go build -o vsm .
GOOS=darwin  GOARCH=arm64 go build -o vsm .
GOOS=windows GOARCH=amd64 go build -o vsm.exe .
```

GUI（Windows，零 CGO，无需 wails CLI）：

```powershell
go build -tags production -ldflags "-H windowsgui" -o vsm-gui.exe ./gui
```

## 命令

| 命令 | 说明 |
|------|------|
| `vsm install <tool> [version]` | 安装某版本（省略或 `latest` 装最新稳定版；支持模糊版本如 `20`） |
| `vsm uninstall <tool> <version>` | 卸载某版本 |
| `vsm use [-g] <tool> <version>` | 设为当前目录版本（写 `.tool-versions`）；`-g` 设为全局 |
| `vsm list [tool]` | 列出已安装版本（`*` 为当前生效） |
| `vsm list-all <tool>` | 列出可安装的远端版本 |
| `vsm current` | 显示各工具当前生效版本及来源 |
| `vsm which <tool>` | 打印当前版本主命令的完整路径 |
| `vsm exec <tool> [args...]` | 在当前解析版本环境下执行工具主命令 |
| `vsm exec <tool> -- <cmd> [args...]` | 在该工具环境下执行任意命令（如 `npm`） |
| `vsm reshim` | 为已安装工具重新生成 shim |
| `vsm activate <shell>` | 输出 shell 激活脚本（含进目录自动切换 hook） |
| `vsm env [shell]` | 输出当前目录应生效的环境（供 hook 调用，一般不手动用） |
| `vsm detect` | 探测系统 PATH / 环境变量中已安装的语言运行时及版本 |
| `vsm adopt [--all] <tool> [ver]` | 纳管系统已装版本（链接进 vsm，可 use/卸载，不动原目录） |
| `vsm mirror [cn\|off]` | 设置下载镜像（cn 国内加速 go/node/github / off 关闭） |
| `vsm proxy [url\|off\|system]` | 设置网络代理（http/https/socks5；off 强制直连 / system 跟随系统环境变量） |
| `vsm path-init` | [Windows] 把 shims 写入用户级 PATH 并广播（新终端免改配置） |

## 快速上手

```bash
vsm install go 1.22.0
vsm use go 1.22.0          # 当前目录用这个版本
vsm use -g node 20         # 全局默认 node 20.x
vsm current
vsm exec go version
```

## 激活（让 PATH 自动指向所选版本）

```bash
vsm reshim                 # 生成 shim
```

PowerShell：

```powershell
vsm activate powershell    # 输出把 shims 加入 PATH 的语句，可写进 $PROFILE
```

bash / zsh：

```bash
vsm activate bash >> ~/.bashrc
```

激活后，在配置了 `.tool-versions` 的目录里直接运行 `go` / `node`，会自动解析到对应版本。

## 数据目录

默认 `~/.vsm`，可用环境变量 `VSM_HOME` 覆盖（便于隔离测试）。

## 扩展更多语言

两种方式，**优先用 manifest（无需改代码、无需重新编译）**：

### 方式一：声明式 manifest（推荐）

往 `~/.vsm/plugins/`（或 `$VSM_HOME/plugins/`）放一个 `.json`，即可新增一门语言：

```json
[
  {
    "name": "deno",
    "source": { "type": "github-releases", "repo": "denoland/deno", "stripPrefix": "v" },
    "url": "https://github.com/denoland/deno/releases/download/v{version}/{asset}",
    "assets": {
      "windows/amd64": "deno-x86_64-pc-windows-msvc.zip",
      "linux/amd64":   "deno-x86_64-unknown-linux-gnu.zip",
      "darwin/arm64":  "deno-aarch64-apple-darwin.zip"
    },
    "bin": "",
    "env": {}
  }
]
```

字段说明：

- `source.type`：`github-releases` 或 `github-tags`；`stripPrefix` 去掉 tag 前缀（如 `v`）。
- `url` / `assets`：模板占位符 `{version}` `{os}` `{arch}` `{asset}`。
- `bin`：可执行所在子目录（空 = 安装根目录），用 `/` 分隔。
- `env`：要注入的环境变量，值里 `{dir}` 代表该版本安装目录（如 Java 的 `JAVA_HOME`）。

内置样板见 `internal/plugin/manifests/languages.json`，可复制的示例见 `examples/plugins/`。
这样要支持几十种语言，只是往 `plugins/` 目录加 JSON 而已。

### 方式二：Go 原生插件

当下载/版本逻辑需要特殊处理（如重定向、解析 assets）时，实现 `internal/plugin.Plugin`
接口并在 `init()` 中 `register`。参考 `golang.go` / `java.go` / `zig.go`。

## 路线图

- **已完成**：7 个语言插件（go / node / java / python / zig / deno / bun）、CLI 全流程、Windows GUI（Wails）。
- **已完成（增强）**：
  - 系统版本探测 `detect`（双路发现：扫全 PATH + 从 `JAVA_HOME`/`GOROOT`/各 `*_HOME` 环境变量定位；识别同一语言多版本、过滤 WindowsApps 占位、两路合并去重）。
  - 一键纳管 `adopt`（junction/symlink 链接系统版本，卸载只删链接、不动原目录）。
  - 镜像源加速 `mirror`（go/node/github 国内镜像，下载与元数据均走镜像）。
  - 网络代理 `proxy`（http/https/socks5，统一作用于所有下载与元数据请求；环境变量 `VSM_PROXY` 优先，`off` 强制直连 / `system` 跟随系统环境变量）。
  - `.tar.xz` 解压（调用系统 tar，零新增依赖；zig / typst 等 xz 发行包在 Windows/macOS/Linux 均可安装）。
  - 并行分块下载 + 进度百分比 + sha256 校验（go 已接入官方校验值）+ 失败重试。
  - shell hook 进目录自动切换（`activate` 安装 hook，`env` 输出当前目录环境）。
  - Windows 用户级 PATH 广播（`path-init`，新终端免改配置）。
  - 自动识别各生态惯用版本文件（`.nvmrc`/`.python-version`/`.ruby-version`/`.java-version`/`.go-version`
    + `go.mod`/`package.json` engines/`pyproject.toml`/`Gemfile`/`runtime.txt`），无需手写 `.tool-versions`。
  - 核心单元测试 + GitHub Actions（CI 跨平台编译、打 tag 自动 release）。
  - GUI：系统检测面板与一键纳管、镜像切换、安装进度条、当前目录 local 设置。
- **v1（待办）**：Lua 插件运行时（gopher-lua）外部扩展、`vsm shell`（会话级临时切换）、`.zst` 解压。
- **v2（待办）**：Bubble Tea TUI（`vsm ui`）、远程插件仓库。
