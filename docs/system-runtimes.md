# 已有运行时、手动路径与 Conda Python

GUI 的「系统检测」扫描 PATH、语言环境变量，以及 Conda 的环境登记文件和常见安装目录。每个语言详情只显示该语言；纳管单个或全部版本后复用本次检测结果，不再次扫描。外部安装发生变化时，点击「重新检测」。

## 添加扫描不到的安装

打开「系统检测 → 手动添加路径」，或语言详情中的「添加路径」。选择工具，填写安装目录或主命令路径，点击「检测路径」，核对版本与主命令后确认纳管。可使用「选择目录」按钮，或者直接粘贴可执行文件路径。

CLI 也可使用：

```bash
vsm adopt maven --path /opt/apache-maven-3.9.6
vsm adopt python --path /opt/python3/bin/python3 --name office-python
vsm use -g python office-python
```

`--name` 是可选管理标识，允许字母、数字及 `._+-`。相同版本的不同安装可分别命名。检测会检查对应工具的主命令，不会将 Java 的版本输出当作 Python 版本。

纳管通过目录链接接入已有安装；VSM 卸载只删除链接，原安装和依赖仍保留。`local` 与 `docker` 隔离项目不链接宿主机工具；需要复用已有安装时使用 `versions` 模式。

## Conda 的 Python 环境

支持纳管已经存在的 Conda / Miniconda / Anaconda / Miniforge Python 环境。自动发现读取 `~/.conda/environments.txt`、`CONDA_PREFIX` / `CONDA_EXE` 和常见 `envs` 目录；自定义位置可手动添加。

每个环境获得独立的 `conda-<名称>-<路径摘要>` 管理标识。两个环境即使都是 Python 3.12.8，也会分别列出并保留各自的包。详情显示解释器版本及 Conda 环境名。也可自行命名：

```bash
vsm adopt python --path /opt/miniconda3/envs/science --name conda-science
vsm use python conda-science
vsm run -- python -c "import sys; print(sys.executable, sys.prefix)"
vsm run -- python -m pip list
```

激活或执行时使用原环境的解释器、标准 Conda 变量；Windows 同时加入 Scripts 和 Library DLL 目录。原环境的包不会被复制或重装。离开 VSM 选择后，shell 钩子恢复原环境变量。

此入口管理已有环境的选择。Conda 环境的创建、包求解及 `activate.d` 自定义脚本仍由 Conda 管理；依赖这些脚本时先执行 `conda activate <环境>`，或在项目中明确配置所需变量。标识绑定本机环境，换机器后需先准备并纳管对应环境；普通 `python 3.12` / `latest` 不会自动选中有独立依赖的 Conda 环境。

## Maven

按 `mvn` 主命令发现 Maven，并优先读取 `lib/maven-core-*.jar` 内的版本元数据，不依赖当前目录或 `JAVA_HOME`。没有元数据时，仅执行版本参数，失败会显示原因，不执行 Maven 构建阶段。

纳管以完整 Maven 安装目录为根，保留旧版曾链接到 `bin` 的记录。实际运行 Maven 仍需要有效 JDK。Windows 终端中可以直接运行 `mvn --version`；通过要求直接启动可执行文件的入口运行批处理时，可显式使用 `cmd.exe /c mvn --version`。
