package plugin

import "sort"

// HostGuide is a searchable integration entry, deliberately not a Plugin.
// It cannot install, create shims, or claim an independently switched runtime.
type HostGuide struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Languages []string `json:"languages"`
	Mode      string   `json:"mode"`
	Host      string   `json:"host"`
	Limit     string   `json:"limit"`
	Steps     []string `json:"steps"`
	URL       string   `json:"url"`
}

var hostGuides = []HostGuide{
	{ID: "vba", Name: "VBA · Office", Languages: []string{"VBA", "Visual Basic for Applications"}, Mode: "host", Host: "Microsoft Office / Microsoft 365", Limit: "VBA 随 Office 应用提供，不能独立安装或通过 PATH 切换；宏兼容性还取决于宿主版本、位数、操作系统和引用库。", Steps: []string{"在目标机器安装并授权需要的 Office 版本。", "在项目文档记录 Excel/Word/Access 版本、32/64 位、引用库及宏要求。", "通过目标 Office 应用打开工程并测试宏；VSM 不修改宏安全设置或 VBA 引用。"}, URL: "https://learn.microsoft.com/office/vba/api/overview/"},
	{ID: "abap", Name: "ABAP · SAP", Languages: []string{"ABAP", "ABAP Objects"}, Mode: "host", Host: "SAP ABAP Platform / SAP BTP ABAP Environment", Limit: "ABAP 编译与运行由 SAP 后端提供；本机 PATH、SAP GUI 或 ADT 版本不会切换服务器上的 ABAP 语言版本。", Steps: []string{"准备已授权的 SAP 后端及开发账号。", "在 ADT 中选择正确系统和开发项目，记录后端版本及 ABAP 语言版本。", "在对应后端执行语法检查、ABAP Unit 和传输流程；VSM 不自动连接或修改 SAP 系统。"}, URL: "https://help.sap.com/docs/abap-cloud"},
	{ID: "verse", Name: "Verse · UEFN", Languages: []string{"Verse"}, Mode: "host", Host: "Unreal Editor for Fortnite (UEFN)", Limit: "Verse 工具链随 UEFN/Fortnite 提供，不能独立用 VSM 切换；工程还受平台发布版本和资源兼容性约束。", Steps: []string{"通过官方启动器安装相应 UEFN。", "在项目中记录 UEFN/Fortnite 版本和所需插件。", "使用对应编辑器构建和运行 Verse，并在升级前备份工程。"}, URL: "https://dev.epicgames.com/documentation/uefn/verse-language-reference"},
	{ID: "plc", Name: "PLC · ST / 梯形图", Languages: []string{"Structured Text", "ST", "IEC 61131-3", "Ladder Diagram", "梯形图", "Instruction List", "SFC"}, Mode: "host", Host: "CODESYS / TIA Portal / TwinCAT / OpenPLC 等", Limit: "PLC 语言取决于厂商 IDE、目标运行时、设备描述及固件；更改 PATH 不会切换控制器上的运行时，也不保证各厂商方言兼容。", Steps: []string{"选定 IDE、编译器版本、目标设备和固件。", "将工程、设备描述、库版本及构建指令纳入项目记录。", "先在仿真环境验证，再按设备维护流程部署；VSM 不自动刷固件或操作现场设备。"}, URL: "https://www.codesys.com/the-system/programming/"},
	{ID: "angelscript", Name: "AngelScript · 嵌入式脚本", Languages: []string{"AngelScript"}, Mode: "host", Host: "集成 AngelScript SDK 的应用或游戏引擎", Limit: "语言运行时嵌入宿主；不同应用注册的 API、编译选项和 SDK 版本可能不同，没有通用的独立运行时切换。", Steps: []string{"确认宿主应用及其 AngelScript SDK 版本。", "固定宿主构建、脚本 API 和 SDK 编译选项。", "使用宿主提供的脚本入口或测试程序执行；如项目自带独立 runner，可通过外部工具定义单独接入。"}, URL: "https://www.angelcode.com/angelscript/"},
	{ID: "scratch", Name: "Scratch", Languages: []string{"Scratch"}, Mode: "host", Host: "Scratch 网页编辑器 / Scratch Desktop", Limit: "Scratch 由编辑器及其 VM 运行；VSM 不把编辑器版本伪装成可通过 PATH 切换的独立语言版本。", Steps: []string{"选择官方网页编辑器或相应桌面版。", "记录编辑器版本、扩展、设备要求并保存项目文件。", "在目标编辑器中打开并测试项目；VSM 不替换项目 VM 或扩展。"}, URL: "https://scratch.mit.edu/download"},
	{ID: "snap-language", Name: "Snap!", Languages: []string{"Snap!", "Snap Programming Language"}, Mode: "host", Host: "Snap! 网页编辑器", Limit: "此入口指编程语言 Snap!，不是 Linux Snap 包管理器；版本由网页或本地部署的编辑器决定。", Steps: []string{"选择官方服务或固定版本的本地 Snap! 部署。", "记录编辑器版本和依赖库，导出项目备份。", "在对应编辑器中执行并验证项目。"}, URL: "https://snap.berkeley.edu/"},
	{ID: "wolfram", Name: "Wolfram Language", Languages: []string{"Wolfram Language", "Mathematica"}, Mode: "host", Host: "Mathematica / Wolfram Engine / WolframScript", Limit: "实际计算版本由所选 Wolfram Kernel 及授权决定；仅替换 wolframscript 包装器不能证明 Kernel 已切换。", Steps: []string{"安装并激活目标 Mathematica 或 Wolfram Engine。", "在宿主中选择准确的 Kernel 路径并记录版本；项目任务可使用已确认的完整可执行路径。", "运行 $Version 和项目测试确认所用 Kernel；VSM 不申请授权或静默启动 Kernel 探测。"}, URL: "https://reference.wolfram.com/language/"},
	{ID: "sas", Name: "SAS", Languages: []string{"SAS"}, Mode: "host", Host: "SAS Foundation / SAS Viya", Limit: "SAS 版本与部署、产品组件及授权绑定；Viya 的服务端运行环境不能通过本机 PATH 切换。", Steps: []string{"确定本机 Foundation 安装或 Viya 服务端及授权。", "记录版本、组件、编码、配置文件和连接目标。", "使用目标部署执行测试；VSM 不启动 SAS 会话探测或更改服务端配置。"}, URL: "https://documentation.sas.com/"},
	{ID: "stata", Name: "Stata", Languages: []string{"Stata", "Mata"}, Mode: "host", Host: "Stata / Stata SE / Stata MP", Limit: "版本、产品版本和许可共同决定可用功能；VSM 不替换 Stata 授权文件或宣称已验证所有宿主版本。", Steps: []string{"安装并激活目标版本与产品版本。", "在 do-file 中声明适当的 version，并记录实际宿主版本及插件。", "通过所选 Stata 完整路径运行测试或批处理；文件、数据与授权由用户管理。"}, URL: "https://www.stata.com/manuals/"},
	{ID: "labview", Name: "LabVIEW", Languages: []string{"LabVIEW", "G Language", "G 图形化语言"}, Mode: "host", Host: "NI LabVIEW / LabVIEW Runtime / 驱动及工具包", Limit: "VI 的编辑与运行受 LabVIEW、运行时、驱动、工具包和许可证共同约束；选择运行时不会自动转换工程或部署硬件。", Steps: []string{"安装匹配的 LabVIEW、运行时、驱动和工具包。", "记录工程版本、目标平台及所有依赖。", "在副本上验证工程转换及 VI 测试，硬件部署按现有维护流程执行。"}, URL: "https://www.ni.com/docs/en-US/bundle/labview/"},
	{ID: "visual-basic6", Name: "Visual Basic 6", Languages: []string{"Visual Basic 6", "VB6", "Classic Visual Basic"}, Mode: "host", Host: "Visual Basic 6 IDE / VB6 Runtime", Limit: "VB6 与现代 VB.NET 是不同工具链；IDE、COM/ActiveX 组件及位数要求由宿主安装负责，VSM 不切换系统 COM 注册。", Steps: []string{"准备符合授权与平台要求的 VB6 开发环境。", "记录运行库、COM/ActiveX 组件和工程编译设置。", "在隔离测试环境中编译验证；现代 VB.NET 请选 .NET SDK。"}, URL: "https://learn.microsoft.com/previous-versions/visualstudio/visual-basic-6/visual-basic-6-support-policy"},
	{ID: "e-language", Name: "易语言", Languages: []string{"易语言", "E Language"}, Mode: "host", Host: "易语言 IDE 与支持库", Limit: "工程版本、IDE、支持库及授权共同决定兼容性；VSM 不独立安装或切换其系统组件。", Steps: []string{"准备相应 IDE 版本及支持库。", "记录工程、支持库和编译配置，并保留可恢复副本。", "通过目标 IDE 或其官方命令行接口构建测试。"}, URL: "https://www.dywt.com.cn/"},
	{ID: "oracle-sql", Name: "Oracle · PL/SQL", Languages: []string{"PL/SQL", "Oracle SQL"}, Mode: "host", Host: "Oracle Database / SQLcl", Limit: "PL/SQL 的实际编译与运行版本由 Oracle 服务端及兼容性设置决定；切换 SQL 客户端不会切换数据库引擎。", Steps: []string{"准备正确的数据库服务、客户端及授权。", "记录服务端版本、兼容性设置、模式和连接目标。", "在目标数据库执行迁移及测试；VSM 不自动连接数据库或修改数据。"}, URL: "https://docs.oracle.com/en/database/"},
	{ID: "tsql", Name: "SQL Server · T-SQL", Languages: []string{"T-SQL", "Transact-SQL"}, Mode: "host", Host: "SQL Server / Azure SQL", Limit: "T-SQL 能力取决于服务端版本、产品版本和数据库兼容级别；本机客户端版本不能替代服务端切换。", Steps: []string{"选择正确的 SQL Server/Azure SQL 环境。", "记录服务端版本、兼容级别及连接配置。", "在测试库中验证迁移和查询；VSM 不修改服务或数据库。"}, URL: "https://learn.microsoft.com/sql/t-sql/language-reference"},
	{ID: "dyalog", Name: "Dyalog APL", Languages: []string{"Dyalog APL"}, Mode: "host", Host: "Dyalog APL", Limit: "Dyalog 的工作区、运行时和授权需配套管理；GNU APL 的兼容入口不代表 Dyalog 已被独立适配。", Steps: []string{"安装并授权目标 Dyalog。", "记录工作区格式、版本和所需组件。", "在对应宿主测试，保留升级前工作区副本。"}, URL: "https://docs.dyalog.com/"},
	{ID: "kdb", Name: "kdb+ / Q", Languages: []string{"Q", "Kdb+", "QSQL"}, Mode: "host", Host: "KX kdb+", Limit: "Q 与数据库引擎、进程和授权配套；切换客户端不会切换已有服务进程或数据格式。Kona 由独立工具入口管理，不与 kdb+ 或 ngn/k 互相替代。", Steps: []string{"安装并授权目标 kdb+ 版本。", "记录服务器、客户端、连接目标及数据格式要求。", "在测试环境验证版本与数据兼容性；VSM 不操作已有服务进程或数据库。"}, URL: "https://code.kx.com/q/"},
}

func Guide(name string) (HostGuide, bool) {
	for _, entry := range hostGuides {
		if entry.ID == name {
			entry.Languages = append([]string{}, entry.Languages...)
			entry.Steps = append([]string{}, entry.Steps...)
			return entry, true
		}
	}
	return HostGuide{}, false
}
func CatalogNames() []string {
	names := Names()
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
	}
	for _, entry := range hostGuides {
		if !seen[entry.ID] {
			names = append(names, entry.ID)
		}
	}
	sort.Strings(names)
	return names
}
