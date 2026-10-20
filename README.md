# vsm — 多语言版本管理器

`vsm`（Versatile Versions Manager）用一套工具统一管理 **Go / Node / Java / Python / Zig / Deno / Bun**
等多语言运行环境的安装、版本切换与激活。设计对标 asdf / mise / vfox，核心追求 **全链路零 CGO 的 Go 单体**：
一键交叉编译全平台、单二进制分发、内置版本切换。

> CLI 为纯标准库实现、**零外部依赖**，内置 **78 种语言/工具**：其中 17 种可直接安装
> （`go` `node` `java` `python` `zig` `deno` `bun` `gleam` `vlang` `kotlin` `tinygo` `nushell` `cmake` `ninja` `typst` `just` `uv`），
> 其余 50+ 种支持 **识别与版本切换**（涵盖 rust/dart/php/dotnet/elixir/swift/scala/elm/purescript/lean/solidity/maven/gradle/haskell/r/vala 等；
> 下载源可按需在 manifest 中补充即变为可安装）。另支持 `~/.vsm/plugins/*.json` 无限扩展、
> `vsm detect` 双路探测系统已装版本（PATH + 环境变量）、`vsm adopt` 一键纳管——不止管"自己装的"，更能接管机器上"已有的"。
> 打通 `install / use / list / current / which / exec` 主流程。
> 
> 跨平台桌面 GUI（`gui/`，Wails v2；**Windows / macOS / Linux**）：语言列表与安装/卸载、系统版本检测与一键纳管、
> 镜像切换、网络代理设置、安装进度条、当前目录 local 设置、一键写入用户级 PATH。
> **macOS 版为 Universal 通用二进制**（Apple Silicon + Intel），隐藏式标题栏 + 原生菜单，内置 CLI，一个 `.app` 开箱即用。

## v1.1.6 自动代理与国内下载源

- **优先本机代理**：自动读取 Windows/macOS 系统本机代理，并检查常见 HTTP/SOCKS5 端口，验证隧道可用后使用，保留手动设置。
- **无代理时国内源**：默认镜像模式 `auto`；更新包尝试国内下载服务，失败回退官方，始终校验官方 SHA256。
- **路径可见**：GUI 显示检测到的本机代理，下载进度标明代理、国内源或官方 CDN；代理出口的 GitHub API 限流可回退官方直连查询。

下载 [v1.1.6 正式版](https://github.com/hquip/Vaelen/releases/tag/v1.1.6)，或使用 GUI「检查 VSM 更新」/ `vsm update --gui`。首次升级由旧更新器执行，未识别代理时可先手动配置本机代理。详见[发布说明](docs/releases/v1.1.6.md)。

## v1.1.5 全局切换进度与忙碌提示修复

- **组件准备可见**：全局切换或激活 PATH 自动补装配套 CLI 时，底部持续显示连接、下载大小、速度、重试、分块与校验进度，完成后自动继续原操作。
- **避免重复操作**：切换与激活期间暂时禁用这两类按钮，界面重新渲染后仍保留进行中的状态，完成或失败后恢复。
- **准确提示**：不再把所有写操作误称为“安装/纳管任务”；配套 CLI 准备期间明确说明实际任务，日志新增 `cli.prepare` 起止与失败记录。

下载 [v1.1.5 正式版](https://github.com/hquip/Vaelen/releases/tag/v1.1.5)，或使用 GUI「检查 VSM 更新」/ `vsm update --gui`。Windows 手动下载时将同版本 CLI 与 GUI 放在同一目录；macOS Universal 应用内置 CLI。详见[发布说明](docs/releases/v1.1.5.md)。

## v1.1.4 更新下载优化

- **停滞重试**：更新包连接或响应体连续 15 秒无数据就重试；有官方资产 ID 时交替尝试官方下载地址与 GitHub 官方 API。
- **分块与续传**：8 MiB 以上资产探测 Range，支持时最多 4 连接下载，复用解析出的 CDN 地址；分块断流只补剩余字节。
- **明确进度**：GUI / CLI 显示连接、保留数据重试、并发数、官方接口及单连接回退，日志保留停滞原因。
- **完整性**：区间、长度和官方 SHA256 均须通过检查，校验失败或取消不替换旧程序。
- **Redis 探测**：增加只读 `-v` 备用参数，只接受 Redis 自身的版本输出；失败详情直接显示，日志包含程序路径。

下载 [v1.1.4 正式版](https://github.com/hquip/Vaelen/releases/tag/v1.1.4)，或使用 GUI「检查 VSM 更新」/ `vsm update --gui`。旧版执行本次升级仍使用旧更新器；若卡在下载，可从发布页手动下载 CLI 与 GUI，放在同一目录。详见[发布说明](docs/releases/v1.1.4.md)。

## v1.1.3 CLI 配套与版本执行修复

- **修复旧 CLI 误选**：识别同目录标准名和 GitHub 原始下载文件名，校验版本后使用，跳过 PATH 中的旧副本。缺少组件时，自动下载与 GUI 同版本的官方 CLI，校验后放到 GUI 旁边。
- **统一命令入口**：`vsm` 与语言 shim 使用同一份 CLI，避免旧 `vsm` 将 shim 写回旧路径。
- **验证实际执行**：全局设置前读取目标程序版本；激活检查实际执行未限定路径的命令，按 cmd / PATHEXT 规则对比输出，不再只凭 shim 文件存在报告成功。
- **简化界面**：移除终端激活弹窗和新开 cmd 按钮；点击后显示简短结果，路径详情默认折叠，日志保留 CLI 来源与实际版本。

Windows 手动下载时，将 GUI 和对应 CLI 放在同一目录，CLI 保持官方文件名也可使用；升级后点击一次「激活 PATH」更新命令入口。详见[发布说明](docs/releases/v1.1.3.md)。

## v1.1.2 Windows 终端自动激活

- 点击「激活 PATH」自动配置 cmd、PowerShell，按已配置工具生成 shims 并验证命令入口，无需手填安装路径。
- 已通过 VSM 的同一 cmd，后续全局切换无需重开；cmd 原有启动命令会保留，重复配置不累积。
- 自动显示版本、来源、命令入口和所选程序；缺失安装、当前目录的同名程序会明确提示。
- GUI 的旧 PATH 不再被当成其它终端的实际状态；配套 CLI 的超时、启动失败和版本不一致分别报告。

**从未加载 VSM 的已有 cmd 仍须加载一次初始化脚本**；GUI 无法直接改写其它进程的环境。详见[Windows 激活说明](docs/system-runtimes.md#windows-全局切换与-cmd)。

## v1.1.1 数据库实例与已有环境修复

- **数据库版本切换**：PostgreSQL、MySQL、MariaDB、Redis、Valkey，支持 Docker 官方镜像和已安装原生程序；GUI 左侧新增「数据库版本 / 实例」。
- **独立数据**：每个实例、版本独立保存数据，支持启动、停止、切换、日志、连接信息和客户端执行；切换失败尝试恢复旧版本。
- **全局切换**：修复旧 shim 和终端 PATH 优先级导致的无效切换，显示全局设置、项目覆盖和实际命令来源，补齐终端激活指引。
- **已有环境**：按工具准确扫描，支持手动添加路径，识别已有 Conda 环境及 Maven 完整安装，保留环境内原有依赖。
- **日志时间**：CLI 和 GUI 按本地时区显示，JSON 与导出保留 UTC。

```bash
vsm db use postgres 17-alpine --name dev --port 15432
vsm db start dev
vsm db switch dev 18-alpine
vsm db list --json
```

数据库切换会短暂重启服务，新版本使用独立数据；跨版本数据迁移需要另行备份、导入。原生方式需要完整的服务端与客户端程序；Docker 方式需要已启动的 Docker。详见[数据库说明](docs/databases.md)和[发布说明](docs/releases/v1.1.1.md)。

## v1.1.0 项目工作台

新增可复现工具链与项目自动化，CLI、GUI、MCP 共用核心：

- **工具链锁定**：`vsm.lock` 固定各平台的版本、安装定义与发布方摘要，`install --locked` / `run --locked` 拒绝静默使用不同构建。
- **项目配置**：`vsm.json` 管理工具、dotenv、项目变量和命名任务；本机内容信任在配置变更后失效。
- **任务执行**：依赖图、并行、超时/取消、退出码透传和输入/输出内容缓存。
- **GUI 工作台**：编辑配置、锁定和安装、运行/停止任务、实时输出、工具目录与 GitHub 来源添加。
- **安装来源**：通用 GitHub 发布包及原生可执行文件，唯一平台匹配与官方 SHA256；Temurin 精确构建、Zulu、Zig 官方摘要。
- **AI**：14 个 MCP 工具，其中 7 个只读；支持锁定工具与受信任任务，不能自行授予信任。
- **迁移与安装**：从 mise 版本 JSON 预览导入；Windows/Unix 安装脚本校验 SHA256 并保留旧程序。

```bash
vsm project init
vsm project configure
vsm lock
vsm install --locked
vsm project trust
vsm tasks run test --locked
```

先检查生成的配置，再执行 trust。具体能力与边界见[项目工作台说明](docs/workspaces.md)。

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

gui/                             # 跨平台桌面 GUI（Wails v2：Windows / macOS / Linux）
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

GUI 各平台构建 —— CLI 可零 CGO 一键交叉编译，但 GUI 用 Wails：macOS/Linux 依赖系统 WebView + CGO，**必须在对应平台本地编译**（不能从 Windows 交叉编译）；Windows GUI 用 go-webview2，免 CGO：

```powershell
# Windows（免 CGO，无需 wails CLI）
go build -tags production -ldflags "-H windowsgui" -o vsm-gui.exe ./gui
```

```bash
# macOS（在 Mac 上，需 Xcode 命令行工具）
CGO_ENABLED=1 go build -tags production -o vsm-gui ./gui

# Linux（需 GTK3 + WebKit2GTK 开发库）
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.0-dev
CGO_ENABLED=1 go build -tags production -o vsm-gui ./gui
```

### macOS GUI

Release 提供 **Universal 通用版** `vsm-gui-macos-universal.zip`（一个 `.app` 同时支持 Apple Silicon 与 Intel Mac），
已做 ad-hoc 签名。首次打开如提示「无法验证开发者」：右键 `VSM.app` → 打开，或执行

```bash
xattr -dr com.apple.quarantine /Applications/VSM.app
```

`VSM.app` **内置了 vsm CLI**（`VSM.app/Contents/MacOS/vsm`），拖到「应用程序」即可使用；
点界面顶部「⚡ 激活PATH」会把激活语句写进 `~/.zshrc`，之后新开的终端里 `go` / `node` 等即按目录自动切换。
想在终端直接敲 `vsm`，可再建一个软链：

```bash
sudo ln -sf /Applications/VSM.app/Contents/MacOS/vsm /usr/local/bin/vsm
```

自行打包（需在 Mac 上，产出与 Release 相同的通用版 zip）：

```bash
bash scripts/package-macos.sh 1.1.6 dist
```

> 打 `v*` tag 即可由 GitHub Actions 在 Windows / macOS / Linux 三个 runner 上分别本地编译，
> 自动产出**全平台 CLI + GUI** 并发布到 Release（见 `.github/workflows/release.yml`）。

## 命令

| 命令 | 说明 |
|------|------|
| `vsm install <tool> [version]` | 安装某版本（省略或 `latest` 装最新稳定版；支持模糊版本如 `20`） |
| `vsm uninstall <tool> <version>` | 卸载某版本 |
| `vsm use [-g] <tool> <version>` | 设为当前目录版本（写 `.tool-versions`）；`-g` 设为全局 |
| `vsm list [tool]` | 列出已安装版本（`*` 为当前生效） |
| `vsm list-all <tool>` | 列出可安装的远端版本 |
| `vsm current [--all]` | 显示已启用工具的版本及来源；`--all` 包含未启用项 |
| `vsm context [--json] [--check]` | Agent/AI 只读版本上下文：项目要求、解析/已激活版本、冲突和建议命令 |
| `vsm run [--install-missing] -- <command> [args...]` | 自动采用项目版本执行命令；可补装缺失工具，补装必须校验 SHA256 |
| `vsm project init [--mode local\|versions\|docker] [--image 镜像]` | 一个项目一个环境，默认本地隔离 |
| `vsm project status [--json]` | 查看项目环境和隔离模式 |
| `vsm mcp [serve\|config] --project <目录> [--read-only]` | MCP 服务或 AI 客户端配置，固定绑定项目；`config --client <客户端>` 输出对应 JSON / TOML |
| `vsm which <tool>` | 打印当前版本主命令的完整路径 |
| `vsm exec <tool> [args...]` | 在当前解析版本环境下执行工具主命令 |
| `vsm exec <tool> -- <cmd> [args...]` | 在该工具环境下执行任意命令（如 `npm`） |
| `vsm reshim` | 为已安装工具重新生成 shim |
| `vsm activate <shell>` | 输出 shell 激活脚本（含进目录自动切换 hook） |
| `vsm env [shell]` | 输出当前目录应生效的环境（供 hook 调用，一般不手动用） |
| `vsm detect` | 探测系统 PATH / 环境变量中已安装的语言运行时及版本 |
| `vsm adopt [--all] <tool> [ver]` | 纳管系统已装版本（链接进 vsm，可 use/卸载，不动原目录） |
| `vsm mirror [auto\|cn\|off]` | 自动优先代理、无代理时国内源；cn 固定国内，off 关闭镜像 |
| `vsm proxy [url\|off\|system]` | 设置网络代理（http/https/socks5；off 强制直连 / system 自动检测） |
| `vsm path-init` | Windows 写入用户级 PATH 和 PowerShell 启动钩子；macOS / Linux 写入当前 shell 的启动配置 |
| `vsm doctor [--json]` | 环境体检：shims 是否在 PATH、是否被同名命令抢占、PATH 重复、shim 残留、配置异常 |
| `vsm shell <tool> <ver>` | 打开临时子 shell 使用某版本（不写任何配置，`exit` 即恢复） |
| `vsm completion <shell>` | 输出 bash / zsh / fish / powershell 补全脚本 |
| `vsm size [tool] [--json]` | 统计各版本占用的磁盘空间 |
| `vsm clean` | 清理下载缓存（安装完成后的压缩包） |
| `vsm help [command [subcommand]]` | 查看总览或指定命令的详细用法、选项与示例；支持命令别名 |
| `vsm update [--check] [--gui]` | 从官方 GitHub 校验并更新 VSM；`--check` 只检查，`--gui` 同步相邻 GUI |

## 更新 VSM

从 v1.0.5 起，CLI 与 GUI 可从官方 GitHub Release 更新 VSM 自身：

```bash
vsm update --check
vsm update
vsm update --gui
```

GUI 左下角提供「检查 VSM 更新」。Windows / Linux GUI 更新时同时更新同目录的 CLI；macOS 应用内会整体更新 `VSM.app`，验证签名与内置 CLI。更新完成后重新启动程序。所有资源必须通过官方 SHA256 校验，替换失败时尝试恢复旧文件，不改项目和语言安装。

更新包达到 8 MiB 时探测 Range 支持，最多使用 4 连接并行下载；分块断流后只续传剩余字节。连接或响应体连续 15 秒无数据会触发重试。默认优先本机代理，无代理时尝试国内下载源，再回退官方；镜像返回损坏文件时回退官方并重新校验。`vsm mirror off` 禁用镜像。不支持分块时回退单连接；全部数据仍需通过同一官方 SHA256，未校验完成不替换程序。进度显示网络路径、重试、分块与回退状态。实际速度取决于网络和代理，此续传限本次更新任务。

旧于 v1.0.5 的程序尚无更新命令，需要先从 [Releases](https://github.com/hquip/Vaelen/releases/latest) 手动升级一次。程序所在目录需可写；macOS 应用请先移出只读磁盘映像。仍在使用的旧程序可能保留备份路径，重启后可按提示删除。详细参数见 `vsm help update`。

## 镜像、代理与终端显示

```bash
vsm mirror set github https://mirror.example/
vsm mirror set github-api https://api-mirror.example/
vsm mirror set node https://mirror.example/node/
vsm mirror set npm https://registry.example/
vsm mirror list --json
vsm proxy socks5h://127.0.0.1:1080
vsm proxy --json
vsm current --all
```

以上镜像域名为格式示例，请替换为自己的可用服务。`github` 将完整原始 URL 附加到代理前缀；`github-api`、`go`、`node`、`npm` 替换对应基地址。`npm` 也通过 `vsm run` 的子进程环境设置 registry，不改 `.npmrc`。`vsm mirror auto` 默认优先代理、无代理时使用国内源；`off` 禁用所有镜像，`cn` 使用内置规则，`custom` 使用自定义规则。可用 `vsm mirror unset <源>` 删除单项。VSM 更新版本元数据和发布方摘要始终来自官方，程序包允许国内源传输，不接受镜像提供的 SHA256。

代理支持 `http` / `https` / `socks5` / `socks5h`；不带参数查看当前配置。未配置时自动使用代理环境变量、系统本机代理或经过协议验证的常见本机端口；支持 `NO_PROXY`，不写系统代理设置。GUI 代理输入留空或点击「自动检测」即可启用。网络设置保存到全局 VSM 数据目录，隔离项目中的 CLI 和 GUI 同样生效。`VSM_PROXY` / `VSM_MIRROR` 环境变量优先。`current` 在可用终端中显示彩色工具/版本，重定向及 JSON 输出不插入颜色；`NO_COLOR` 或 `TERM=dumb` 禁用颜色。

`list` / `list-all` / `current` / `detect` / `doctor` / `size` / `clean` / `context` 均支持 `--json`，便于编辑器插件与 CI 集成。
`vsm exec` 会**原样透传子进程退出码**，可直接用于 `vsm exec go test ./... && deploy` 这类脚本。

查看单个命令的详细帮助：

```bash
vsm help install
vsm install --help
vsm help project init
vsm help mcp config
```

`help` 支持别名，例如 `vsm help i` 等同于 `vsm help install`。未知命令或子命令会给出提示并返回非零退出码。帮助可在项目配置损坏时查看，不会初始化环境或执行安装。`vsm run -- node --help`、`vsm exec node --help` 仍将帮助参数交给对应工具。

## 快速上手

```bash
vsm install go 1.22.0
vsm use go 1.22.0          # 当前目录用这个版本
vsm use -g node 20         # 全局默认 node 20.x
vsm current
vsm exec go version
```

## Shell 补全

```bash
eval "$(vsm completion bash)"                                   # bash（写进 ~/.bashrc 永久生效）
eval "$(vsm completion zsh)"                                    # zsh
vsm completion fish | source                                    # fish
vsm completion powershell | Out-String | Invoke-Expression      # PowerShell（写进 $PROFILE）
```

## 激活（让 PATH 自动指向所选版本）

```bash
vsm reshim                 # 生成 shim
```

最省事的方式——一条命令配置好新终端：

```bash
vsm path-init              # Windows：用户 PATH + cmd / PowerShell 钩子；macOS / Linux：~/.zshrc、~/.bashrc 等
```

macOS / Linux 也可以手动写进 shell 配置（用 `eval` 的动态写法，vsm 升级后无需重写）：

```bash
echo 'eval "$(vsm activate zsh)"'  >> ~/.zshrc     # zsh（macOS 默认）
echo 'eval "$(vsm activate bash)"' >> ~/.bashrc    # bash
```

```powershell
vsm path-init powershell  # 自动发现 Windows PowerShell / pwsh 配置位置，保留自定义提示符
vsm activate powershell | Out-String | Invoke-Expression # 立即激活已有 PowerShell
```

Windows 的系统 PATH 排在用户 PATH 前面；只写用户 PATH，可能仍命中系统 Java 等旧版本。「激活 PATH」/ `path-init` 会配置 cmd 和 PowerShell 启动钩子，把 VSM 放在系统工具之前，并实际执行工具核对版本。已经通过 VSM 的 cmd 后续全局切换即时生效，无需重开；从未加载 VSM 的已有 cmd 需加载一次初始化脚本。PowerShell 随提示符刷新；不修改执行策略。GUI 自动选择匹配的 CLI，缺少时下载同版本官方组件；不会直接使用 PATH 中的旧 CLI。详见[Windows 激活说明](docs/system-runtimes.md#windows-全局切换与-cmd)。

全局默认是兜底设置：项目的 `vsm.json` / `.tool-versions` 和 `VSM_<TOOL>_VERSION` 仍有更高优先级。GUI 详情会分别显示全局默认、VSM 选择和 GUI 进程 PATH 命中位置，不代表所有终端都已切换。CLI 与 GUI 应使用同一版本，并放在同一目录；切换全局版本会更新 shim，避免继续调用旧 CLI。

离开项目目录时，PATH 与 `GOROOT` / `JAVA_HOME` 等变量会自动还原，不会把上一个项目的版本带到别处。

激活后，在配置了 `.tool-versions` 的目录里直接运行 `go` / `node`，会自动解析到对应版本。

## 数据库版本与实例

支持 PostgreSQL、MySQL、MariaDB、Redis、Valkey 的 **Docker / 原生**版本切换，按实例和版本独立保存数据。GUI 左侧打开「数据库版本 / 实例」。

```bash
vsm db use postgres 17-alpine --name api-pg --port 15432
vsm db start api-pg
vsm db switch api-pg 18-alpine
vsm db connection api-pg
```

原生方式先纳管完整数据库程序，再使用 `--backend native`。切回旧版本恢复原数据，切换失败尝试恢复旧服务；跨版本数据迁移单独处理。详见[数据库版本切换](docs/databases.md)。

## Agent / AI 与切换提示

系统检测支持已有 Conda Python 环境和手动添加安装路径；纳管后复用扫描结果，单语言详情不会混入其他工具。详见[已有运行时与 Conda](docs/system-runtimes.md)。

新增 MCP 与三种项目环境：见 [MCP 接入](docs/mcp.md) 和 [项目隔离](docs/project-environments.md)。GUI 的「隔离环境 / MCP」可初始化环境并生成配置。

```bash
vsm project init                     # 独立工具、缓存、Python venv
vsm mcp config --project /path/to/app # 生成 MCP 客户端配置
```

执行 `vsm context --json` 获取项目要求、版本来源、解析版本、shell 已激活版本以及建议命令；该命令不安装工具、不修改项目和终端环境。
AI 可直接执行 `vsm run -- go test ./...`、`vsm run -- node app.js`，自动选择项目声明的工具版本；`vsm run --install-missing -- go test ./...` 还会补装缺失工具。
`run` 的项目声明优先于继承的版本覆盖，作用于这次命令及其子进程，并透传退出码；不会修改项目声明或父终端。
CI 或 Agent 可用 `vsm context --json --check`：存在缺失版本、覆盖冲突或尚未激活时返回非零状态。

需要看到版本变化时，激活 shell hook 后设置 `VSM_NOTIFY=1`（bash/zsh：`export VSM_NOTIFY=1`；PowerShell：`$env:VSM_NOTIFY='1'`）。
仅版本组合实际变化时向 stderr 提示，重复进入同一环境不会重复提示；默认保持静默。
完整示例见 [Agent 接入说明](docs/agent-integration.md)。

## 下载与目录安全

`vsm install go 1.25.11 --require-checksum` 要求发布方提供 SHA256；也可设置 `VSM_REQUIRE_CHECKSUM=1` 对 CLI 和 GUI 统一启用。
GUI 安装区提供「要求发布方 SHA256 校验」选项。Go、Node 从官方元数据获取校验值，GitHub Release 使用可用的资产摘要，manifest 支持固定摘要。
获取校验元数据失败、摘要不匹配时停止安装；默认模式遇到发布方没有摘要会明确提示，严格模式会拒绝安装。

解压使用受目录边界约束的文件操作，拒绝路径穿越、越界链接、设备文件与重复文件，并限制展开大小与文件数量。
卸载自定义安装时同时验证 registry 侧记录和安装目录中的随机令牌；纳管目录始终只删除链接。
完整边界、限制与兼容性说明见 [安全设计](docs/security.md)。

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

## JDK 下载加速

Java 使用 Adoptium 官方元数据中的具体 Temurin 安装包、大小和 SHA256；开启国内镜像后直接下载清华 Adoptium 镜像，避免先经过 Adoptium 再跳转 GitHub：

```bash
vsm mirror cn
vsm install java 21 --require-checksum
```

GUI 顶部选择「镜像：国内」后安装 JDK，行为相同。镜像缺包、返回错误或校验不符时，回退同一官方文件，始终使用原发布方 SHA256。需要自建镜像可用 `vsm mirror set java https://mirror.example/Adoptium/`，目录结构为 `<major>/jdk/<arch>/<os>/<filename>`；`vsm mirror off` 关闭镜像。

大包在服务器支持 Range 且有稳定 ETag 或官方 SHA256 时使用 4 连接并行下载。HEAD 不可用时用 1 字节 Range 探测；断流仅续传失败分块的剩余字节，成功分块保留，进度包含速度。此续传发生在本次下载任务内，尚不支持关闭程序后继续。源站不支持 Range 时自动回退单连接。实际速度仍取决于网络和镜像限速。

## 本地运行日志

CLI、GUI 和 MCP 统一记录操作、下载重试与失败原因，文件自动轮换（最多约 8 MiB）。
GUI 左下角「日志」支持筛选、导出和清理。

```bash
vsm logs --tail 100
vsm logs --level error
vsm logs export vsm-report.jsonl
vsm logs clear
```

默认保存在 `~/.vsm/logs`（或 `VSM_HOME/logs`），通过 `VSM_LOG_LEVEL=debug|info|warn|error|off` 控制记录级别。
日志保存在本地，代理凭据、URL 查询参数与常见密钥会脱敏。详见[日志说明](docs/logging.md)。

## 许可证

[Apache 2.0](LICENSE)

## 路线图

- **已完成**：7 个语言插件（go / node / java / python / zig / deno / bun）、CLI 全流程、桌面 GUI（Wails）。
- **已完成（增强）**：
  - 系统版本探测 `detect`（双路发现：扫全 PATH + 从 `JAVA_HOME`/`GOROOT`/各 `*_HOME` 环境变量定位；识别同一语言多版本、过滤 WindowsApps 占位、两路合并去重）。
  - 一键纳管 `adopt`（junction/symlink 链接系统版本，卸载只删链接、不动原目录）。
  - 镜像源加速 `mirror`（go/node/github 国内镜像，下载与元数据均走镜像）。
  - 网络代理 `proxy`（http/https/socks5，统一作用于所有下载与元数据请求；环境变量 `VSM_PROXY` 优先，`off` 强制直连 / `system` 跟随系统环境变量）。
  - `.tar.xz` 解压（调用系统 tar，零新增依赖；zig / typst 等 xz 发行包在 Windows/macOS/Linux 均可安装）。
  - 并行分块下载 + 进度百分比 + SHA256 校验（Go / Node 官方校验值、GitHub 资产摘要、manifest 固定摘要）+ 失败重试。
  - shell hook 进目录自动切换（`activate` 安装 hook，`env` 输出当前目录环境）。
  - Windows 用户级 PATH 广播及 PowerShell 启动钩子（`path-init`）。
  - 自动识别各生态惯用版本文件（`.nvmrc`/`.python-version`/`.ruby-version`/`.java-version`/`.go-version`
    + `go.mod`/`package.json` engines/`pyproject.toml`/`Gemfile`/`runtime.txt`），无需手写 `.tool-versions`。
  - 核心单元测试 + GitHub Actions（CI 跨平台编译、打 tag 自动 release）。
  - GUI：系统检测面板与一键纳管、镜像切换、安装进度条、当前目录 local 设置。
- **已完成（v1.0.1）**：`vsm doctor` 环境体检、`vsm shell` 会话级临时切换、shell 补全、`--json` 输出、`vsm size` / `vsm clean`、`vsm exec` 退出码透传、macOS 原生 GUI（Universal，内置 CLI）、`vsm path-init` 支持 macOS / Linux。
- **已完成（v1.0.2）**：MCP stdio 服务与 7 个 AI 工具、每项目独立环境（`local` / `versions` / `docker`）、Python venv、GUI「隔离环境 / MCP」入口，并修复 Go 版本探测与项目版本约束。详见 [发布说明](docs/releases/v1.0.2.md)。
- **已完成（v1.0.3）**：多 AI 客户端配置预设与只读模式、项目选择和使用引导、Docker 镜像工具视图，以及 Python venv、预发布版本选择和 MCP 安装超时修复。详见 [发布说明](docs/releases/v1.0.3.md)。
- **已完成（v1.0.4）**：`help <command>` 详细帮助、嵌套子命令帮助、命令别名与 `-h` / `--help`，以及帮助主题的 shell 补全。详见 [发布说明](docs/releases/v1.0.4.md)。
- **已完成（v1.0.5）**：GitHub 自更新、Python 元数据 EOF 修复、自定义来源镜像、代理/镜像 JSON 查看，以及精简和彩色 `current` 输出。详见 [发布说明](docs/releases/v1.0.5.md)。
- **已完成（v1.0.6）**：CLI / GUI / MCP 统一本地运行日志、日志筛选与导出、自动轮换与敏感信息脱敏。详见 [发布说明](docs/releases/v1.0.6.md)。
- **已完成（v1.0.7）**：GitHub 自更新显示实时下载进度、速度、整体大小、重试与校验/替换阶段。详见 [发布说明](docs/releases/v1.0.7.md)。
- **已完成（v1.0.8）**：JDK 清华镜像与官方回退、官方元数据固定 SHA256、4 连接分块探测与任务内续传、下载速度显示，以及 macOS Java 路径修复。详见 [发布说明](docs/releases/v1.0.8.md)。
- **v1（待办）**：Lua 插件运行时（gopher-lua）外部扩展、`.zst` 解压。
- **v2（待办）**：Bubble Tea TUI（`vsm ui`）、远程插件仓库。
