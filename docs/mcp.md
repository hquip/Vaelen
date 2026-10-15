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
