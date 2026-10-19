# 数据库版本切换

VSM 支持 PostgreSQL、MySQL、MariaDB、Redis、Valkey 的具名实例，提供 Docker 和本机原生两种运行方式。GUI 从左侧「数据库版本 / 实例」进入，CLI 使用 `vsm db`（别名 `vsm database`）。

## 两种运行方式

| 方式 | 程序来源 | 数据位置 | 前提 |
| --- | --- | --- | --- |
| Docker | 官方镜像，首次启动拉取后记录镜像 ID | 按实例与版本创建的具名 Docker 卷 | 已安装并启动 Docker Desktop / Engine，使用 Linux 容器及对应架构可用的镜像 |
| 原生 native | 用户已经安装并纳管的完整服务端和客户端程序 | `VSM_HOME/databases/instances/<实例>/versions/native-<版本>/data` | 对应平台的数据库发行包；Linux/macOS 也可纳管系统包管理器安装的程序 |

CLI 保持纯 Go、零 CGO；运行数据库所需的 Docker 或原生程序按所选方式准备。Windows 的 Redis / Valkey 推荐 Docker；原生方式需要兼容的服务端和客户端程序。

数据库实例按名称保存在全局 VSM_HOME 下，独立于项目的 local / versions / docker 环境。不同项目可使用不同实例名、端口。实例只监听本机 `127.0.0.1`。

## Docker 示例

```bash
# 保存实例配置，此时不启动服务
vsm db use postgres 17-alpine --name api-pg --port 15432
vsm db start api-pg

# 使用所选版本的客户端执行 SQL
vsm db exec api-pg -- -c "SELECT version()"
vsm db exec api-pg -- -c "CREATE TABLE example (id INT PRIMARY KEY)"

# 运行中切换会停止旧容器并启动新版本，保留端口和连接凭据
vsm db switch api-pg 18-alpine
vsm db switch api-pg 17-alpine
vsm db stop api-pg
```

其它数据库示例：

```bash
vsm db use mysql 8.4 --name api-mysql --port 13306
vsm db use mariadb 11.4 --name api-maria --port 13307
vsm db use redis 7.4-alpine --name cache --port 16379
vsm db use valkey 8-alpine --name queue --port 16380
vsm db start cache
vsm db exec cache -- SET hello world
vsm db exec cache -- GET hello
```

版本使用明确的数字标签，例如 `17`、`17.9`、`8.4`、`7.4-alpine`；不使用浮动的 `latest`。首次拉取后按镜像 ID 运行，重启同一版本不重新解析浮动标签。启动前会检查程序实际报告的版本。

首次 Docker 拉取显示层下载进度；CLI 的进度写入 stderr，`--json` 输出保持可解析。GUI 通过操作输出显示进度，可取消运行中的操作。

## 原生示例

准备完整数据库发行包后纳管程序，再创建实例：

```powershell
vsm adopt mysql --path 'D:\tools\mysql-8.4'
vsm db versions mysql
vsm db use mysql 8.4 --backend native --name local-mysql --port 13306
vsm db start local-mysql
vsm db exec local-mysql -- -e "SELECT VERSION()"
```

```bash
# PostgreSQL 示例，路径以本机实际安装位置为准
vsm adopt postgres --path /usr/lib/postgresql/17
vsm db use postgres 17 --backend native --name local-pg --port 15432
vsm db start local-pg
vsm db exec local-pg -- -c "SELECT version()"
```

原生模式需要数据库的配套工具，例如 PostgreSQL 的 initdb / pg_ctl / psql、MySQL 的 mysql / mysqladmin。GUI 可选择完整目录直接纳管并选择版本。原生程序不匹配请求版本、或已初始化后被替换成不同版本时，会拒绝复用该数据目录。

VSM 创建自己的实例和数据目录；选择已有安装目录仅复用程序。不会接管系统正在运行的数据库服务及其原数据。端口已占用时会报错，需选择其它端口。服务使用当前用户运行，不注册系统服务。

## 数据与切换行为

- 一个实例的每个版本独立保存数据。首次切到新版本使用新数据；切回旧版本时使用它原来的数据。
- 停止、切换和重建容器保留数据；没有自动删除数据库数据的命令。
- 运行中切换会短暂重启服务，已有连接会断开。启动失败或取消切换时尝试恢复原版本；恢复失败会明确报告。
- 跨版本数据升级、导出/导入和备份需要单独完成。本功能不会用新版本直接打开旧版本的数据目录。
- 原生初始化被中断时保留现场与日志；不覆盖已有但无法确认完成状态的数据。
- 已有数据目录、数据卷或凭据丢失时明确报错，不创建空库冒充原来的数据。锁定镜像被外部清理时需恢复该镜像；不会自动换成同标签的另一构建来打开已有数据。

数据库服务通过 `db switch` 或 GUI 明确切换。普通 `vsm use` / shell 目录切换管理程序命令的 PATH，不自动重启运行中的数据库。

## 状态、日志与连接

```bash
vsm db engines
vsm db list --json
vsm db logs api-pg
vsm db connection api-pg --json
vsm help db
vsm help db switch
```

状态包含请求版本、实际服务端版本、运行方式、端口、数据位置与保留的版本历史。状态输出不包含密码。`connection` 是显式查看连接凭据的入口，包含主机、端口、用户名、密码、数据库及 URI；GUI 支持复制连接地址。

凭据保存在实例的 `credentials.json`，Unix 新建目录为 `0700`、文件为 `0600`，Windows 使用当前用户目录的 ACL。已有数据的凭据丢失时会要求恢复该文件，不重新生成密码。实例操作使用跨进程锁协调 CLI 与 GUI；Docker 容器与卷的所有权标签不匹配时拒绝操作。
原生实例同时记录服务进程 ID 与创建信息；PID 已复用或进程记录不匹配时，不向该端口发送停库命令。
