// Package mcpconfig renders VSM's stdio connection for supported AI clients.
// It generates text only; it never writes or replaces client settings.
package mcpconfig

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Client struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Format       string `json:"format"`
	Location     string `json:"location"`
	Instructions string `json:"instructions"`
}

type Configuration struct {
	Client  Client `json:"client"`
	Content string `json:"content"`
}

// Clients returns save locations as instructions, not paths to be written.
func Clients(platform string) []Client {
	desktopLocation := "Claude Desktop → 设置 → 开发者 → 编辑配置"
	switch platform {
	case "windows":
		desktopLocation = `%APPDATA%\Claude\claude_desktop_config.json`
	case "darwin":
		desktopLocation = "~/Library/Application Support/Claude/claude_desktop_config.json"
	}
	return []Client{
		{"cursor", "Cursor", "json", "项目/.cursor/mcp.json", "将 vsm 项合并到 mcpServers，保存后在 Cursor 的 MCP 设置中启用，查看可用工具。"},
		{"claude-desktop", "Claude Desktop", "json", desktopLocation, "在设置 → 开发者 → 编辑配置中，将 vsm 项合并到 mcpServers。完全退出并重新启动客户端，再检查可用工具。"},
		{"claude-code", "Claude Code", "json", "项目/.mcp.json", "将 vsm 项合并到 mcpServers，从此项目重新启动 Claude Code，使用 /mcp 查看连接状态。"},
		{"codex", "Codex（CLI / IDE / 桌面）", "toml", "项目/.codex/config.toml（受信任项目）或 ~/.codex/config.toml", "将 [mcp_servers.vsm] 段合并到配置文件；已有同名段请更新该段。重新连接客户端，可使用 codex mcp list 检查。"},
		{"vscode", "VS Code / GitHub Copilot", "json", "项目/.vscode/mcp.json", "将 vsm 项合并到 servers。运行 MCP: List Servers，启动 vsm，再在 Agent 的工具列表中启用。"},
		{"windsurf", "Windsurf / Cascade", "json", "从 Cascade 的 MCP 设置中打开 MCP 配置文件", "使用客户端的 Open MCP config file 入口，将 vsm 项合并到 mcpServers，保存并刷新/启用服务。配置位置以当前客户端打开的文件为准。"},
		{"generic", "通用 JSON（mcpServers）", "json", "AI 客户端的 MCP 配置文件", "适用于接受 mcpServers、command 和 args 的本地 stdio MCP 客户端。将 vsm 项合并到现有配置，按客户端说明重新连接。"},
	}
}

func Generate(clientID, platform, executable string, args []string) (Configuration, error) {
	if clientID == "" {
		clientID = "generic"
	}
	var client Client
	for _, candidate := range Clients(platform) {
		if candidate.ID == clientID {
			client = candidate
			break
		}
	}
	if client.ID == "" {
		return Configuration{}, fmt.Errorf("未知 MCP 客户端 %q；支持 cursor、claude-desktop、claude-code、codex、vscode、windsurf、generic", clientID)
	}
	if executable == "" {
		return Configuration{}, fmt.Errorf("MCP executable is required")
	}
	if client.Format == "toml" {
		quoted := make([]string, len(args))
		for i, arg := range args {
			quoted[i] = quoteTOML(arg)
		}
		return Configuration{Client: client, Content: "[mcp_servers.vsm]\ncommand = " + quoteTOML(executable) +
			"\nargs = [" + strings.Join(quoted, ", ") + "]\ntool_timeout_sec = 600\n"}, nil
	}
	server := map[string]any{"command": executable, "args": args}
	key := "mcpServers"
	if client.ID == "vscode" {
		key = "servers"
	}
	if client.ID == "cursor" || client.ID == "vscode" || client.ID == "claude-code" {
		server["type"] = "stdio"
	}
	data, err := json.MarshalIndent(map[string]any{key: map[string]any{"vsm": server}}, "", "  ")
	if err != nil {
		return Configuration{}, err
	}
	return Configuration{Client: client, Content: string(data) + "\n"}, nil
}

// TOML basic strings require escaping control characters, including DEL, and
// do not accept Go's \xNN escapes. Paths and arguments must remain literal.
func quoteTOML(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
