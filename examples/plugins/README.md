# 自定义语言 manifest 示例

把本目录里的 `.json` 复制到 `~/.vsm/plugins/`（或设置了 `VSM_HOME` 时为 `$VSM_HOME/plugins/`），
启动 `vsm` 时会自动加载，**无需重新编译**即可新增语言。

```bash
mkdir -p ~/.vsm/plugins
cp sample.json ~/.vsm/plugins/
vsm list            # 即可看到新增的语言
vsm install deno latest
```

## manifest 字段

| 字段 | 说明 |
|------|------|
| `name` | 工具名（命令名，如 `deno`） |
| `source.type` | `github-releases` 或 `github-tags` |
| `source.repo` | `owner/repo` |
| `source.stripPrefix` | 去掉的 tag 前缀（如 `v`、`bun-v`） |
| `url` | 下载 URL 模板，占位 `{version}` `{os}` `{arch}` `{asset}` |
| `assets` | `"os/arch"`（如 `windows/amd64`）→ asset 名模板 |
| `bin` | 可执行所在子目录（空 = 安装根目录），用 `/` 分隔 |
| `env` | 注入的环境变量，值里 `{dir}` = 该版本安装目录 |

`os` 取值：`windows` / `linux` / `darwin`；`arch` 取值：`amd64` / `arm64` / `386`。

## 说明

- `sample.json` 里的 `deno` / `bun` 与内置实现等价，放在这里作为**格式参考**，可照此为任意
  使用 GitHub Releases 分发预编译包的语言编写 manifest。
- 解压目前支持 `.zip` 与 `.tar.gz`；`.tar.xz` 暂未支持（不影响 Windows 目标）。
- 若安装报 404，多半是 `assets` 里的命名与该项目实际 release 文件名不一致，按官方
  release 页面调整即可。
