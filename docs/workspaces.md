# 可复现项目与图形工作台

本页说明 v1.1.0 开发版本的新能力。正式 v1.0.8 包不包含这些入口；测试时请使用同一预览包中的 CLI 和 GUI。

## 从一个项目开始

在已有 Go 项目中：

```bash
vsm project init --mode local
vsm project configure
vsm lock
vsm install --locked
vsm project trust
vsm tasks run test --locked
```

`configure` 从已有 `.tool-versions`、`go.mod` 等声明导入工具要求，并为 Go/Node 项目提供任务示例，不覆盖已有 `vsm.json`。先检查生成的配置及引用的环境文件，再执行 `trust`。

可提交 `vsm.json`、`vsm.lock`、`.vsm/project.json`；安装、venv、缓存及本机信任记录不需要提交。`vsm use` 在项目根目录存在 `vsm.json` 时更新其中的工具要求，否则保留原来的 `.tool-versions` 行为。

## vsm.json

```json
{
  "schema_version": 1,
  "tools": { "go": "1.25.11", "node": "22" },
  "env": { "APP_MODE": "development" },
  "env_files": [".env.local"],
  "tasks": {
    "vet": { "command": ["go", "vet", "./..."] },
    "test": {
      "description": "运行测试",
      "command": ["go", "test", "./..."],
      "env": { "APP_MODE": "test" },
      "timeout_seconds": 300
    },
    "check": { "depends": ["vet", "test"] },
    "build": {
      "command": ["go", "build", "-o", "build/app", "."],
      "depends": ["check"],
      "inputs": ["**/*.go", "go.mod"],
      "outputs": ["build/app"]
    }
  }
}
```

命令是参数数组，不隐式拼接 shell 字符串。需要 shell 时明确写 `bash`、`sh`、`powershell` 或 `cmd.exe` 及其参数。Windows 的 npm/npx 包装由 Node 直接执行，不依赖批处理参数拼接。

变量优先级为：继承环境 → dotenv 文件（按声明顺序）→ 项目 `env` → 任务 `env` / `run --env`。变量在 `run`、任务及受信任配置的 shell 激活中生效；父进程不被修改，离开目录时 shell hook 恢复原值。

dotenv 支持 `KEY=value`、行首注释、可选 `export` 与成对引号。当前按字面量读取，不进行 shell 执行、变量插值或多行表达式展开。引用文件必须存在且位于项目内，单文件最多 1 MiB。PATH、工具链控制变量、缓存隔离变量和动态链接器变量不能被项目 `env` 覆盖。

`dir` 可为任务指定项目内子目录，适合一个仓库的多个子项目。配置会拒绝重复 JSON 键、未知字段、依赖缺失、依赖循环、越界路径与危险变量。

## 锁定工具链

```bash
vsm lock
vsm lock --platform windows/amd64,darwin/arm64,linux/amd64
vsm lock --check --json
vsm lock --update --platform windows/amd64,darwin/arm64,linux/amd64
vsm install --locked
vsm run --locked -- go test ./...
vsm run --locked --install-missing -- go test ./...
```

锁文件记录原始要求、具体版本、安装定义摘要和每个平台的 URL、发布方 SHA256、可用大小与格式。普通 `lock` 保留已有构建，仅补充所选平台；`--update` 重新解析所选平台，应检查生成差异。未指定平台时使用当前平台。不会为了锁定而下载整个工具包。

锁定安装使用固定 URL/摘要，不重新查询远端版本列表。项目要求或安装定义改变、缺少当前平台、已安装构建来源不匹配时会明确失败。旧版安装没有来源记录时不能被静默当作锁定构建：请先卸载该版本，再 `install --locked`。外部纳管安装同样不能仅凭目录名认定为可复现安装。

缺少发布方 SHA256 时拒绝锁定，不自动降级。锁文件记录的是安装来源证据，不对用户之后修改的每个文件进行重新散列，也不包含应用依赖；仍需提交 `package-lock.json`、`uv.lock` 等生态锁文件。

Docker 模式继续通过指定镜像提供工具，工具包锁定只适用于 local/versions；容器镜像请使用内容摘要固定。

## 任务、并行与缓存

```bash
vsm tasks list --json
vsm tasks graph build --json
vsm tasks run build --dry-run
vsm tasks run check --jobs 4 --locked
vsm tasks run test --no-cache -- -count=1
vsm tasks run build --locked --json
```

依赖任务先完成；独立任务可并行，默认 4、最多 16 个。依赖只执行一次。任务失败会取消同批其它进程并阻止下游，保留退出码。默认任务超时 600 秒，可配置 1–86400 秒；取消和超时会终止进程树。GUI 支持停止任务，CLI 支持 Ctrl+C。

只有同时声明 `inputs` 与 `outputs`、且没有额外命令参数的任务会使用缓存。缓存键包含输入内容、任务、环境、项目配置、锁文件、平台和 VSM 版本，输出也按内容复核。`**` 支持递归文件匹配；递归扫描跳过 `.git`、`.vsm`、`node_modules`、`.venv`，并限制扫描条目、文件数和总读取大小。目录模式需明确选择文件，例如 `src/**/*.go`。

缓存是本机内容缓存，不是远程构建缓存。任务需要声明完整输入；未声明的文件、网络资源或外部状态变化无法自动推断，相关任务应省略缓存或使用 `--no-cache`。

人类可读进度写 stderr；`--json` 的 stdout 保持结构化结果。每个任务保留最多 64 KiB 输出并标记截断，GUI 显示最近的输出。任务输出可能包含该任务自行打印的值；诊断日志不记录输出正文或环境值。

## 配置信任

```bash
vsm project inspect --json
vsm project trust
```

信任绑定本机规范项目路径以及 `vsm.json`、所有引用环境文件的内容。变化后失效。任务和项目变量未信任时不会运行/应用，`list`、`graph`、`inspect` 可只读查看。只读概况返回环境变量名称，任务环境值会脱敏。

MCP 不提供授予信任的工具。AI 不能通过参数把未检查的配置标为已信任。本地执行项目代码仍具有当前用户权限，local 隔离不是操作系统沙箱；需要文件系统边界时使用已有 Docker 模式。

## GUI 项目工作台

侧栏「项目工作台」提供三个页面：

1. 工具链与配置：选择项目、生成/编辑配置、检查后信任、选择多个平台生成锁文件、按锁文件安装。
2. 项目任务：查看依赖，调整并发、锁定和缓存选项，实时输出并停止任务。
3. 工具目录：搜索已有工具、区分可安装与仅识别项、添加 GitHub 安装来源。

配置语法损坏时仍可进入编辑器修复；保存无效配置不会覆盖旧文件。CLI 与 GUI 请使用同一预览包，macOS 应用内置配套 CLI。

## GitHub 工具来源

```bash
vsm tools search github --json
vsm tools add ripgrep github:BurntSushi/ripgrep --command rg --match msvc
vsm install ripgrep latest --require-checksum
vsm use ripgrep v15.1.0
```

示例版本与筛选条件需要按实际平台调整；`msvc` 面向 Windows，跨平台项目可在安装前使用匹配所有目标的条件或自行配置 manifest。

按源站发布资产中的平台和架构选择归档或原生可执行文件，支持 `--bin` 子目录与 `--match` 资产关键词。元数据/摘要从官方 GitHub 获取；匹配不到、多个匹配或缺少发布资产 SHA256 时拒绝猜测。默认不选 musl、调试符号、安装器及校验文件，不执行第三方安装脚本。

这增加了通用 GitHub 安装能力，不代表每个 GitHub 项目、平台或历史版本都经过测试；复杂布局仍可使用显式 manifest。外部定义保存在 `VSM_HOME/plugins`，CLI 下次启动自动加载，GUI 添加后即时加载。

## Java 与 Zig

```bash
vsm install java 21
vsm install java temurin-21
vsm install java zulu-21
vsm use java zulu-21
vsm install java 21.0.12.1+1
```

Java 支持 Temurin、Zulu 和精确构建。Temurin 的国内镜像继续可用；Zulu 从 Azul 官方元数据和 CDN 取得包及摘要。锁定时保存精确 Java 构建而非仅保存大版本。Java 8 使用其实际 update/build 标识。Zig 也从官方索引读取包、大小和 SHA256，可用于严格安装与锁定。

## 从 mise 迁移工具版本

```bash
vsm project import --from mise
mise ls --current --json > mise-tools.json
vsm project import --from mise --file mise-tools.json
vsm project import --from mise --file mise-tools.json --apply
```

默认仅预览，`--apply` 创建新的 `vsm.json`，不覆盖已有配置。未指定文件时以 `MISE_SAFE=1` 调用 mise。仅导入工具的已解析版本，不复制任务、秘密变量或安装脚本；未知工具需要先登记安装来源，多当前版本需要先明确选择。

## MCP

普通模式 14 个工具，只读模式 7 个。新增项目概况、任务图、锁文件状态和目录查询，以及生成锁、锁定安装、运行受信任任务。`vsm_run` 支持 `locked`。所有操作仍绑定项目；MCP 安装只允许 local，命名任务不能自行授予信任。详见 [MCP](mcp.md)。

## 安装脚本

仓库 `scripts/install.sh` 和 `scripts/install.ps1` 从官方稳定 Release 安装 CLI，验证 SHA256，已有程序先备份，替换失败恢复。不自动改 PATH；按输出运行 `path-init`。可指定版本和安装目录。它们不是已被 Homebrew/winget 官方收录的包，当前仍以 GitHub Release 为发布来源。
