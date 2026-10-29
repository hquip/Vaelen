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

## 安装位置与版本列表

左侧「安装目录」是所有工具的查找根目录。例如设为 `J:\vsm` 后，Go 1.27.3 默认安装在 `J:\vsm\go\1.27.3`。安装时的「选择安装父目录」用于单次指定其它位置，同样会自动创建 `工具名/版本`，界面会预览完整路径。

选择当前安装根目录或当前工具目录（例如 `J:\vsm` 或 `J:\vsm\go`）都会使用上述默认位置，不会再生成 `go\go\1.27.3`。其它嵌套在安装根目录内的父目录会被拒绝；需要单独存放时请选择外部父目录，例如 `K:\sdk`。

旧版产生的嵌套安装通过原有链接继续使用。版本列表会依据匹配的安装所有权记录排除存储文件夹，拒绝将它们作为版本整体卸载；正常卸载具体版本时，只清理对应安装和已经空出的存储父目录。不会自动搬迁或重装其它版本。

## Go 与 Lua 版本读取

Go 优先读取程序自身的构建元数据，兼容发行版版本后缀和附加构建信息；命令回退仍禁用项目工具链自动切换。无法识别时会显示实际的版本标识，方便诊断，不从报错中的数字猜测版本。Lua 与 LuaJIT 使用 `-v`，同时识别标准输出和错误输出中的语言版本横幅。

## fish 激活与子 shell

在 fish 中执行 `vsm activate fish | source`，或用 `vsm path-init fish` 将动态激活入口写入 `config.fish`。也可自行将 `vsm activate fish | source` 放到 fish 的 `conf.d` 脚本；选择一种加载位置即可。

设置了绝对路径 `XDG_CONFIG_HOME` 时，自动配置写入该目录下的 `fish/config.fish`；否则使用 `~/.config/fish/config.fish`。保留原配置内容。

`PATH` 和 `VSM_PATH_BASE` 均按 fish 的路径列表导出，嵌套启动 fish 后仍保留目录边界及目录中的空格。再次应用脚本会恢复基础 PATH，并保留系统命令。已有的冒号分隔基础 PATH 会重新标记为路径列表；旧版若已把目录以空格合并，不能可靠区分目录内的空格，请从健康的终端重新启动 fish，勿按空格手动拆分。

激活会将 shims 前置并去重；支持 `fish_add_path` 时使用 `--path --global --move`，只调整当前进程的 PATH，避免自动激活改写持久的 `fish_user_paths`。旧 fish 使用等价的列表处理。进入目录后立即刷新一次，紧随其后的提示符不再重复启动 VSM；其它提示符仍刷新，以便在同一目录感知 GUI / 其它终端修改的全局版本和项目配置。重复加载不会累积事件处理函数。

## 临时版本、环境恢复与更新锁

`vsm context --check` 识别当前实例生成的完整 shim 脚本，核对工具、数据目录和 CLI，不会仅因 shim 与真实程序路径不同就误报未激活。旧 CLI、其它实例或被修改的 shim 仍需修复；旧提示符导出的版本标签不会覆盖已确认的命令路径。

Unix 安装的主命令必须是有执行权限的非空文件或有效链接。损坏安装不会再被 `install` 当成可用而跳过，也不会自动修改外部纳管目录的权限。

删除或编辑外部插件 JSON 后，在 GUI 点击「刷新」即可重新加载。完整新集合验证成功后才替换旧集合；无效 JSON、重复定义或读取失败会显示错误并保留原集合。删除覆盖内置工具的外部定义后恢复内置实现。

`vsm shell java 21` 等临时会话会在子进程中记录明确的版本选择，启动钩子和后续提示符继续使用它；退出后父终端和全局、项目配置保持原值。缺少主命令的安装不能启动临时会话，隔离项目使用 `vsm run`。

项目配置损坏或初始化失败时，`vsm env` 仍输出完整的基础环境恢复脚本，并返回非零退出码、将原因写入 stderr。显式 `vsm mcp config --project <目录>` 按指定项目生成配置，不受调用位置的无关项目配置影响。

本地 Python venv 的完整性检查由 shell 激活和执行入口共用。已有环境缺少版本标记、配置或 Python 入口时停止切换，保留依赖；hook 不会自动创建或重装虚拟环境。

更新与配套 CLI 准备共用系统文件锁，进程异常退出后由系统释放。程序目录保留 `.vsm-update.lock` 文件属于正常现象，文件存在不代表任务仍在运行；不要在更新期间手动删除它。旧版残留的空锁文件可由新版复用。

环境规划与 GUI 状态刷新会在单次操作内共享配置及安装索引，下一次提示符或刷新重新读取，以减少重复查找并及时反映新版本选择。

## Windows 全局切换与 cmd

打开 Windows GUI 即会自动注册全局 `vsm` 命令：校验或下载配套 CLI，创建 `shims/vsm.cmd`，写入用户 PATH 并配置 cmd 自动加载。下载文件可保持 `vsm-windows-amd64.exe` 等官方原名；无需先安装任何语言。重复启动会去重 PATH，保留其它程序的 PATH 和已有 cmd 启动命令。即使 GUI 当前打开隔离项目或项目配置损坏，注册的仍是全局 VSM 入口。

自动注册与语言版本激活分别处理；自动注册不重建语言入口、不切换语言版本，也不修改 PowerShell 配置。Windows 安装脚本调用 `path-init --command-only`，安装旧版时兼容使用 `path-init cmd`；成功后在当前 PowerShell 前置安装目录。自动注册失败会显示具体原因，安装脚本保留已校验的程序并给出重试命令；GUI 可点击注册结果区「重新验证」重试。

点击 GUI「激活 PATH」会配置用户 PATH、cmd AutoRun 和 PowerShell 启动钩子。新开的 cmd 自动把 VSM shims 放到系统 Java 等工具之前，不修改系统 PATH 或原安装。

Windows 的 `PATHEXT` 决定无扩展名命令会搜索哪些文件。若缺少 `.CMD`，cmd 会跳过 VSM 的 `.cmd` 入口；初始化脚本现在会自动补齐，保留原有扩展名，重复加载不累积。此修复在加载脚本的 cmd 中生效。

若实际执行验证未通过，页面会保留原因和处理方法，可展开或复制诊断信息，查看实际命中的程序、所选安装及命令扩展名。当前目录的同名程序、VSM 入口目录里的高优先级程序和入口缺失会分别提示；VSM 不会自动删除这些文件。验证使用独立的受控 cmd，并不读取或修改其它已运行终端的环境。

GUI 会识别同目录的标准 CLI 文件名和 GitHub 原始下载文件名，并校验与 GUI 的版本一致；PATH 中的旧版本会被跳过。缺少匹配组件时，从对应的官方 Release 下载并校验 CLI，以官方文件名保存在 GUI 旁边，不覆盖其它位置或同目录标准名的旧 CLI。程序目录需要可写。激活后 `vsm` 和语言命令使用同一份 CLI。

全局切换也可能触发上述准备。下载期间底部会持续显示「正在准备 VSM 运行组件」、字节数、速度与重试信息，完成后自动继续原操作。全局切换和 PATH 激活按钮在操作结束前暂时禁用；其它写操作若被拦截，会说明正在准备配套 CLI。失败后可重试，查看日志中的 `cli.prepare` 与原操作记录定位原因。

激活结果保留在页面中。验证会实际执行命令、对比所选程序与 shim 的版本输出；结果、CLI 来源和失败原因保存在日志中，路径详情可在语言页展开。仅存在 shim 文件不会被视为验证通过。受控 cmd 验证通过表示启动配置可用，不代表其它已打开 cmd 的 PATH 已改变。

已经通过 VSM shim 的 cmd，GUI 切换全局默认后，下次 `java -version` 即使用所选版本，无需重新激活或重开。`where java` 的第一项应位于 VSM 数据目录的 `shims` 下；后面的系统 Java 可以保留。项目版本和 `VSM_JAVA_VERSION` 环境覆盖仍优先于全局默认。

生成的语言入口与 `vsm` 入口绑定到其所属的数据目录。旧入口只转发到 CLI，却继续使用调用者继承的 `VSM_HOME`；当 GUI 和已有终端的该变量不同时，即使执行了 GUI 生成的入口，也可能读取另一目录的默认版本。修复后的入口只在子进程中设置所属目录，保留原终端变量以及其它目录的配置；直接调用 CLI 可执行文件仍按其环境解析 `VSM_HOME`。更新后重新点击「激活 PATH」或执行 `vsm reshim` 重建入口。

执行验证会额外传入一个独立空目录作为继承环境，要求入口仍能使用所属配置执行所选程序；不再用强制匹配的 `VSM_HOME` 掩盖旧入口的问题。日志中的 `owner_home_verified` 表示这项检查通过，仍不表示其它终端的 PATH 已被修改。

若原有 cmd 的 PATH 从未包含 VSM，GUI 无法直接改写那个进程的环境。可在原 cmd 执行 `call "%VSM_HOME%\vsm-cmd-init.cmd"` 加载一次；未自定义 VSM_HOME 时使用 `call "%USERPROFILE%\.vsm\vsm-cmd-init.cmd"`。GUI 显示的是它自身启动时的 PATH，不能据此断言其它终端仍在使用旧版本。

已有 cmd 也可能包含 VSM 目录，但顺序仍是系统 Java、Oracle `javapath`、VSM。此时 `vsm which java` 会正确显示所选 Java 17，而 `java -version` 仍运行排在前面的 Java 21。点击「激活 PATH」保存启动配置后，在这个原 cmd 中加载一次初始化命令，即可前置 VSM 并继续在同一终端切换，不需要重装 JDK 或删除 Oracle 的文件。GUI 的结果区提供按当前数据目录生成的可复制命令；这条命令不会自行在其它终端执行。

全局设置与当前有效版本分别显示。若项目版本文件或 `VSM_JAVA_VERSION` 等环境变量指定其它版本，会继续优先采用它们；全局切换不会自动改写项目要求或其它终端的变量。诊断时可在原 cmd 执行 `vsm version`、`vsm current`、`vsm which java` 和 `where java`，对照配置来源与实际命令顺序。`vsm current` 当前不接受单个工具参数。

`vsm path-init cmd` 只配置 cmd 与用户 PATH；不带 shell 时同时配置 PowerShell。cmd `/D` 会跳过 AutoRun。初始化脚本支持 `/V:ON` 和 `/V:OFF`，保留原来的延迟展开模式及 PATH 中的字面 `!`、引号和 `&`，执行验证覆盖两种模式。旧版初始化脚本在 `/V:ON` 下会直接退出，旧验证又固定使用 `/V:OFF`，因此会漏报这条激活失败路径；旧 `/V:OFF` 分支的百分号拼接也会误处理带引号和 `&` 的 PATH 项。修复后需重新点击「激活 PATH」生成新版脚本。

cmd 数据目录不能包含 `%` 或 `!`。启动配置位于 `HKCU\Software\Microsoft\Command Processor`，原来的 AutoRun 字符串与类型保存在 `VSM.AutoRun.Previous` / `VSM.AutoRun.Kind`；若已被外部修改，VSM 会报错并保留现状。撤销时恢复原 AutoRun，再移除 `VSM.AutoRun.*` 值；不要删除其它程序的启动命令。

## 用详细日志定位切换失败

修改「安装目录」会改变版本的查找根目录，程序不会自动移动原安装。例如从默认 `~/.vsm/installs` 改到空目录后，旧全局版本配置仍存在，但新目录中没有对应安装，便会报告版本不可用。修复版会拒绝让当前可用全局版本失效的目录更改，并显示原目录和目标目录；目标目录已经准备好匹配版本时仍可切换。未选择为全局版本的其它安装也不会自动迁移。

已经遇到此问题时，将安装目录恢复到实际包含原版本的根目录，或在新的根目录重新安装/纳管所需版本。默认布局为 `<安装目录>/<工具>/<版本>`。错误详情显示版本要求、配置来源、查找根目录和可见版本；入口激活无法补齐缺失的安装。CLI 的 `vsm install-root default` 可恢复默认查找根目录，操作保留旧目录内容。

更新配套 CLI / GUI 后，先点击「激活 PATH」生成新版初始化脚本。默认 `info` 级别即可记录详细诊断；`VSM_LOG_LEVEL=off` 会关闭新记录。GUI 导出时还会补充一份明确标记为 `gui-process` 的快照。

| 日志事件 | 可以判断的内容 |
| --- | --- |
| `activation · global.config.write.*` | 全局版本文件路径、请求版本、写入是否成功 |
| `activation · shims.generate.*` | 实际 CLI、入口目录及生成失败原因 |
| `activation · user-path.*` | 用户 PATH 是否写入；明确标记没有改写其它进程 |
| `activation · cmd.hook.readback` | AutoRun 写入后的匹配结果、原设置保留状态、注册表类型及指纹、用户默认延迟展开配置 |
| `activation · environment.snapshot` | PATH 前 12 项、VSM 入口位置（索引从 0 开始，-1 表示未找到）、PATHEXT、当前目录和数据目录 |
| `activation · command.route` | 全局默认与当前有效版本、覆盖来源、首个命中的命令、所选程序、Java/Go HOME；手动诊断和 GUI 导出含最多 16 个候选程序 |
| `activation · cmd.verify.*` | 受控 cmd 的 ON/OFF 验证、实际版本与预期版本、失败分类 |
| `activation · runtime.dispatch.*` | 已进入 VSM 执行链后最终启动的程序及成功/失败结果 |

`scope=controlled-cmd` 只表示受控测试；`cmd-session` 是初始化脚本实际运行时记录的 PATH 调整前后，按 `helper_session` 关联；GUI 和核心步骤按 `operation_id` 关联。AutoRun 原始命令不写入日志，只保留指纹和匹配结果。不会记录完整环境、任意命令参数或子进程输出。

没有用户会话记录只能说明日志未观察到脚本执行，也可能是脚本未加载、配套 CLI 不可用、日志被关闭或写入失败，不能仅凭缺少记录断言根因。此时在原终端执行：

```cmd
vsm logs diagnose java
vsm logs export vsm-diagnostics.jsonl
```

`diagnose` 只读取当前进程继承的配置与命令顺序，不运行语言程序、不修改 PATH，并确认诊断已实际写入。若配置读取失败，会保留 `capture.failed`；自动初始化中的日志失败保持静默，不阻断原有命令。日志沿用本地时间显示、敏感内容脱敏、2 MiB × 4 文件轮换及新文件导出机制。

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
