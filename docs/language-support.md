# 语言与工具链覆盖

VSM 按编译器、解释器和 SDK 管理版本。C/C++ 对应 GCC、Clang、MSVC 或 Zig；C#/F#/VB.NET 共用 .NET SDK。语言标准、编译器、虚拟机和项目镜像是不同的版本维度。

当前内置 **151 项工具链 / 工具**，其中 **30 项配置了自动安装来源**；另有 **17 项宿主接入说明**。数量包含工具和不同实现，不将同义词、标准方言或宿主说明计作独立语言。GUI 和 CLI 按实际平台说明能力。

## 使用方式

```bash
vsm tools search "C#"
vsm tools search "Move" --json
vsm tools info vba
vsm install dotnet 8.0.408 --require-checksum
vsm use dotnet 8.0.408
vsm run -- dotnet build
vsm install scarb 2.21.0 --require-checksum
vsm use scarb 2.21.0
vsm run -- scarb --offline build
```

版本号为格式示例，实际安装以发行方目标包和校验元数据为准。工具 ID 可能与命令不同：`j` 使用 `jconsole`，`gnu-apl` 使用 `apl`，`matlab` 使用 `matlab`。

```bash
vsm adopt gnu-apl --path /opt/gnu-apl
vsm use gnu-apl 2.0
vsm run -- apl --script -f hello.apl
vsm adopt red --path /opt/red --name 0.6.6
vsm use red 0.6.6
vsm run -- red hello.red
```

## 能力和版本标识

- **自动安装**：当前平台有发行包；版本和架构必须匹配。新增 .NET、Dart、Julia、Flutter、PowerShell、Godot/GDScript、Vyper、Arturo、Factor、Nim、Haxe、Scarb/Cairo、Aptos/Move 安装来源。Cairo 0、Aptos Move 与 Sui Move 需分别管理。
- **纳管已有安装**：先准备完整 SDK/运行时及系统依赖，再通过路径纳管；卸载只移除 VSM 的链接。EiffelStudio 保留平台目录并设置 ISE_EIFFEL/ISE_PLATFORM。Pharo/Squeak 管理 VM，用户仍需提供匹配 image 和插件。
- **只读元数据**：MATLAB 读取 VersionInfo.xml，NetLogo 读取应用配置或官方 JAR 清单。检查不启动软件、Java 或授权会话；Windows 终端报告明确标注仅验证入口与元数据，运行及授权需在实际使用时确认。
- 元数据和用户标注版本用于本机 `versions` 模式；Docker 自动版本检查会明确拒绝这两类要求，不会为了猜测版本而启动宿主软件。
- **用户标注版本**：Red、Vale、Kona/K、UCBLogo 需要显式填写版本标识，GUI 中必填，CLI 使用 `--name`。VSM 验证命令文件及所选路径，不自动启动这些程序探测，不将标识宣称为已验证的软件版本。`context --json` 的 `version_mode` 为 `label`，终端诊断只报告入口验证。外部 manifest 可使用 `source.type=local` 和 `version_manual=true` 接入同类工具。
- **宿主接入**：VBA、ABAP、Verse、PLC 等在宿主中运行。入口显示所需软件、限制和接入步骤；不会建立虚假独立运行时、shim 或全局版本。CLI 用 `vsm tools info <ID> [--json]` 查看。SQL/PLSQL/TSQL 的服务端版本也不由客户端切换。

## 安装与校验边界

- .NET 使用微软官方 SHA512，其它可用来源使用发行方 SHA256；严格校验、下载、锁文件及来源记录兼容两种摘要。Haxe 部分历史包没有摘要，`--require-checksum` 会拒绝。
- .NET 列出受支持稳定通道，可输入精确历史 SDK；读取 global.json 的 sdk.version，不改写 rollForward/工作负载。探测使用 --list-sdks，隔离项目工具链选择。
- Dart 列表显示当前稳定版，历史版本可完整输入。Julia/Flutter 根据平台元数据选包。Flutter 扫描仅读已有缓存，首次初始化由用户执行。PowerShell 不替换 Windows 内置 powershell.exe。
- Godot 默认标准版，不含 .NET 版或导出模板；固定 Windows/Linux 命令名，保留 macOS 原签名应用包。Vyper 当前自动安装适配 amd64，并检查二进制实际格式。Factor 的 macOS DMG 需手动安装后纳管。
- Nim 从官网稳定下载表选包，缺少校验附件时读取同一官方资产的 SHA256，不误装 nightly；生成机器码需 C/C++ 后端。Arturo 默认官方 mini 命令行包，避免完整图形包的桌面 WebKit 依赖。
- Aptos 使用专用 CLI 标签与官方包，排除节点版本；安装和探测不执行链上操作。Scarb 使用对应平台包，本地项目拥有独立 SCARB_CACHE；合约框架、网络和部署由项目控制。
- J 探测先检查相邻引擎库，自动扫描仅查 ijconsole/jamalgam，避免启动 JDK 同名 jconsole；原 jconsole 可显式添加路径。CBQN 仅纳管带可识别版本/提交信息的构建；Hylo 的 unknown 构建不能自动识别。
- MSVC 需 Build Tools/开发者环境；CUDA 需 SDK/驱动。VSM 不安装许可证、驱动、系统 COM 组件或固件。跨平台 UI 不代表每个 SDK 在每种系统都有发行包。

## 验证范围

单元测试覆盖定义、命令映射、版本参数、摘要、错误架构、SDK 元数据、手动标识与宿主操作边界。集成验证包括 C/C++、C#/F#/VB.NET、Godot/GDScript、Vyper、Arturo、Factor、Haxe、Nim，以及新增 Scarb/Cairo、Aptos/Move、J、Rebol 的运行或编译；Linux 测试 Forth、REXX、GNU APL。按实际存在的目标包选择组合。

MATLAB/Delphi 等专有 SDK 未包含在公开 CI 中，不宣称已取得授权或验证其全部运行/编译功能。Pharo/Squeak 的具体 image、各类未标记开发构建和不同厂商 SDK 仍需项目自行验收。“所有编程语言”没有固定全集；本目录说明当前实现和接入方式。

## 工具链目录

下表安装能力以 Windows/amd64 为参考，GUI 随实际平台调整。标注版本与元数据模式单独列出。

| 工具 ID | 名称 / 语言检索词 | 能力 |
| --- | --- | --- |
| `agda` | Agda · Agda | 纳管已有安装 |
| `aptos` | Aptos CLI · Move · Move / Aptos Move | 自动安装 / 纳管 |
| `arturo` | Arturo · Arturo | 自动安装 / 纳管 |
| `ballerina` | Ballerina · Ballerina | 纳管已有安装 |
| `bash` | Bash · Bash / POSIX Shell / Shell Script | 纳管已有安装 |
| `bazel` | bazel | 纳管已有安装 |
| `bqn` | BQN · CBQN · BQN / CBQN | 纳管已有安装 |
| `bun` | bun · JavaScript / TypeScript / JSX / TSX | 自动安装 / 纳管 |
| `c3` | C3 · C3 | 纳管已有安装 |
| `carbon` | Carbon · Carbon | 纳管已有安装 |
| `chapel` | Chapel · Chapel | 纳管已有安装 |
| `chez` | Chez Scheme · Scheme / R6RS Scheme | 纳管已有安装 |
| `chicken` | CHICKEN Scheme · Scheme / CHICKEN | 纳管已有安装 |
| `clang` | LLVM / Clang · C / C++ · C / C++ / Objective-C / Objective-C++ / Clang / LLVM | 纳管已有安装 |
| `clisp` | CLISP · Common Lisp · Common Lisp / Lisp | 纳管已有安装 |
| `clojure` | Clojure · Clojure / ClojureScript | 纳管已有安装 |
| `cmake` | cmake | 自动安装 / 纳管 |
| `cobol` | GnuCOBOL · COBOL / GnuCOBOL | 纳管已有安装 |
| `coffeescript` | CoffeeScript · CoffeeScript | 纳管已有安装 |
| `coq` | Coq · Coq / Gallina | 纳管已有安装 |
| `crystal` | Crystal · Crystal | 纳管已有安装 |
| `cuda` | CUDA · NVCC · CUDA / CUDA C / CUDA C++ | 纳管已有安装 |
| `dart` | dart · Dart | 自动安装 / 纳管 |
| `delphi` | Delphi 命令行编译器 · Delphi / Object Pascal | 纳管已有安装 |
| `deno` | deno · JavaScript / TypeScript / JSX / TSX | 自动安装 / 纳管 |
| `dmd` | D · DMD · D | 纳管已有安装 |
| `dotnet` | .NET SDK · C# / F# / VB.NET · C# / CSharp / C Sharp / F# / FSharp / Visual Basic .NET / VB.NET / VBNet | 自动安装 / 纳管 |
| `eiffel` | EiffelStudio · Eiffel / EiffelStudio | 纳管已有安装 |
| `elixir` | Elixir · Elixir | 纳管已有安装 |
| `elm` | Elm · Elm | 纳管已有安装 |
| `erlang` | Erlang/OTP · Erlang | 纳管已有安装 |
| `factor` | Factor · Factor | 自动安装 / 纳管 |
| `fennel` | Fennel · Fennel | 纳管已有安装 |
| `fish` | fish · fish / Shell Script | 纳管已有安装 |
| `flang` | LLVM Flang · Fortran | 纳管已有安装 |
| `flutter` | flutter · Dart / Flutter | 自动安装 / 纳管 |
| `fpc` | Free Pascal · Pascal / Object Pascal / Delphi-compatible syntax | 纳管已有安装 |
| `freebasic` | FreeBASIC · BASIC / FreeBASIC / QuickBASIC-compatible syntax | 纳管已有安装 |
| `fsharp` | F# · Mono tools · F# / FSharp | 纳管已有安装 |
| `gambit` | Gambit Scheme · Scheme / Gambit | 纳管已有安装 |
| `gawk` | GNU AWK · AWK / gawk | 纳管已有安装 |
| `gcc` | GCC · C / C++ · C / C++ / C89 / C99 / C11 / C17 / C23 / C++11 / C++14 / C++17 / C++20 / C++23 | 纳管已有安装 |
| `gforth` | Gforth · Forth / Gforth | 纳管已有安装 |
| `gfortran` | GNU Fortran · Fortran / FORTRAN 77 / Fortran 90 / Fortran 2008 | 纳管已有安装 |
| `ghdl` | GHDL · VHDL | 纳管已有安装 |
| `gleam` | Gleam · Gleam | 自动安装 / 纳管 |
| `glslang` | glslang · GLSL / HLSL / SPIR-V | 纳管已有安装 |
| `glslc` | Shaderc · GLSL / HLSL / SPIR-V | 纳管已有安装 |
| `gnat` | GNAT · Ada · Ada / SPARK | 纳管已有安装 |
| `gnu-apl` | GNU APL · APL / GNU APL | 当前平台不支持 |
| `gnu-prolog` | GNU Prolog · Prolog / GNU Prolog | 纳管已有安装 |
| `go` | go · Go / Golang | 自动安装 / 纳管 |
| `godot` | Godot · GDScript · GDScript / Godot | 自动安装 / 纳管 |
| `gradle` | gradle | 纳管已有安装 |
| `grain` | Grain · Grain | 纳管已有安装 |
| `groff` | GNU troff · troff / roff | 纳管已有安装 |
| `groovy` | Groovy · Groovy | 纳管已有安装 |
| `guile` | GNU Guile · Scheme / Guile | 纳管已有安装 |
| `hare` | Hare · Hare | 当前平台不支持 |
| `haskell` | Haskell · GHC · Haskell | 纳管已有安装 |
| `haxe` | Haxe · Haxe | 自动安装 / 纳管 |
| `hylo` | Hylo · Hylo | 纳管已有安装 |
| `idris2` | Idris 2 · Idris | 纳管已有安装 |
| `iverilog` | Icarus Verilog · Verilog / SystemVerilog | 纳管已有安装 |
| `j` | J · J / J Programming Language | 纳管已有安装 |
| `janet` | Janet · Janet | 纳管已有安装 |
| `java` | java · Java / JVM | 自动安装 / 纳管 |
| `julia` | julia · Julia | 自动安装 / 纳管 |
| `just` | just | 自动安装 / 纳管 |
| `jython` | Jython · Python 2 / Jython | 纳管已有安装 |
| `koka` | Koka · Koka | 纳管已有安装 |
| `kona` | Kona · K · K / Kona / K3 | 纳管已有安装（用户标注版本） |
| `kotlin` | Kotlin · Kotlin / Kotlin Script | 自动安装 / 纳管 |
| `ldc` | D · LDC · D | 纳管已有安装 |
| `lean` | Lean · Lean | 纳管已有安装 |
| `lua` | Lua · Lua | 纳管已有安装 |
| `luau` | Luau · Luau | 纳管已有安装 |
| `mariadb` | mariadb · SQL | 数据库实例 |
| `matlab` | MATLAB · MATLAB / Simulink | 纳管已有安装（只读元数据） |
| `maven` | maven | 纳管已有安装 |
| `micropython` | MicroPython · MicroPython / Python | 纳管已有安装 |
| `mlton` | MLton · Standard ML / SML | 纳管已有安装 |
| `mojo` | Mojo · Mojo | 当前平台不支持 |
| `mono` | Mono · C# / CSharp / F# / VB.NET / Mono | 纳管已有安装 |
| `msvc` | MSVC · C / C++ · C / C++ / MSVC / Visual C++ | 纳管已有安装 |
| `mysql` | mysql · SQL | 数据库实例 |
| `nasm` | NASM · Assembly / x86 Assembly / 汇编 | 纳管已有安装 |
| `netlogo` | NetLogo · NetLogo | 纳管已有安装（只读元数据） |
| `nim` | Nim · Nim | 自动安装 / 纳管 |
| `ninja` | ninja | 自动安装 / 纳管 |
| `node` | node · JavaScript / TypeScript / Node.js | 自动安装 / 纳管 |
| `nushell` | Nushell · Nushell | 自动安装 / 纳管 |
| `ocaml` | OCaml · OCaml | 纳管已有安装 |
| `octave` | GNU Octave · Octave / MATLAB-compatible syntax | 纳管已有安装 |
| `odin` | Odin · Odin | 纳管已有安装 |
| `perl` | Perl · Perl | 纳管已有安装 |
| `pharo` | Pharo VM · Smalltalk / Pharo | 纳管已有安装 |
| `php` | PHP · PHP | 纳管已有安装 |
| `pony` | Pony · Pony | 纳管已有安装 |
| `postgres` | postgres · SQL | 数据库实例 |
| `powershell` | PowerShell · PowerShell / pwsh | 自动安装 / 纳管 |
| `purescript` | PureScript · PureScript | 纳管已有安装 |
| `pypy` | PyPy · Python / PyPy | 纳管已有安装 |
| `python` | python · Python | 自动安装 / 纳管 |
| `r` | R · R | 纳管已有安装 |
| `racket` | Racket · Racket / Scheme | 纳管已有安装 |
| `raku` | Raku · Raku / Perl 6 | 纳管已有安装 |
| `reason` | Reason · Reason / ReasonML | 纳管已有安装 |
| `rebol` | Rebol 3 · Rebol / Rebol 3 | 纳管已有安装 |
| `red` | Red / Red-System · Red / Red/System | 纳管已有安装（用户标注版本） |
| `redis` | redis · Redis protocol | 数据库实例 |
| `regina` | Regina REXX · REXX / Regina REXX | 纳管已有安装 |
| `rescript` | ReScript · ReScript | 纳管已有安装 |
| `roc` | Roc · Roc | 纳管已有安装 |
| `ruby` | Ruby · Ruby | 纳管已有安装 |
| `rust` | Rust · Rust | 纳管已有安装 |
| `sbcl` | SBCL · Common Lisp · Common Lisp / Lisp | 纳管已有安装 |
| `sbt` | sbt | 纳管已有安装 |
| `scala` | Scala · Scala | 纳管已有安装 |
| `scarb` | Scarb · Cairo · Cairo / Scarb | 自动安装 / 纳管 |
| `sdcc` | SDCC · C / Embedded C / 嵌入式C | 纳管已有安装 |
| `smalltalk` | GNU Smalltalk · Smalltalk / GNU Smalltalk | 纳管已有安装 |
| `sml` | Standard ML · SML/NJ · Standard ML / SML | 纳管已有安装 |
| `solidity` | Solidity · Solidity / Yul | 纳管已有安装 |
| `sqlite` | SQLite · SQL / SQLite SQL | 纳管已有安装 |
| `squeak` | Squeak / OpenSmalltalk VM · Smalltalk / Squeak / OpenSmalltalk | 纳管已有安装 |
| `sui` | Sui CLI · Move · Move / Sui Move | 纳管已有安装 |
| `swi-prolog` | SWI-Prolog · Prolog / SWI-Prolog | 纳管已有安装 |
| `swift` | Swift · Swift | 纳管已有安装 |
| `tcc` | Tiny C Compiler · C / TinyCC | 纳管已有安装 |
| `tcl` | Tcl / Tk · Tcl / Tk | 纳管已有安装 |
| `tinygo` | TinyGo · Go / TinyGo | 自动安装 / 纳管 |
| `typescript` | TypeScript Compiler · TypeScript / TSX | 纳管已有安装 |
| `typst` | Typst · Typst | 自动安装 / 纳管 |
| `ucblogo` | UCBLogo · Logo / UCBLogo | 纳管已有安装（用户标注版本） |
| `unison` | Unison · Unison | 纳管已有安装 |
| `uv` | uv | 自动安装 / 纳管 |
| `vala` | Vala · Vala / Genie | 纳管已有安装 |
| `vale` | Vale · Vale | 纳管已有安装（用户标注版本） |
| `valkey` | valkey · Redis protocol | 数据库实例 |
| `verilator` | Verilator · Verilog / SystemVerilog | 纳管已有安装 |
| `vlang` | V · V / Vlang | 自动安装 / 纳管 |
| `vulkan-glsl` | Vulkan Shader Tools · GLSL / HLSL | 纳管已有安装 |
| `vyper` | Vyper · Vyper | 自动安装 / 纳管 |
| `wabt` | WABT · WebAssembly / WAT | 纳管已有安装 |
| `wasmer` | Wasmer · WebAssembly / WAT | 纳管已有安装 |
| `wasmtime` | Wasmtime · WebAssembly / WAT | 纳管已有安装 |
| `wren` | Wren · Wren | 纳管已有安装 |
| `yasm` | Yasm · Assembly / x86 Assembly / 汇编 | 纳管已有安装 |
| `zig` | zig · Zig / C / C++ | 自动安装 / 纳管 |
| `zsh` | Zsh · Zsh / Shell Script | 纳管已有安装 |

## 宿主接入目录

这些说明不计入可管理工具链数量；查看完整步骤可用 `vsm tools info <ID>`。

| ID | 语言 / 宿主 | 切换限制 |
| --- | --- | --- |
| `abap` | ABAP · SAP · SAP ABAP Platform / SAP BTP ABAP Environment | ABAP 编译与运行由 SAP 后端提供；本机 PATH、SAP GUI 或 ADT 版本不会切换服务器上的 ABAP 语言版本。 |
| `angelscript` | AngelScript · 嵌入式脚本 · 集成 AngelScript SDK 的应用或游戏引擎 | 语言运行时嵌入宿主；不同应用注册的 API、编译选项和 SDK 版本可能不同，没有通用的独立运行时切换。 |
| `dyalog` | Dyalog APL · Dyalog APL | Dyalog 的工作区、运行时和授权需配套管理；GNU APL 的兼容入口不代表 Dyalog 已被独立适配。 |
| `e-language` | 易语言 · 易语言 IDE 与支持库 | 工程版本、IDE、支持库及授权共同决定兼容性；VSM 不独立安装或切换其系统组件。 |
| `kdb` | kdb+ / Q · KX kdb+ | Q 与数据库引擎、进程和授权配套；切换客户端不会切换已有服务进程或数据格式。Kona 由独立工具入口管理，不与 kdb+ 或 ngn/k 互相替代。 |
| `labview` | LabVIEW · NI LabVIEW / LabVIEW Runtime / 驱动及工具包 | VI 的编辑与运行受 LabVIEW、运行时、驱动、工具包和许可证共同约束；选择运行时不会自动转换工程或部署硬件。 |
| `oracle-sql` | Oracle · PL/SQL · Oracle Database / SQLcl | PL/SQL 的实际编译与运行版本由 Oracle 服务端及兼容性设置决定；切换 SQL 客户端不会切换数据库引擎。 |
| `plc` | PLC · ST / 梯形图 · CODESYS / TIA Portal / TwinCAT / OpenPLC 等 | PLC 语言取决于厂商 IDE、目标运行时、设备描述及固件；更改 PATH 不会切换控制器上的运行时，也不保证各厂商方言兼容。 |
| `sas` | SAS · SAS Foundation / SAS Viya | SAS 版本与部署、产品组件及授权绑定；Viya 的服务端运行环境不能通过本机 PATH 切换。 |
| `scratch` | Scratch · Scratch 网页编辑器 / Scratch Desktop | Scratch 由编辑器及其 VM 运行；VSM 不把编辑器版本伪装成可通过 PATH 切换的独立语言版本。 |
| `snap-language` | Snap! · Snap! 网页编辑器 | 此入口指编程语言 Snap!，不是 Linux Snap 包管理器；版本由网页或本地部署的编辑器决定。 |
| `stata` | Stata · Stata / Stata SE / Stata MP | 版本、产品版本和许可共同决定可用功能；VSM 不替换 Stata 授权文件或宣称已验证所有宿主版本。 |
| `tsql` | SQL Server · T-SQL · SQL Server / Azure SQL | T-SQL 能力取决于服务端版本、产品版本和数据库兼容级别；本机客户端版本不能替代服务端切换。 |
| `vba` | VBA · Office · Microsoft Office / Microsoft 365 | VBA 随 Office 应用提供，不能独立安装或通过 PATH 切换；宏兼容性还取决于宿主版本、位数、操作系统和引用库。 |
| `verse` | Verse · UEFN · Unreal Editor for Fortnite (UEFN) | Verse 工具链随 UEFN/Fortnite 提供，不能独立用 VSM 切换；工程还受平台发布版本和资源兼容性约束。 |
| `visual-basic6` | Visual Basic 6 · Visual Basic 6 IDE / VB6 Runtime | VB6 与现代 VB.NET 是不同工具链；IDE、COM/ActiveX 组件及位数要求由宿主安装负责，VSM 不切换系统 COM 注册。 |
| `wolfram` | Wolfram Language · Mathematica / Wolfram Engine / WolframScript | 实际计算版本由所选 Wolfram Kernel 及授权决定；仅替换 wolframscript 包装器不能证明 Kernel 已切换。 |
