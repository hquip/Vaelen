# 一个项目，一个环境

在项目根目录运行 `vsm project init`，默认启用 `local`。配置保存在 `.vsm/project.json`；运行时数据保存在项目自己的 `.vsm/` 下。

| 模式 | 工具与依赖 | 运行位置 |
| --- | --- | --- |
| `versions` | 工具安装共享，版本声明按项目选择 | 宿主机子进程 |
| `local`（默认） | 独立安装目录、工具缓存、Python venv | 宿主机子进程 |
| `docker` | 使用项目指定镜像中的工具，检查是否满足版本要求；缓存保存在该项目 | Docker 容器 |

## 本地隔离

```bash
cd my-app
vsm project init
vsm use go 1.25.11
vsm run --install-missing -- go test ./...
```

初始化后，普通 `install`、`list`、`use`、`run` 和 `context` 自动识别项目环境。项目内不读取父项目的版本声明，`local` 模式也不继承全局默认语言版本。

普通数字前缀、`latest` 和未声明预发布版本的范围默认选择正式版。需要 rc / beta 时应明确声明对应版本；版本比较忽略 SemVer 构建元数据，并将预发布版本排在对应正式版之前。

```text
my-app/
  .tool-versions
  .vsm/
    project.json
    .gitignore
    env/
      installs/<tool>/<version>/
      downloads/
      cache/
      go/
      node/
      cargo/
      gradle/
      venvs/python-<version>/
```

项目运行会指定独立 Go 的 GOPATH/GOBIN/构建及模块缓存、npm 缓存与全局前缀、pip 缓存、Cargo 和 Gradle 用户目录，并屏蔽继承的 Python/Node 扩展搜索路径。
Node 常规依赖仍位于该项目的 `node_modules`。其它语言仍使用其自身的项目依赖机制；这里不声称隔离每一种第三方包管理器的所有配置。

Python 在第一次 `vsm run` 前使用项目安装的解释器建立版本对应的 venv：

```bash
vsm use python 3.12.10
vsm run --install-missing -- python -m pip install -r requirements.txt
vsm run -- python app.py
```

解释器需要包含标准 `venv` / `ensurepip`。缺少这些组件时明确报错；不回退到系统 Python 安装依赖。`PIP_REQUIRE_VIRTUALENV=true` 防止误装到非 venv 解释器。

已有 venv 如果缺少 Python 入口，VSM 会停止运行并显示环境路径；不会改用基础解释器。使用项目安装的同版本 Python，针对显示的 venv 路径重新运行 `-m venv <环境路径>` 可修复入口。VSM 不会自动删除已有依赖环境。

本地项目不纳管指向系统的共享运行时、不允许 `--path` 指向项目外，也不会将项目 shims 持久写入系统 PATH。若已经配置全局 shell hook，它会按当前目录计算项目版本；准备 Python venv 的推荐入口是 `vsm run`。

## 只隔离版本

```bash
vsm project init --mode versions
vsm use node 22
vsm run -- node app.js
```

此模式共享已安装工具，不提供独立依赖缓存。适合希望节省磁盘空间的项目。

## Docker 隔离

```bash
vsm project init --mode docker --image golang:1.25.11
vsm use go 1.25.11
vsm run -- go test ./...
```

需要安装并启动 Docker Desktop / Docker Engine。镜像必须已经包含项目要求的语言工具；多语言项目使用相应的自定义镜像。VSM 验证镜像中的版本，再在同一个内容寻址镜像内执行，不会向宿主机安装工具。

Docker 项目的 GUI 使用镜像工具视图，可直接保存语言版本要求；不提供宿主机安装/纳管入口。GUI、CLI 和底层安装器统一拒绝在 Docker 模式安装或纳管宿主机工具。GUI 不会把项目中遗留的宿主机安装目录显示成镜像内的版本。

容器只挂载当前项目到 `/workspace`，可写项目缓存位于 `.vsm/docker-cache/`；根文件系统只读、丢弃 capabilities、禁止提权并限制进程数，不挂载 Docker socket。网络按 Docker 默认设置提供，项目目录仍可写；这不是对所有恶意代码的完整安全保证。

## 查看与迁移

```bash
vsm project status --json
vsm context --json
vsm mcp config --project /path/to/my-app
```

已初始化的项目不能被同一命令静默改成另一模式。需要迁移时，先确认并备份所需依赖/缓存，再在新的目录初始化目标模式；当前版本不提供自动搬迁或自动删除旧环境。
可提交 `.vsm/project.json` 和 `.tool-versions`；`.vsm/.gitignore` 会忽略运行时数据。配置不保存机器的绝对路径。
