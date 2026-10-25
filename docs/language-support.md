# 语言与工具链覆盖

VSM 按编译器、解释器和 SDK 管理版本。一门语言可以对应多个工具链，例如 C/C++ 可用 GCC、Clang 或 MSVC；C#、F#、VB.NET 共用 .NET SDK，语言标准版本不等于编译器版本。

本目录包含 **133 项工具链 / 工具**，其中 **28 项配置了自动安装来源**（具体系统和架构以程序中的能力提示为准）。语言名称、缩写、语法方言和框架可用于搜索，但不将它们分别宣称为独立语言数量。GUI 支持搜索 `C`、`C++`、`C#`、`Objective-C`、`Fortran`、`COBOL` 等名称，并显示真实能力。

## 使用方式

```bash
vsm tools search "C++"
vsm tools search "C#" --json
vsm install dotnet 8.0.408 --require-checksum
vsm use dotnet 8.0.408
vsm run -- dotnet build
vsm adopt gcc --path /opt/gcc-14
vsm use gcc 14
vsm run -- gcc hello.c -o hello
vsm run -- g++ hello.cpp -o hello
```

版本号为格式示例，可使用远端列表选择可用版本。已纳管安装的命令缺失、执行权限损坏或依赖不完整时仍会报错，不自动修改系统编译器、驱动或许可证。

## 自动安装与校验

- 原有 Go、Node、Java、Python、Zig、Deno、Bun 等来源保留。
- 新增 Godot（GDScript）、Vyper、Arturo、Factor、GNU Smalltalk、Gforth、Regina REXX、GNU APL、Ballerina 的内置入口。GNU Smalltalk / Gforth / Regina / GNU APL / Ballerina 当前纳管已有完整安装；不将同族其它实现或方言视为已支持。
- 新增 Godot、Vyper、Arturo、Factor、Nim、Haxe 自动安装来源。Godot 使用官方标准版，不包括 .NET 版及导出模板；Windows 固定命令名并保留配套控制台启动器，macOS 保持原签名应用包及外部命令链接。Vyper 当前自动安装适配 amd64，并在安装前验证实际二进制架构；Factor macOS 的 DMG 需手动安装后纳管。平台没有对应归档时回退为纳管能力。
- Nim 从官网稳定版下载表选包，校验附件缺失时读取同一官方发行资产的 SHA256，不将 nightlies 仓库中的开发快照当作正式版；编译仍需 C/C++ 后端。Haxe 某些历史归档缺少发布方摘要，`--require-checksum` 会拒绝；haxelib 及不同目标后端可能需要额外依赖。
- Arturo 默认安装官方 mini 命令行包，避免完整版在无桌面系统中缺少 WebKit 动态库；图形界面等可选组件需自行安装完整版后纳管。
- 新增 .NET SDK、Dart、Julia、Flutter 和 PowerShell。`.NET` 从微软官方元数据读取 SHA512；其它新来源使用发布方 SHA256。`--require-checksum`、锁文件和来源记录支持两种摘要，均不执行下载的第三方安装脚本。
- `.NET` 的远端列表包含受支持通道的稳定 SDK；精确的历史 SDK 版本可直接输入。项目读取 `global.json` 的 `sdk.version`，不自动改写其 rollForward、工作负载或 NuGet 设置。
- Dart 列表显示当前稳定版，历史版本可直接输入完整版本号。Julia / Flutter 从官方版本索引按平台选择归档；没有目标包时明确报错。PowerShell 使用跨平台 pwsh 发行包，不替换 Windows 自带 powershell.exe。
- C/C++ 也可使用已支持的 Zig（`zig cc` / `zig c++`）；Windows MSVC 需先安装 Build Tools，并在加载 vcvars 的开发者环境中使用。VSM 不创建 INCLUDE/LIB 或替代 Windows SDK。
- 本地隔离项目同时隔离 .NET CLI / NuGet、Dart Pub 和 Julia depot 的缓存。Flutter 新解压安装需先由用户完成首次初始化；系统扫描仅读取已有版本缓存，避免探测时自动下载引擎。

## 覆盖边界

“所有编程语言”没有固定、可验收的全集。本表说明当前内置定义，不表示每种语言在三个系统上均有自动安装包，也不表示所有商业实现已经接入。

MATLAB、Embarcadero Delphi、SAS、Stata、LabVIEW、ABAP 和 VBA 等专有或嵌入式环境尚无完整内置版本适配。Octave、Free Pascal 对部分 MATLAB / Delphi 语法的兼容不等于支持其专有工具箱、SDK 或授权。SQL 的执行由相应数据库或 SQLite 提供；HTML、CSS、JSON 等文档/样式格式不单独安装语言运行时。

未列出的独立工具可通过 `vsm tools add` 接入有校验摘要的 GitHub 包，或添加外部 manifest 后在 GUI 刷新。新 manifest 支持 `display_name`、`languages`、`notes`、`platforms`、单个 `version_flag` 和含一个捕获组的 `version_pattern`，以复用搜索、能力展示和安全版本探测。依赖授权、复杂环境脚本或无稳定版本接口的实现，需要专门适配和实测后再标为可用。

仍未内置：Wolfram Language、Pharo/Squeak、Eiffel、J/BQN/K、Move/Cairo、AngelScript/Verse、Scratch/Logo/NetLogo、PLC Structured Text/梯形图、Hare、Red/Rebol、Hylo/Vale 等。GDScript 不替代 Godot 的其它脚本插件；APL 入口只覆盖 GNU APL。

## 内置目录

此表根据内置目录生成，以下自动安装状态以 Windows/amd64 为参考；GUI 会按实际系统和架构调整。单元测试验证定义、命令映射、只读探测和错误架构拒绝。集成测试包括 C/C++、.NET 的 C#/F#/VB.NET，以及 Godot/GDScript、Vyper、Arturo、Factor、Haxe、Nim 的真实运行/编译；Linux 额外运行 Gforth、Regina REXX 和 GNU APL。各系统只测试存在对应发行包的组合，其余新增本机工具链未逐一在所有系统实装验证。

| 工具 ID | 名称 / 语言检索词 | 能力 |
| --- | --- | --- |
| `agda` | Agda · Agda | 纳管已有安装 |
| `arturo` | Arturo · Arturo | 自动安装 / 纳管 |
| `ballerina` | Ballerina · Ballerina | 纳管已有安装 |
| `bash` | Bash · Bash / POSIX Shell / Shell Script | 纳管已有安装 |
| `bazel` | bazel | 纳管已有安装 |
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
| `deno` | deno · JavaScript / TypeScript / JSX / TSX | 自动安装 / 纳管 |
| `dmd` | D · DMD · D | 纳管已有安装 |
| `dotnet` | .NET SDK · C# / F# / VB.NET · C# / CSharp / C Sharp / F# / FSharp / Visual Basic .NET / VB.NET / VBNet | 自动安装 / 纳管 |
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
| `haskell` | Haskell · GHC · Haskell | 纳管已有安装 |
| `haxe` | Haxe · Haxe | 自动安装 / 纳管 |
| `idris2` | Idris 2 · Idris | 纳管已有安装 |
| `iverilog` | Icarus Verilog · Verilog / SystemVerilog | 纳管已有安装 |
| `janet` | Janet · Janet | 纳管已有安装 |
| `java` | java · Java / JVM | 自动安装 / 纳管 |
| `julia` | julia · Julia | 自动安装 / 纳管 |
| `just` | just | 自动安装 / 纳管 |
| `jython` | Jython · Python 2 / Jython | 纳管已有安装 |
| `koka` | Koka · Koka | 纳管已有安装 |
| `kotlin` | Kotlin · Kotlin / Kotlin Script | 自动安装 / 纳管 |
| `ldc` | D · LDC · D | 纳管已有安装 |
| `lean` | Lean · Lean | 纳管已有安装 |
| `lua` | Lua · Lua | 纳管已有安装 |
| `luau` | Luau · Luau | 纳管已有安装 |
| `mariadb` | mariadb · SQL | 数据库实例 |
| `maven` | maven | 纳管已有安装 |
| `micropython` | MicroPython · MicroPython / Python | 纳管已有安装 |
| `mlton` | MLton · Standard ML / SML | 纳管已有安装 |
| `mojo` | Mojo · Mojo | 当前平台不支持 |
| `mono` | Mono · C# / CSharp / F# / VB.NET / Mono | 纳管已有安装 |
| `msvc` | MSVC · C / C++ · C / C++ / MSVC / Visual C++ | 纳管已有安装 |
| `mysql` | mysql · SQL | 数据库实例 |
| `nasm` | NASM · Assembly / x86 Assembly / 汇编 | 纳管已有安装 |
| `nim` | Nim · Nim | 自动安装 / 纳管 |
| `ninja` | ninja | 自动安装 / 纳管 |
| `node` | node · JavaScript / TypeScript / Node.js | 自动安装 / 纳管 |
| `nushell` | Nushell · Nushell | 自动安装 / 纳管 |
| `ocaml` | OCaml · OCaml | 纳管已有安装 |
| `octave` | GNU Octave · Octave / MATLAB-compatible syntax | 纳管已有安装 |
| `odin` | Odin · Odin | 纳管已有安装 |
| `perl` | Perl · Perl | 纳管已有安装 |
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
| `redis` | redis · Redis protocol | 数据库实例 |
| `regina` | Regina REXX · REXX / Regina REXX | 纳管已有安装 |
| `rescript` | ReScript · ReScript | 纳管已有安装 |
| `roc` | Roc · Roc | 纳管已有安装 |
| `ruby` | Ruby · Ruby | 纳管已有安装 |
| `rust` | Rust · Rust | 纳管已有安装 |
| `sbcl` | SBCL · Common Lisp · Common Lisp / Lisp | 纳管已有安装 |
| `sbt` | sbt | 纳管已有安装 |
| `scala` | Scala · Scala | 纳管已有安装 |
| `sdcc` | SDCC · C / Embedded C / 嵌入式C | 纳管已有安装 |
| `smalltalk` | GNU Smalltalk · Smalltalk / GNU Smalltalk | 纳管已有安装 |
| `sml` | Standard ML · SML/NJ · Standard ML / SML | 纳管已有安装 |
| `solidity` | Solidity · Solidity / Yul | 纳管已有安装 |
| `sqlite` | SQLite · SQL / SQLite SQL | 纳管已有安装 |
| `swi-prolog` | SWI-Prolog · Prolog / SWI-Prolog | 纳管已有安装 |
| `swift` | Swift · Swift | 纳管已有安装 |
| `tcc` | Tiny C Compiler · C / TinyCC | 纳管已有安装 |
| `tcl` | Tcl / Tk · Tcl / Tk | 纳管已有安装 |
| `tinygo` | TinyGo · Go / TinyGo | 自动安装 / 纳管 |
| `typescript` | TypeScript Compiler · TypeScript / TSX | 纳管已有安装 |
| `typst` | Typst · Typst | 自动安装 / 纳管 |
| `unison` | Unison · Unison | 纳管已有安装 |
| `uv` | uv | 自动安装 / 纳管 |
| `vala` | Vala · Vala / Genie | 纳管已有安装 |
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
