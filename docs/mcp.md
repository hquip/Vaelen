# MCP：让 AI 管理项目工具链

VSM 提供 stdio MCP 服务，直接使用 vsm CLI，不需要额外 Go 依赖或网络监听端口。实现遵循 [MCP stdio](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports) 与 [工具调用协议](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)。

先生成当前可执行文件的客户端配置：

```bash
vsm mcp config --project /absolute/path/to/project
```

Windows 示例：

```json
{
  "mcpServers": {
    "vsm": {
      "command": "C:\\tools\\vsm.exe",
      "args": ["mcp", "serve", "--project", "D:\\work\\my-app"]
    }
  }
}
```

把生成的内容加入支持此配置格式的 AI 客户端。GUI 的「隔离环境 / MCP」也可以生成并复制配置。使用 `--read-only` 启动时仅提供三个只读工具。

## 多客户端接入

GUI 提供 6 类客户端预设与通用预设，按客户端生成 JSON 或 TOML，并显示保存位置和连接步骤。所有连接均绑定所选项目；可选只读模式。生成配置不会修改任何 AI 客户端的设置。

| `--client` | 客户端 | 格式与保存位置 |
| --- | --- | --- |
| `cursor` | Cursor | JSON，`mcpServers`；项目 `.cursor/mcp.json` |
| `claude-desktop` | Claude Desktop | JSON，`mcpServers`；开发者设置中的 `claude_desktop_config.json` |
| `claude-code` | Claude Code | JSON，`mcpServers`；项目 `.mcp.json` |
| `codex` | Codex CLI / IDE / 桌面 | TOML，`[mcp_servers.vsm]`；项目 `.codex/config.toml`（受信任项目）或 `~/.codex/config.toml` |
| `vscode` | VS Code / GitHub Copilot | VS Code 专用 JSON，`servers`；项目 `.vscode/mcp.json` |
| `windsurf` | Windsurf / Cascade | JSON，`mcpServers`；使用客户端的 Open MCP config file 入口 |
| `generic`（CLI 默认） | 接受通用格式的 stdio 客户端 | JSON，`mcpServers`；按客户端说明合并 |

```bash
vsm mcp config --project /path/to/app --client cursor
vsm mcp config --project /path/to/app --client codex
vsm mcp config --project /path/to/app --client vscode --read-only
```

将生成内容中的 `vsm` 项或 `[mcp_servers.vsm]` 段合并到现有配置，再重新连接客户端。已有同名项应更新，避免重复定义。默认提供 7 个工具；只读模式仅提供 `vsm_context`、`vsm_project_status` 和 `vsm_list`，不允许初始化、安装、切换版本或运行命令。

Codex 预设将工具调用超时设置为 600 秒，以覆盖安装和测试；VSM 自身的请求超时仍然生效。VS Code 新版也支持项目根目录 `.mcp.json` 的便携 `mcpServers` 格式，可使用 `generic` / `claude-code` 输出；`vscode` 预设保留专用格式以兼容已有配置。Windsurf/Cascade 的配置路径随版本变化，以客户端打开的实际文件为准。

预设依据官方文档：[Cursor](https://cursor.com/docs/mcp)、[Claude Desktop](https://modelcontextprotocol.io/docs/develop/connect-local-servers)、[Claude Code](https://code.claude.com/docs/en/mcp)、[Codex](https://developers.openai.com/codex/mcp)、[VS Code](https://code.visualstudio.com/docs/agents/reference/mcp-configuration)、[Windsurf / Cascade](https://docs.windsurf.com/windsurf/cascade/mcp)。本地预设面向支持 stdio 的客户端；仅接受远程 HTTP 服务的客户端需要相应的远程服务，当前 VSM 未提供该传输。

## 在 GUI 中使用

1. 点击侧栏「隔离环境 / MCP」，在弹窗中点击「选择项目目录」或「更换项目目录」，选择代码所在的文件夹。选目录不会初始化环境。
2. 选择环境模式，点击「为此项目初始化环境」。默认本地隔离会为项目准备独立工具目录和缓存；Docker 模式还需要填写包含所需工具的镜像。
3. 初始化成功后，点击「去选择语言和版本」，从左侧选择语言、安装版本并「设为本目录」。在项目终端中通过 `vsm run -- <命令>` 使用该环境。
4. 需要 AI 接入时，在同一弹窗选择客户端及是否只读，点击「生成 JSON / TOML 配置」和「复制配置」，按显示的保存位置与说明合并配置并重新连接，即可让 AI 读取项目要求并执行命令。

Windows / Linux 用户需将对应平台的新版 CLI 重命名为 `vsm.exe` / `vsm`，放在 GUI 同目录，或确保它可通过 PATH 找到。macOS 应用已内置 CLI。GUI 会检查所选 CLI 能否生成 MCP 配置；旧版或缺失时会在弹窗中提示更换，不会生成无法连接的配置。

## 工具

| 工具 | 行为 |
| --- | --- |
| `vsm_context` | 读取项目要求、解析版本、激活记录、冲突和建议命令 |
| `vsm_project_status` | 查看项目是否初始化、隔离模式和镜像 |
| `vsm_list` | 查看当前项目作用域内已安装版本 |
| `vsm_project_init` | 初始化 `local`、`versions` 或 `docker` 环境，默认 `local` |
| `vsm_use` | 写入绑定项目的 `.tool-versions`，只接受具体版本、数字前缀或 `latest` |
| `vsm_install` | 在 `local` 项目中安装工具，强制校验发布方 SHA256 |
| `vsm_run` | 在项目环境运行命令，可指定参数、补装和 1–600 秒超时 |

建议 AI 的工作顺序：读取 `context` → 确认/初始化隔离模式 → 设置项目版本 → 安装缺失工具 → 执行 `run`。`versions` 模式不会通过 MCP 向共享安装目录自动安装工具。

## 调用示例

```json
{"name":"vsm_project_init","arguments":{"mode":"local"}}
```

```json
{"name":"vsm_use","arguments":{"tool":"go","version":"1.25.11"}}
```

```json
{"name":"vsm_run","arguments":{"command":"go","args":["test","./..."],"install_missing":true,"timeout_seconds":120}}
```

工具结果同时提供文本和结构化数据，包含退出码、stdout、stderr、项目路径及截断标记。命令失败通过 `isError` 呈现；工具输出不会污染 MCP 协议。

## 边界

- 每个 MCP 进程绑定一个明确目录，调用参数不能把管理操作重定向到其它项目。
- MCP 不提供全局设置、卸载、写系统 PATH 或任意安装根目录的工具；安装必须位于本地项目环境。
- 命令参数按数组传递，不拼成隐式 shell 字符串。显式调用 shell 会执行传入的脚本，与普通执行代码具有相同权限。
- `run` 会执行项目代码。`local` 模式隔离工具、缓存和 Python 依赖环境，不限制代码访问当前用户可访问的其它文件；需要文件系统边界时选 `docker`。
- 输入、输出、并发请求和运行时长有上限。超时/取消会终止进程树；Docker 调用按该请求的专属标签清理容器。
- 这是 stdio 服务；未提供 HTTP 服务或远程认证接口。

完整隔离方式见 [项目环境](project-environments.md)。
