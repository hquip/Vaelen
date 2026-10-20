// 启动即应用已保存的主题（避免首屏闪烁）。
(function () {
  try {
    document.documentElement.setAttribute("data-theme", localStorage.getItem("vsm-theme") || "dark");
  } catch (e) {}
})();

// 通过 Wails 注入的全局对象调用 Go 后端方法（window.go.main.App.*）。
function call(method, ...args) {
  const app = window.go && window.go.main && window.go.main.App;
  if (!app || typeof app[method] !== "function") {
    return Promise.reject(new Error("后端未就绪，请稍候再试"));
  }
  return app[method](...args);
}

let logsBusy = false;
function logsError(message) {
  const error = document.getElementById("logsError");
  error.textContent = message || "";
  error.classList.toggle("hidden", !message);
}
function logsLoading(busy) {
  logsBusy = busy;
  for (const id of ["logsLevel", "logsRefresh", "logsExport", "logsClear", "logsConfirmClear", "logsCancelClear", "logsClose"]) {
    document.getElementById(id).disabled = busy;
  }
}
async function refreshLogs() {
  const snapshot = await call("Logs", 200, document.getElementById("logsLevel").value);
  document.getElementById("logsLocation").textContent = snapshot.directory;
  const bytes = (snapshot.files || []).reduce((sum, file) => sum + file.bytes, 0);
  document.getElementById("logsStatus").textContent = "记录级别: " + snapshot.level + " · " +
    (bytes / 1024).toFixed(1) + " KiB · " + snapshot.entries.length + " 条记录（最新在前） · 本地时间（" + Intl.DateTimeFormat().resolvedOptions().timeZone + "）；导出保留 UTC";
  document.getElementById("logsText").textContent = snapshot.entries.length
    ? [...snapshot.entries].reverse().map(entry => localLogTime(entry.time) + " [" + entry.level.toUpperCase() + "] " +
      entry.component + " · " + entry.event + "\n" + JSON.stringify(entry.fields || {})).join("\n\n")
    : "暂无符合条件的日志。完成操作后点击刷新；调试记录需在启动 VSM 前设置 VSM_LOG_LEVEL=debug。";
}
function localLogTime(value) {
  const time = new Date(value);
  if (!Number.isFinite(time.getTime())) return value;
  const pad = (n, width = 2) => String(n).padStart(width, "0");
  const offset = -time.getTimezoneOffset();
  return time.getFullYear() + "-" + pad(time.getMonth() + 1) + "-" + pad(time.getDate()) + " " +
    pad(time.getHours()) + ":" + pad(time.getMinutes()) + ":" + pad(time.getSeconds()) + "." + pad(time.getMilliseconds(), 3) + " " +
    (offset >= 0 ? "+" : "-") + pad(Math.floor(Math.abs(offset) / 60)) + ":" + pad(Math.abs(offset) % 60);
}
async function logsAction(action) {
  if (logsBusy) return;
  logsLoading(true);
  logsError("");
  try { await action(); }
  catch (error) { logsError(String(error)); }
  finally { logsLoading(false); }
}
document.getElementById("logsBtn").addEventListener("click", () => {
  document.getElementById("logsModal").classList.remove("hidden");
  document.getElementById("logsClearConfirm").classList.add("hidden");
  logsAction(refreshLogs);
});
document.getElementById("logsRefresh").addEventListener("click", () => logsAction(refreshLogs));
document.getElementById("logsLevel").addEventListener("change", () => logsAction(refreshLogs));
document.getElementById("logsExport").addEventListener("click", () => logsAction(async () => {
  const path = await call("ExportLogs");
  if (path) document.getElementById("logsStatus").textContent = "已导出全部保留日志: " + path;
}));
document.getElementById("logsClear").addEventListener("click", () => document.getElementById("logsClearConfirm").classList.remove("hidden"));
document.getElementById("logsCancelClear").addEventListener("click", () => document.getElementById("logsClearConfirm").classList.add("hidden"));
document.getElementById("logsConfirmClear").addEventListener("click", () => logsAction(async () => {
  await call("ClearLogs");
  document.getElementById("logsClearConfirm").classList.add("hidden");
  await refreshLogs();
}));
function closeLogs() {
  if (!logsBusy) document.getElementById("logsModal").classList.add("hidden");
}
document.getElementById("logsClose").addEventListener("click", closeLogs);
document.getElementById("logsModal").addEventListener("click", event => { if (event.target.id === "logsModal") closeLogs(); });
document.addEventListener("keydown", event => { if (event.key === "Escape") closeLogs(); });

let state = [];
let selected = null;
let sysCache = {};
let systemScan = null;
let detectionEpoch = 0;
let detectionRequests = {};
function clearDetectionCache() {
  sysCache = {}; systemScan = null; detectionEpoch++; detectionRequests = {};
}
async function detectedVersions(tool) {
  if (tool && Object.prototype.hasOwnProperty.call(sysCache, tool)) return sysCache[tool];
  if (systemScan !== null) return tool ? systemScan.filter(v => v.tool === tool) : systemScan;
  const key = tool || "*";
  if (tool && detectionRequests["*"]) return (await detectionRequests["*"]).filter(v => v.tool === tool);
  if (detectionRequests[key]) return detectionRequests[key];
  const epoch = detectionEpoch;
  const pending = call(tool ? "DetectTool" : "DetectSystem", ...(tool ? [tool] : [])).then(items => {
    const result = (items || []).filter(v => !tool || v.tool === tool);
    if (epoch === detectionEpoch) {
      if (tool) sysCache[tool] = result;
      else { systemScan = result; for (const s of state) sysCache[s.tool] = result.filter(v => v.tool === s.tool); }
    }
    return result;
  }).finally(() => { if (detectionRequests[key] === pending) delete detectionRequests[key]; });
  detectionRequests[key] = pending;
  return pending;
}
function runtimeID(runtime) { return runtime.managed_version || runtime.version; }
let curDir = "";
let installDir = "";
let projectMode = "";
let projectEnvironment = null;
let projectEnvBusy = false;
let projectEnvFailed = false;
let updateBusy=false;
let updateApplied=false;
let updateRunning=false;
let lastUpdateProgress = null;
let updateProgressAt = 0;
let updateProgressTimer = null;
function updateBytes(bytes) {
  const value = Math.max(0, Number(bytes) || 0);
  if (value < 1024) return Math.round(value) + " B";
  if (value < 1024 * 1024) return (value / 1024).toFixed(1) + " KiB";
  return (value / (1024 * 1024)).toFixed(1) + " MiB";
}
function renderUpdateProgress(progress, received = true) {
  if (!progress || !updateBusy || updateApplied) return;
  if (received) {
    if (!lastUpdateProgress || progress.stage !== lastUpdateProgress.stage ||
        progress.downloaded !== lastUpdateProgress.downloaded || progress.attempt !== lastUpdateProgress.attempt) updateProgressAt = Date.now();
    // Failure events carry no partial-body buffer; keep the last byte count.
    if (progress.stage === "failed" && lastUpdateProgress) progress = {...lastUpdateProgress, stage: "failed"};
    lastUpdateProgress = progress;
  }
  const labels = {checking:"检查发布版本",connecting:"连接 GitHub 下载源",retrying:"重试下载，保留已收数据",parallel:"分块下载更新包",fallback:"分块不可用，改为单连接",downloading:"下载更新包",verifying:"校验 SHA256",extracting:"解压应用",validating:"验证程序与签名",replacing:"备份并替换程序",done:"更新完成",failed:"更新失败"};
  document.getElementById("updateProgress").classList.remove("hidden");
  document.getElementById("updateStage").textContent = labels[progress.stage] || "正在更新";
  const bar = document.getElementById("updateProgressBar");
  const total = Number(progress.overall_total || progress.total || 0);
  const downloaded = Number(progress.overall_downloaded || progress.downloaded || 0);
  if (progress.stage !== "done" && (progress.stage === "checking" || !total)) {
    bar.removeAttribute("value");
    document.getElementById("updatePercent").textContent = "";
  } else {
    const percent = progress.stage === "done" ? 100 : Math.min(100, Math.max(0, 100 * downloaded / total));
    bar.value = percent;
    document.getElementById("updatePercent").textContent = percent.toFixed(1) + "%";
  }
  let detail = progress.asset || "正在连接官方 GitHub…";
  if (progress.file_count) detail = "文件 " + progress.file_index + "/" + progress.file_count + " · " + detail;
  if (["connecting", "retrying", "parallel", "fallback", "downloading"].includes(progress.stage)) {
    const idle = Math.floor((Date.now() - updateProgressAt) / 1000);
    detail += "\n" + updateBytes(downloaded) + " / " + updateBytes(total);
    detail += idle >= 3 ? " · 等待网络数据 " + idle + " 秒" : " · " + updateBytes(progress.bytes_per_second) + "/s";
    if (progress.attempt > 1) detail += " · 第 " + progress.attempt + "/3 次尝试";
    if (progress.connections > 1) detail += " · " + progress.connections + " 连接分块";
    if (progress.source) detail += progress.source === "github-api" ? " · GitHub 官方接口" : progress.source === "github-cdn" ? " · GitHub 官方 CDN" : " · GitHub 官方下载";
    if (progress.stage === "connecting" || idle >= 3) detail += "\n连续 15 秒无数据会自动重试；支持分块时保留已收进度。";
  }
  document.getElementById("updateProgressDetails").textContent = detail;
}

async function checkVSMUpdate(){
  if(updateBusy||updateApplied)return;
  updateBusy=true;
  const status=document.getElementById("updateStatus"),error=document.getElementById("updateError");
  error.classList.add("hidden");document.getElementById("updateApply").disabled=true;document.getElementById("updateCheck").disabled=true;
  document.getElementById("updateProgress").classList.add("hidden");
  status.textContent="正在检查官方 GitHub Release…";
  try{const info=await call("CheckUpdate");status.textContent="当前 "+info.current+" · 最新 "+info.latest+(info.available?" · 可以更新":" · 已是最新版本");document.getElementById("updateNotes").textContent=info.notes||"暂无发布说明";document.getElementById("updateApply").disabled=!info.available;}
  catch(e){error.textContent=errMsg(e);error.classList.remove("hidden");status.textContent="检查失败，可配置代理后重试";}
  finally{updateBusy=false;document.getElementById("updateCheck").disabled=false;}
}
document.getElementById("updateBtn").addEventListener("click",()=>{document.getElementById("updateModal").classList.remove("hidden");checkVSMUpdate();});
document.getElementById("updateCheck").addEventListener("click",checkVSMUpdate);
document.getElementById("updateClose").addEventListener("click",()=>document.getElementById("updateModal").classList.add("hidden"));
document.getElementById("updateApply").addEventListener("click",async()=>{
  if(updateBusy)return;updateBusy=true;document.getElementById("updateApply").disabled=true;document.getElementById("updateCheck").disabled=true;
  const status=document.getElementById("updateStatus"),error=document.getElementById("updateError");error.classList.add("hidden");status.textContent="正在更新，下载完成后会校验并替换程序…";
  updateRunning = true;
  lastUpdateProgress = null;
  renderUpdateProgress({stage:"checking"});
  clearInterval(updateProgressTimer);
  updateProgressTimer = setInterval(() => { if (lastUpdateProgress) renderUpdateProgress(lastUpdateProgress, false); }, 1000);
  try{const result=await call("ApplyUpdate");renderUpdateProgress({...lastUpdateProgress,stage:"done"});updateApplied=true;status.textContent="已更新到 "+result.version+"，请关闭并重新启动 VSM。";if(result.backups&&result.backups.length)document.getElementById("updateNotes").textContent+="\n\n旧程序备份（重启后可删除）：\n"+result.backups.join("\n");}
  catch(e){renderUpdateProgress({...lastUpdateProgress,stage:"failed"});error.textContent=errMsg(e);error.classList.remove("hidden");status.textContent="更新失败，请检查原因后重试";document.getElementById("updateApply").disabled=false;}
  finally{clearInterval(updateProgressTimer);updateProgressTimer=null;updateRunning=false;updateBusy=false;document.getElementById("updateCheck").disabled=updateApplied;}
});
let mcpClients = [];

function projectEnvError(message) {
  const box = document.getElementById("projectEnvError");
  box.textContent = message || "";
  box.classList.toggle("hidden", !message);
  if (message) box.scrollIntoView({ block: "nearest" });
}

function clearMCPConfig() {
  document.getElementById("projectMCPConfig").value = "";
  document.getElementById("projectMCPConfig").classList.add("hidden");
  document.getElementById("projectMCPCopy").classList.add("hidden");
}

function renderProjectEnvControls() {
  const locked = projectEnvBusy || !curDir || projectEnvFailed;
  document.getElementById("projectEnvPick").disabled = projectEnvBusy;
  document.getElementById("projectEnvMode").disabled = locked || !!projectEnvironment;
  document.getElementById("projectEnvImage").disabled = locked || !!projectEnvironment;
  const init = document.getElementById("projectEnvInit");
  init.disabled = locked || !!projectEnvironment;
  init.textContent = projectEnvironment ? "环境已初始化" : "为此项目初始化环境";
  document.getElementById("projectMCPGenerate").disabled = locked || !mcpClients.length;
  document.getElementById("projectMCPClient").disabled = projectEnvBusy || !mcpClients.length;
  document.getElementById("projectMCPReadOnly").disabled = projectEnvBusy;
  document.getElementById("projectMCPCopy").disabled = projectEnvBusy;
  document.getElementById("projectEnvTools").disabled = projectEnvBusy;
}

function renderProjectEnvMode() {
  const mode = document.getElementById("projectEnvMode").value;
  const hints = {
    local: "本项目独立保存工具、缓存和 Python venv，不修改系统环境。运行项目命令时使用 vsm run；这不是操作系统沙箱。",
    versions: "与其它项目共用已安装的工具，每个项目分别选择版本。适合节省磁盘空间，不隔离依赖缓存。",
    docker: "在 Docker 容器中运行，需要已启动的 Docker 和包含所需语言的镜像。项目目录可写，其余根文件系统只读。"
  };
  document.getElementById("projectEnvModeHint").textContent = hints[mode];
  document.getElementById("projectEnvImageRow").classList.toggle("hidden", mode !== "docker");
  document.getElementById("projectEnvCommands").textContent = "vsm use go 1.25.11\nvsm run " +
    (mode === "local" ? "--install-missing " : "") + "-- go version";
}

function renderMCPClient() {
  clearMCPConfig();
  const client = mcpClients.find(item => item.id === document.getElementById("projectMCPClient").value);
  if (!client) return;
  document.getElementById("projectMCPLocation").textContent = client.location;
  document.getElementById("projectMCPInstructions").textContent = client.instructions;
  document.getElementById("projectMCPGenerate").textContent = "生成 " + client.format.toUpperCase() + " 配置";
  document.getElementById("projectMCPConfig").setAttribute("aria-label", client.name + " MCP 配置");
  document.getElementById("projectMCPToolsHint").textContent = document.getElementById("projectMCPReadOnly").checked
    ? "只读连接提供 7 个查看工具，包含任务图、锁文件状态和工具目录。"
    : "连接后提供 14 个工具，支持锁定工具链、项目任务和安装；任务需用户先检查并信任，AI 无权授予信任。";
}

async function loadMCPClients() {
  if (mcpClients.length) return;
  const clients = await call("MCPClients");
  if (!Array.isArray(clients) || !clients.length) throw new Error("无法加载 AI 客户端预设，请使用新版 GUI");
  mcpClients = clients;
  const select = document.getElementById("projectMCPClient");
  select.replaceChildren(...clients.map(client => {
    const option = document.createElement("option");
    option.value = client.id; option.textContent = client.name;
    return option;
  }));
  let preferred = "cursor";
  try { preferred = localStorage.getItem("vsm-mcp-client") || preferred; } catch (_) {}
  select.value = clients.some(client => client.id === preferred) ? preferred : clients[0].id;
  renderMCPClient();
}

async function projectEnvTask(action) {
  if (projectEnvBusy) return;
  projectEnvBusy = true;
  projectEnvError("");
  renderProjectEnvControls();
  try { await action(); }
  catch (e) { projectEnvError(errMsg(e)); }
  finally { projectEnvBusy = false; renderProjectEnvControls(); }
}

async function refreshProjectEnvironment() {
  projectEnvFailed = false;
  projectEnvironment = null;
  projectEnvError("");
  clearMCPConfig();
  renderProjectEnvControls();
  document.getElementById("projectEnvStatus").textContent = "正在读取项目状态…";
  try {
    curDir = (await call("CurrentDir")) || "";
    document.getElementById("projectEnvDir").textContent = curDir || "尚未选择项目目录";
    document.getElementById("projectEnvPick").textContent = curDir ? "更换项目目录" : "选择项目目录";
    projectEnvironment = curDir ? await call("ProjectEnvironment") : null;
    projectMode = projectEnvironment ? projectEnvironment.mode : "";
    document.getElementById("projectEnvMode").value = projectMode || "local";
    document.getElementById("projectEnvImage").value = projectEnvironment ? projectEnvironment.image || "" : "";
    document.getElementById("projectEnvStatus").textContent = !curDir
      ? "请选择代码所在的文件夹。选择目录不会创建文件。"
      : projectEnvironment
        ? "已初始化 · " + projectMode + " · 环境所属项目：" + projectEnvironment.root + "。已有环境保持当前模式。"
        : "尚未初始化。点击下方按钮，将在此项目内创建 .vsm/project.json。";
  } catch (e) {
    projectEnvFailed = true;
    projectMode = "";
    document.getElementById("projectEnvStatus").textContent = "无法读取环境，请检查配置或选择其它项目目录。";
    projectEnvError(errMsg(e));
  } finally {
    document.getElementById("projectEnvNext").classList.toggle("hidden", !projectEnvironment);
    document.getElementById("projectEnvTools").classList.toggle("hidden", !projectEnvironment);
    renderProjectEnvMode();
    renderProjectEnvControls();
    document.querySelector("#projectEnvModal .project-env-body").scrollTop = 0;
  }
}

document.getElementById("projectEnvBtn").addEventListener("click", async () => {
  document.getElementById("projectEnvModal").classList.remove("hidden");
  await projectEnvTask(async () => { await refreshProjectEnvironment(); await loadMCPClients(); });
});
document.getElementById("projectEnvPick").addEventListener("click", async () => {
  await projectEnvTask(async () => {
    const dir = await call("PickProjectDir");
    if (!dir) return;
    await refreshProjectEnvironment();
    await load(); await loadDataDir(); await loadInstallRoot();
  });
});
document.getElementById("projectEnvMode").addEventListener("change", renderProjectEnvMode);
document.getElementById("projectEnvClose").addEventListener("click", () => document.getElementById("projectEnvModal").classList.add("hidden"));
document.getElementById("projectEnvTools").addEventListener("click", () => {
  document.getElementById("projectEnvModal").classList.add("hidden");
  document.getElementById("filter").focus();
});
document.getElementById("projectEnvInit").addEventListener("click", async () => {
  await projectEnvTask(async () => {
    const mode = document.getElementById("projectEnvMode").value;
    const image = mode === "docker" ? document.getElementById("projectEnvImage").value.trim() : "";
    if (mode === "docker" && !image) throw new Error("请填写包含项目工具链的 Docker 镜像，例如 golang:1.25.11");
    await call("InitProjectEnvironment", mode, image);
    await refreshProjectEnvironment();
    await load(); await loadDataDir(); await loadInstallRoot();
    toast("项目环境已初始化：" + mode);
  });
});
document.getElementById("projectMCPGenerate").addEventListener("click", async () => {
  clearMCPConfig();
  await projectEnvTask(async () => {
    const client = document.getElementById("projectMCPClient").value;
    const readOnly = document.getElementById("projectMCPReadOnly").checked;
    const result = await call("MCPConfigForClient", client, readOnly);
    if (!result || typeof result.content !== "string" || !result.content.trim()) throw new Error("未能生成客户端配置，请检查 CLI 后重试");
    const box = document.getElementById("projectMCPConfig"); box.value = result.content; box.classList.remove("hidden");
    document.getElementById("projectMCPCopy").classList.remove("hidden");
    box.scrollIntoView({ block: "nearest" });
  });
});
document.getElementById("projectMCPClient").addEventListener("change", () => {
  projectEnvError("");
  renderMCPClient();
  try { localStorage.setItem("vsm-mcp-client", document.getElementById("projectMCPClient").value); } catch (_) {}
});
document.getElementById("projectMCPReadOnly").addEventListener("change", renderMCPClient);
document.getElementById("projectMCPCopy").addEventListener("click", async () => {
  const box = document.getElementById("projectMCPConfig");
  try { await navigator.clipboard.writeText(box.value); toast("MCP 配置已复制，请加入 AI 客户端的 MCP 设置"); }
  catch (_) { box.focus(); box.select(); toast("请复制选中的配置，加入 AI 客户端的 MCP 设置"); }
});

// ====== 语言图标 & 规范显示名 ======
// 仅需特殊大小写 / 符号的显示名在此覆盖，其余按首字母大写兜底。
const DISPLAY_NAMES = {
  node: "Node.js", dotnet: ".NET", csharp: "C#", fsharp: "F#", cpp: "C++",
  php: "PHP", tinygo: "TinyGo", cmake: "CMake", purescript: "PureScript",
  typescript: "TypeScript", javascript: "JavaScript", nushell: "Nushell",
  ocaml: "OCaml", vlang: "V", uv: "uv", graalvm: "GraalVM", openjdk: "OpenJDK",
};

function displayName(tool) {
  if (DISPLAY_NAMES[tool]) return DISPLAY_NAMES[tool];
  return tool.charAt(0).toUpperCase() + tool.slice(1);
}

// 品牌色（c=主色, c2=渐变副色可选, fg=文字色可选, g=徽标文字）。
// 未登记的语言走名字 hash 生成稳定彩色 + 首字母，保证人人有彩色图标。
const BRAND = {
  go:        { c: "#00ADD8", c2: "#0094b8", g: "Go" },
  node:      { c: "#5FA04E", c2: "#3c873a", g: "n" },
  python:    { c: "#3776AB", c2: "#FFD343", g: "Py" },
  java:      { c: "#E76F00", c2: "#C7300C", g: "J" },
  zig:       { c: "#F7A41D", c2: "#ec8a00", fg: "#1a1a1a", g: "Z" },
  deno:      { c: "#2b2b2b", c2: "#4a4a4a", g: "D" },
  bun:       { c: "#FBF0DF", c2: "#f2e3c8", fg: "#1a1a1a", g: "B" },
  gleam:     { c: "#FFAFF3", c2: "#ff7eea", fg: "#1a1a1a", g: "★" },
  vlang:     { c: "#5D87BF", c2: "#3f6aa6", g: "V" },
  kotlin:    { c: "#7F52FF", c2: "#E44857", g: "K" },
  tinygo:    { c: "#00ADD8", c2: "#0094b8", g: "tG" },
  nushell:   { c: "#4E9A06", c2: "#3a7404", g: "Nu" },
  cmake:     { c: "#064F8C", c2: "#0a6bbd", g: "C" },
  ninja:     { c: "#3b3b3b", c2: "#5a5a5a", g: "Nj" },
  typst:     { c: "#239DAD", c2: "#1b7a87", g: "t" },
  just:      { c: "#384EFF", c2: "#1f33d6", g: "ju" },
  uv:        { c: "#DE5FE9", c2: "#7E56C2", g: "uv" },
  rust:      { c: "#33332f", c2: "#5a5852", g: "Rs" },
  dart:      { c: "#0175C2", c2: "#00C7B7", g: "Dt" },
  php:       { c: "#777BB4", c2: "#565f97", g: "php" },
  dotnet:    { c: "#512BD4", c2: "#7a4ef0", g: ".N" },
  csharp:    { c: "#512BD4", c2: "#7a4ef0", g: "C#" },
  fsharp:    { c: "#378BBA", c2: "#2a6f96", g: "F#" },
  ruby:      { c: "#CC342D", c2: "#9b1c16", g: "Rb" },
  elixir:    { c: "#4B275F", c2: "#6e3d8a", g: "Ex" },
  swift:     { c: "#F05138", c2: "#d63a1f", g: "Sw" },
  scala:     { c: "#DC322F", c2: "#a81e1b", g: "Sc" },
  elm:       { c: "#1293D8", c2: "#0e76ad", g: "El" },
  purescript:{ c: "#1d222a", c2: "#3b4250", g: "Ps" },
  lean:      { c: "#6b4fbb", c2: "#4f388f", g: "Le" },
  solidity:  { c: "#363636", c2: "#5a5a5a", g: "Sol" },
  maven:     { c: "#C71A36", c2: "#9b1228", g: "Mv" },
  gradle:    { c: "#02303A", c2: "#0B7B8E", g: "Gr" },
  haskell:   { c: "#5D4F85", c2: "#453a66", g: "Hs" },
  r:         { c: "#276DC3", c2: "#1c5499", g: "R" },
  vala:      { c: "#7239B3", c2: "#552a87", g: "Va" },
  perl:      { c: "#39457E", c2: "#2a3460", g: "Pl" },
  lua:       { c: "#2C2D72", c2: "#1f2057", g: "Lua" },
  julia:     { c: "#9558B2", c2: "#CB3C33", g: "Jl" },
  crystal:   { c: "#222", c2: "#444", g: "Cr" },
  nim:       { c: "#FFE953", c2: "#F3D400", fg: "#1a1a1a", g: "Nim" },
  ocaml:     { c: "#EC6813", c2: "#c2530c", g: "Ml" },
  erlang:    { c: "#A90533", c2: "#7d0426", g: "Er" },
  clojure:   { c: "#5881D8", c2: "#63B132", g: "Cl" },
  groovy:    { c: "#4298B8", c2: "#317890", g: "Gv" },
  typescript:{ c: "#3178C6", c2: "#2461a8", g: "TS" },
  javascript:{ c: "#F7DF1E", c2: "#e6cf00", fg: "#1a1a1a", g: "JS" },
};

// 字符串稳定 hash → 色相，给未登记语言兜底彩色（同名永远同色）。
function hashHue(s) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360;
  return h;
}

// 生成一个语言图标（圆角彩色徽标）。size 为像素边长。
function langIcon(tool, size) {
  const sz = size || 24;
  const b = BRAND[tool];
  let c1, c2, fg, glyph;
  if (b) {
    c1 = b.c;
    c2 = b.c2 || b.c;
    fg = b.fg || "#fff";
    glyph = b.g;
  } else {
    const hue = hashHue(tool);
    c1 = `hsl(${hue},52%,42%)`;
    c2 = `hsl(${(hue + 26) % 360},56%,52%)`;
    fg = "#fff";
    glyph = (tool[0] || "?").toUpperCase();
  }
  const ratio = glyph.length >= 3 ? 0.34 : glyph.length === 2 ? 0.44 : 0.52;
  const fs = Math.round(sz * ratio);
  const radius = Math.round(sz * 0.28);
  return (
    `<span class="lang-icon" style="width:${sz}px;height:${sz}px;` +
    `border-radius:${radius}px;background:linear-gradient(135deg,${c1},${c2});` +
    `color:${fg};font-size:${fs}px">${escapeHtml(glyph)}</span>`
  );
}

async function load() {
  try {
    state = await call("Status");
  } catch (e) {
    document.getElementById("toolList").innerHTML =
      `<div class="empty-hint" style="padding:12px 10px">加载失败：${escapeHtml(e.message)}</div>`;
    return;
  }
  renderStat();
  renderSidebar();
  if (selected && statusOf(selected)) renderDetail(selected);
}

function renderStat() {
  const total = state.length;
  const installed = state.filter((s) => s.installed && s.installed.length).length;
  document.getElementById("stat").textContent = `共 ${total} 种 · ${installed} 已安装`;
}

function renderSidebar() {
  const filter = (document.getElementById("filter").value || "").toLowerCase();
  const box = document.getElementById("toolList");
  box.innerHTML = "";

  const match = state.filter((s) => !filter || s.tool.toLowerCase().includes(filter));
  if (match.length === 0) {
    box.innerHTML = `<div class="empty-hint" style="padding:12px 10px">无匹配语言</div>`;
    return;
  }

  const installedTools = match.filter((s) => s.installed && s.installed.length);
  const otherTools = match.filter((s) => !(s.installed && s.installed.length));

  if (installedTools.length) box.appendChild(buildGroup("已安装", installedTools));
  if (otherTools.length) {
    box.appendChild(buildGroup(installedTools.length ? "未安装" : "全部语言", otherTools));
  }
}

function buildGroup(label, items) {
  const group = document.createElement("div");
  group.className = "tool-group";
  const head = document.createElement("div");
  head.className = "group-label";
  head.innerHTML = `<span>${escapeHtml(label)}</span><span class="gcount">${items.length}</span>`;
  group.appendChild(head);

  const ul = document.createElement("ul");
  ul.className = "tool-list";
  for (const s of items) {
    const li = document.createElement("li");
    li.className = "tool-item" + (s.tool === selected ? " selected" : "");
    const hasInstalled = s.installed && s.installed.length;
    const badge = s.current
      ? `<span class="tbadge">${escapeHtml(s.current)}</span>`
      : `<span class="tdot ${hasInstalled ? "on" : ""}"></span>`;
    li.innerHTML =
      `<span class="titem-left">${langIcon(s.tool, 24)}` +
      `<span class="tname">${escapeHtml(displayName(s.tool))}</span></span>${badge}`;
    li.addEventListener("click", () => select(s.tool));
    ul.appendChild(li);
  }
  group.appendChild(ul);
  return group;
}

function select(tool) {
  selected = tool;
  installDir = "";
  renderSidebar();
  renderDetail(tool);
}

function statusOf(tool) {
  return state.find((s) => s.tool === tool);
}

function renderDetail(tool) {
  const databaseTool = ["postgres","mysql","mariadb","redis","valkey"].includes(tool);
  const s = statusOf(tool);
  const detail = document.getElementById("detail");
  if (!s) {
    detail.innerHTML = `<div class="placeholder"><p class="ph-title">未找到 ${escapeHtml(tool)}</p></div>`;
    return;
  }

  const isActive = !!s.current;
  const installedCount = (s.installed && s.installed.length) || 0;
  const headBadge = projectMode === "docker" ? `<span class="head-badge on">Docker 容器</span>` : isActive
    ? `<span class="head-badge on">● VSM 选择 ${escapeHtml(s.current)}</span>`
    : `<span class="head-badge off">未启用</span>`;
  const cards = `
    <div class="stat-cards">
      <div class="stat-card">
        <div class="sc-label">VSM 选择版本</div>
        <div class="sc-value ${isActive ? "green" : "muted"}">${projectMode === "docker" ? "运行时校验镜像版本" : isActive ? escapeHtml(s.current) : "未设置"}</div>
      </div>
      <div class="stat-card">
        <div class="sc-label">来源</div>
        <div class="sc-value ${s.source ? "" : "muted"}">${s.source ? escapeHtml(s.source) : "—"}</div>
      </div>
      <div class="stat-card">
        <div class="sc-label">版本要求</div>
        <div class="sc-value ${s.spec ? "" : "muted"}">${s.spec ? escapeHtml(s.spec) : "未指定"}</div>
      </div>
      <div class="stat-card">
        <div class="sc-label">${projectMode === "docker" ? "运行位置" : "已安装"}</div>
        <div class="sc-value">${projectMode === "docker" ? "Docker 容器" : installedCount + " 个"}</div>
      </div>
      <div class="stat-card clickable" id="projDirCard" title="点击选择项目目录（「设为本目录」写入其中的 .tool-versions）">
        <div class="sc-label">项目目录 (local) ✎</div>
        <div class="sc-value ${curDir ? "" : "muted"}">${curDir ? escapeHtml(curDir) : "未选择，点击选择"}</div>
      </div>
    </div>`;

  const hint = projectMode === "local" || projectMode === "docker"
    ? `<p class="act-hint">项目隔离模式：在项目目录使用 <code>vsm run -- &lt;命令&gt;</code>，不会修改系统 PATH。可在「隔离环境 / MCP」生成 AI 客户端配置。</p>`
    : installedCount
    ? `<p class="act-hint">全局默认用于未指定项目版本的目录；项目配置和环境变量仍优先。</p>`
    : "";

  let versHtml;
  if (s.installed && s.installed.length) {
    versHtml =
      `<ul class="vers">` +
      s.installed
        .map(
          (v) => `
      <li class="ver ${v === s.current ? "active" : ""}">
        <span class="vname">${escapeHtml(v)}</span>
        <span class="vactions">
          <button class="mini" data-act="local" data-ver="${escapeHtml(v)}">设为本目录</button>
          ${projectMode === "local" || projectMode === "docker" ? "" : `<button class="mini" data-act="global" data-ver="${escapeHtml(v)}">设为全局</button>`}
          <button class="mini danger" data-act="uninstall" data-ver="${escapeHtml(v)}">卸载</button>
        </span>
      </li>`
        )
        .join("") +
      `</ul>`;
  } else {
    versHtml = `<div class="empty-hint">尚无已安装版本</div>`;
  }

  detail.innerHTML = `
    <div class="detail-head">${langIcon(tool, 42)}<h2>${escapeHtml(displayName(tool))}</h2>${headBadge}</div>
    ${cards}
    ${databaseTool ? `<div class="section"><p class="project-hint">本页管理数据库程序与命令路径。运行中的服务版本、端口和独立数据，请通过数据库实例入口切换。</p><button id="openDatabaseTool" class="primary-btn">管理 ${escapeHtml(tool)} 实例与版本</button></div>` : ""}
    ${hint}
    ${s.activation ? `<details class="act-hint"><summary>版本来源与命令路径</summary><strong>全局默认：</strong>${escapeHtml(s.activation.global || "未设置")}<br /><strong>VSM 选择的程序：</strong><code>${escapeHtml(s.activation.executable || "未找到")}</code><br /><strong>GUI 启动时的 PATH：</strong><code>${escapeHtml(s.activation.command || "未找到")}</code>${(s.activation.warnings || []).map(w => `<p>${escapeHtml(w)}</p>`).join("")}</details>` : ""}

    ${projectMode === "docker" ? `
    <div class="section">
      <p class="section-title">镜像中的工具链</p>
      <p class="act-hint">请在「隔离环境 / MCP」查看项目镜像。需要其它工具版本时，准备包含该版本的镜像；运行命令前会检查镜像是否满足项目要求。</p>
      <label class="project-field-label" for="dockerVersionInput">本项目的 ${escapeHtml(displayName(tool))} 版本要求</label>
      <div class="install-row">
        <input id="dockerVersionInput" value="${escapeHtml(s.spec || "")}" placeholder="输入版本或前缀" autocomplete="off" />
        <button id="dockerVersionSave" class="primary-btn">保存项目版本要求</button>
      </div>
    </div>` : `
    <div class="section">
      <p class="section-title">已安装版本</p>
      ${versHtml}
    </div>

    <div class="section">
      <p class="section-title">系统检测到的版本 <span class="muted-tag">非 vsm 管理</span><button id="adoptToolAllBtn" class="mini" style="margin-left:10px;vertical-align:middle">全部纳管</button><button id="addToolPathBtn" class="mini">添加路径</button><button id="rescanToolBtn" class="mini">重新检测</button></p>
      <div id="sysVers"><div class="empty-hint">检测中…</div></div>
    </div>

    <div class="section">
      <p class="section-title">${databaseTool ? "获取数据库程序" : "安装新版本"}</p>
      ${databaseTool ? `<p class="project-hint">Docker 实例会自动拉取官方镜像；原生程序请先安装完整发行包，再点击「添加路径」纳管。</p><button id="databaseInstallEntry" class="ghost-btn">打开数据库实例</button>` : `
      <div class="install-row">
        <input id="versionInput" placeholder="latest（聚焦可加载远端版本，输入可筛选）" autocomplete="off" list="remoteList" />
        <datalist id="remoteList"></datalist>
        <button id="installBtn" class="primary-btn">安装</button>
      </div>
      <div class="install-path">
        <button id="pickDirBtn" class="mini">📁 选择安装位置</button>
        <span id="installDirLabel" class="install-dir-label">默认目录</span>
      </div>
      <label class="install-path" title="发布方没有提供 SHA256 校验值时停止安装">
        <input id="requireChecksum" type="checkbox"> 要求发布方 SHA256 校验
      </label>
      <div id="installProgress" class="progress hidden"><div class="progress-bar" id="installBar"></div><span class="progress-text" id="installPct"></span></div>
      <pre id="installLog" class="log"></pre>`}
    </div>`}`;

  detail.querySelectorAll("button[data-act]").forEach((btn) => {
    btn.addEventListener("click", () => onVerAction(tool, btn.dataset.act, btn.dataset.ver));
  });
  for (const id of ["openDatabaseTool","databaseInstallEntry"]) { const button=document.getElementById(id);if(button)button.addEventListener("click",()=>openDatabaseManager(tool)); }
  const ata = document.getElementById("adoptToolAllBtn");
  if (ata) ata.addEventListener("click", () => onAdoptToolAll(tool));
  const addPath = document.getElementById("addToolPathBtn");
  if (addPath) addPath.addEventListener("click", () => openManualRuntime(tool));
  const rescan = document.getElementById("rescanToolBtn");
  if (rescan) rescan.addEventListener("click", () => { clearDetectionCache(); loadSysVers(tool); });
  const pdc = document.getElementById("projDirCard");
  if (pdc) pdc.addEventListener("click", () => pickProjectDir(tool));
  const verInput = document.getElementById("versionInput");
  const dlist = document.getElementById("remoteList");
  if (verInput) verInput.addEventListener("focus", () => loadRemote(tool, verInput, dlist), { once: true });
  const installButton = document.getElementById("installBtn");
  if (installButton) installButton.addEventListener("click", () => doInstall(tool));
  const dockerVersionSave = document.getElementById("dockerVersionSave");
  if (dockerVersionSave) dockerVersionSave.addEventListener("click", async () => {
    const version = document.getElementById("dockerVersionInput").value.trim();
    if (!version) { toast("请输入项目版本要求", true); return; }
    dockerVersionSave.disabled = true;
    try { await onVerAction(tool, "local", version); }
    finally { dockerVersionSave.disabled = false; }
  });
  const pickBtn = document.getElementById("pickDirBtn");
  if (pickBtn) {
    pickBtn.addEventListener("click", async () => {
      try {
        const d = await call("PickDirectory", "选择 " + tool + " 安装位置");
        if (d) installDir = d;
        updateInstallDirLabel();
      } catch (e) {
        toast(e.message, true);
      }
    });
  }
  const dirLbl = document.getElementById("installDirLabel");
  if (dirLbl) dirLbl.addEventListener("click", () => { installDir = ""; updateInstallDirLabel(); });
  updateInstallDirLabel();
  if (projectMode !== "docker") loadSysVers(tool);
}

function updateInstallDirLabel() {
  const el = document.getElementById("installDirLabel");
  if (!el) return;
  if (installDir) {
    el.textContent = "→ " + installDir + "  ✕";
    el.title = "点此清除，恢复默认目录";
    el.classList.add("set");
  } else {
    el.textContent = "默认目录";
    el.title = "";
    el.classList.remove("set");
  }
}

function isInstalledVersion(tool, ver) {
  const s = statusOf(tool);
  return !!(s && s.installed && s.installed.indexOf(ver) >= 0);
}

function sysRow(tool, v) {
  const versionLabel = (v.version || "版本读取失败") + (v.environment ? " · " + v.environment : "");
  const versionHint = v.version_error || (v.version ? "" : "已找到可执行文件，但未能读取版本；请检查安装后重新检测。");
  const action = !v.version
    ? ""
    : isInstalledVersion(tool, runtimeID(v))
      ? `<span class="sys-tag">已纳管</span>`
      : `<button class="mini" data-tool="${escapeHtml(tool)}" data-adopt="${escapeHtml(runtimeID(v))}" data-path="${escapeHtml(v.path)}">纳管</button>`;
  return `
      <li class="ver${!v.version ? " probe-failed" : ""}">
        <span class="vname" title="${escapeHtml(versionHint)}">${escapeHtml(versionLabel)}</span>
        <span class="sys-right">
          <span class="sys-path" title="${escapeHtml(v.path)}">${escapeHtml(v.path)}</span>
          ${action}
        </span>
        ${!v.version ? `<div class="probe-error">${escapeHtml(versionHint)}</div>` : ""}
      </li>`;
}

function bindAdopt(box) {
  box.querySelectorAll("button[data-adopt]").forEach((b) => {
    b.addEventListener("click", () => onAdopt(b.dataset.tool, b.dataset.adopt, b.dataset.path));
  });
}

function renderSysVers(tool, box, list) {
  if (!list || !list.length) {
    box.innerHTML = `<div class="empty-hint">系统 PATH 未检测到</div>`;
    return;
  }
  box.innerHTML = `<ul class="vers">` + list.map((v) => sysRow(tool, v)).join("") + `</ul>`;
  bindAdopt(box);
}

async function onAdopt(tool, version, path) {
  try {
    await call("AdoptTool", tool, version, path);
    toast(`已纳管 ${tool} ${version}`);
    const view = selected;
    await load();
    if (view === null && selected === null) showDetectOverview();
  } catch (e) {
    toast("纳管失败: " + e.message, true);
  }
}

async function onAdoptToolAll(tool) {
  try {
    const n = await call("AdoptDetected", await detectedVersions(tool));
    toast(`已纳管 ${tool} ${n} 个版本`);
  } catch (e) {
    // 部分成功时后端也会返回错误（列出失败项），这里照样刷新界面
    toast(errMsg(e), true);
  }
  await load();
}

async function onAdoptAll() {
  try {
    const n = await call("AdoptDetected", await detectedVersions());
    toast(`已纳管 ${n} 个系统版本`);
  } catch (e) {
    toast(errMsg(e), true);
  }
  await load();
  if (selected === null) showDetectOverview();
}

// Wails 把 Go 的 error 以字符串或 Error 对象形式抛给前端，这里统一取文本。
function errMsg(e) {
  if (!e) return "未知错误";
  if (typeof e === "string") return e;
  return e.message || String(e);
}

async function loadSysVers(tool) {
  const epoch = detectionEpoch;
  let box = document.getElementById("sysVers");
  if (!box) return;
  if (sysCache[tool]) {
    renderSysVers(tool, box, sysCache[tool]);
    return;
  }
  box.innerHTML = `<div class="empty-hint">检测中…</div>`;
  let list;
  try {
    list = await detectedVersions(tool);
  } catch (e) {
    box = document.getElementById("sysVers");
    if (box && selected === tool && epoch === detectionEpoch) {
      box.innerHTML = `<div class="empty-hint">检测失败：${escapeHtml(e.message)}</div>`;
    }
    return;
  }
  box = document.getElementById("sysVers");
  if (box && selected === tool && epoch === detectionEpoch) renderSysVers(tool, box, list);
}

async function showDetectOverview() {
  const epoch = detectionEpoch;
  selected = null;
  renderSidebar();
  const detail = document.getElementById("detail");
  detail.innerHTML = `
    <div class="detail-head"><h2>系统检测</h2><span class="head-badge off">外部运行时</span><button id="manualRuntimeBtn" class="mini" style="margin-left:auto">手动添加路径</button><button id="rescanAllBtn" class="mini">重新检测</button><button id="adoptAllBtn" class="mini">全部纳管</button></div>
    <p class="overview-sub">扫描 PATH、语言环境变量和已有 Conda Python 环境。纳管后复用本次结果；新增或删除外部安装后点击「重新检测」。扫描不到的安装可手动添加路径。</p>
    <div id="detectAll"><div class="empty-hint">检测中…</div></div>`;
  const aab = document.getElementById("adoptAllBtn");
  if (aab) aab.addEventListener("click", onAdoptAll);
  document.getElementById("manualRuntimeBtn").addEventListener("click", () => openManualRuntime());
  document.getElementById("rescanAllBtn").addEventListener("click", () => { clearDetectionCache(); showDetectOverview(); });
  let list;
  try {
    list = await detectedVersions();
  } catch (e) {
    const b = document.getElementById("detectAll");
    if (b && epoch === detectionEpoch) b.innerHTML = `<div class="empty-hint">检测失败：${escapeHtml(e.message)}</div>`;
    return;
  }
  const box = document.getElementById("detectAll");
  if (!box || epoch !== detectionEpoch) return;
  if (!list || !list.length) {
    box.innerHTML = `<div class="empty-hint">系统 PATH 未检测到任何运行时</div>`;
    return;
  }
  const groups = {};
  for (const v of list) (groups[v.tool] = groups[v.tool] || []).push(v);
  box.innerHTML = Object.keys(groups)
    .sort()
    .map(
      (tool) => `
    <div class="section">
      <p class="section-title">${escapeHtml(tool)} <span class="gcount">${groups[tool].length}</span></p>
      <ul class="vers">
        ${groups[tool].map((v) => sysRow(tool, v)).join("")}
      </ul>
    </div>`
    )
    .join("");
  bindAdopt(box);
}

let manualRuntimeBusy = false;
function resetManualRuntimePreview() {
  document.getElementById("manualRuntimePreview").classList.add("hidden");
  document.getElementById("manualRuntimeAdopt").disabled = true;
  document.getElementById("manualRuntimeError").classList.add("hidden");
}
function openManualRuntime(tool) {
  if (projectMode === "local" || projectMode === "docker") { toast("隔离项目请独立安装工具；纳管已有运行时请使用 versions 模式", true); return; }
  const select = document.getElementById("manualRuntimeTool");
  select.innerHTML = state.map(s => `<option value="${escapeHtml(s.tool)}">${escapeHtml(displayName(s.tool))}</option>`).join("");
  select.value = tool || (state.some(s => s.tool === "python") ? "python" : state[0]?.tool || "");
  document.getElementById("manualRuntimePath").value = "";
  document.getElementById("manualRuntimeAlias").value = "";
  resetManualRuntimePreview();
  document.getElementById("manualRuntimeModal").classList.remove("hidden");
}
async function manualRuntimeAction(adopt) {
  if (manualRuntimeBusy) return;
  manualRuntimeBusy = true;
  const ids = ["Tool","Path","Alias","Pick","Inspect","Adopt","Close"];
  const controls = ids.map(id => document.getElementById("manualRuntime" + id));
  controls.forEach(c => c.disabled = true);
  const tool = document.getElementById("manualRuntimeTool").value;
  const path = document.getElementById("manualRuntimePath").value.trim();
  const alias = document.getElementById("manualRuntimeAlias").value.trim();
  const error = document.getElementById("manualRuntimeError");
  error.classList.add("hidden");
  let inspected = false;
  try {
    const runtime = await (adopt ? call("AdoptCustomTool", tool, path, alias) : call("InspectToolPath", tool, path));
    inspected = true;
    const preview = document.getElementById("manualRuntimePreview");
    preview.textContent = "检测版本：" + runtime.version + (runtime.environment ? "\n环境：" + runtime.environment : "") + "\n管理标识：" + (alias || runtimeID(runtime)) + "\n主命令：" + runtime.path;
    preview.classList.remove("hidden");
    if (adopt) {
      if (systemScan !== null) systemScan.push(runtime);
      if (sysCache[tool]) sysCache[tool].push(runtime);
      document.getElementById("manualRuntimeModal").classList.add("hidden");
      toast("已纳管 " + tool + " " + runtimeID(runtime));
      await load();
      if (selected === null) showDetectOverview();
    }
  } catch (e) { error.textContent = errMsg(e); error.classList.remove("hidden"); }
  finally { manualRuntimeBusy = false; controls.forEach(c => c.disabled = false); document.getElementById("manualRuntimeAdopt").disabled = !inspected; }
}
for (const id of ["manualRuntimeTool","manualRuntimePath","manualRuntimeAlias"]) document.getElementById(id).addEventListener("input", resetManualRuntimePreview);
document.getElementById("manualRuntimePick").addEventListener("click", async () => {
  try { const path = await call("PickDirectory", "选择已有运行时或 Conda 环境目录"); if (path) { document.getElementById("manualRuntimePath").value = path; resetManualRuntimePreview(); } }
  catch (e) { document.getElementById("manualRuntimeError").textContent = errMsg(e); document.getElementById("manualRuntimeError").classList.remove("hidden"); }
});
document.getElementById("manualRuntimeInspect").addEventListener("click", () => manualRuntimeAction(false));
document.getElementById("manualRuntimeAdopt").addEventListener("click", () => manualRuntimeAction(true));
document.getElementById("manualRuntimeClose").addEventListener("click", () => document.getElementById("manualRuntimeModal").classList.add("hidden"));

async function loadRemote(tool, input, dlist) {
  const ph = input.placeholder;
  input.placeholder = "加载远端版本中…";
  try {
    const vers = await call("RemoteVersions", tool);
    // 全量填入 datalist：浏览器原生按输入子串筛选，版本再多也能搜到。
    dlist.innerHTML = vers.map((v) => `<option value="${escapeHtml(v)}"></option>`).join("");
    input.placeholder = vers.length
      ? `latest（共 ${vers.length} 个远端版本，输入可筛选）`
      : ph;
  } catch (e) {
    input.placeholder = ph;
    toast("远端版本加载失败：" + e.message, true);
  }
}

async function doInstall(tool) {
  const version = (document.getElementById("versionInput").value || "").trim() || "latest";
  const log = document.getElementById("installLog");
  const btn = document.getElementById("installBtn");
  const wrap = document.getElementById("installProgress");
  if (wrap) {
    wrap.classList.remove("hidden");
    document.getElementById("installBar").style.width = "0%";
    document.getElementById("installPct").textContent = "0%";
  }
  log.classList.add("show");
  log.textContent = `开始安装 ${tool} ${version} …\n`;
  btn.disabled = true;
  try {
    await call("InstallVerified", tool, version, installDir,
      document.getElementById("requireChecksum").checked);
    toast(`已安装 ${tool} ${version}`);
    await load();
    renderDetail(tool);
  } catch (e) {
    log.textContent += "失败: " + e.message + "\n";
    btn.disabled = false;
    toast("安装失败: " + e.message, true);
  }
}

// pickProjectDir 让用户选择项目目录；返回是否选中（取消返回 false）。
async function pickProjectDir(tool) {
  try {
    const d = await call("PickProjectDir");
    if (!d) return false;
    curDir = d;
    await loadCurrentDir();
    toast("项目目录已设为 " + d);
    await load();
    await loadDataDir(); await loadInstallRoot();
    if (tool && selected === tool) renderDetail(tool);
    return true;
  } catch (e) {
    toast(errMsg(e), true);
    return false;
  }
}

async function onVerAction(tool, act, ver) {
  const buttons = act === "global" ? [...document.querySelectorAll('[data-act="global"]')] : [];
  buttons.forEach(button => button.disabled = true);
  try {
    if (act === "local") {
      try {
        await call("SetLocal", tool, ver);
      } catch (e) {
        // 从 Finder / 资源管理器启动时没有项目目录：先让用户选，再写入
        if (!/请先选择项目目录/.test(errMsg(e))) throw e;
        if (!(await pickProjectDir())) return;
        await call("SetLocal", tool, ver);
      }
      toast(`已设 ${tool} = ${ver}（${curDir}）`);
    } else if (act === "global") {
      await call("SetGlobal", tool, ver);
      toast(`已设置 ${tool} 全局默认 = ${ver}`);
    } else if (act === "uninstall") {
      await call("Uninstall", tool, ver);
      toast(`已卸载 ${tool} ${ver}`);
    }
    await load();
    renderDetail(tool);
  } catch (e) {
    toast(errMsg(e), true);
  } finally {
    buttons.forEach(button => button.disabled = false);
  }
}

function bindEvents() {
  if (window.runtime && typeof window.runtime.EventsOn === "function") {
    window.runtime.EventsOn("path:progress", progress => {
      const button = document.getElementById("pathBtn");
      if (button.disabled && progress.stage === "downloading") {
        const percent = progress.total > 0 ? Math.floor(100 * progress.downloaded / progress.total) : 0;
        button.textContent = `准备运行组件 ${percent}%`;
      }
    });
    window.runtime.EventsOn("update:progress", progress => { if (updateRunning) renderUpdateProgress(progress); });
    window.runtime.EventsOn("install:progress", (msg) => {
      const log = document.getElementById("installLog");
      if (log) {
        log.classList.add("show");
        log.textContent += "  " + msg + "\n";
        log.scrollTop = log.scrollHeight;
      }
      const m = /(\d+)%/.exec(msg);
      const wrap = document.getElementById("installProgress");
      if (wrap && m) {
        wrap.classList.remove("hidden");
        document.getElementById("installBar").style.width = m[1] + "%";
        document.getElementById("installPct").textContent = m[1] + "%";
      }
    });
  }
}

let toastTimer = null;
function toast(msg, isErr) {
  const el = document.getElementById("toast");
  el.textContent = msg;
  el.className = "toast" + (isErr ? " err" : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.add("hidden"), 3200);
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

document.getElementById("filter").addEventListener("input", renderSidebar);
document.getElementById("refreshBtn").addEventListener("click", async () => { clearDetectionCache(); await load(); if (selected === null && document.getElementById("detectAll")) showDetectOverview(); });
document.getElementById("detectBtn").addEventListener("click", showDetectOverview);
document.getElementById("mirrorSel").addEventListener("change", async (e) => {
  try {
    await call("SetMirror", e.target.value);
    toast("下载镜像已设为 " + (e.target.value === "cn" ? "国内加速" : e.target.value === "custom" ? "自定义（使用 vsm mirror set 配置来源）" : "关闭"));
  } catch (err) {
    toast(err.message, true);
  }
});

const THEME_NAMES = { dark: "深蓝", midnight: "纯黑", emerald: "祖母绿", violet: "紫罗兰", amber: "琥珀", rose: "玫红", light: "浅色" };
document.getElementById("themeSel").addEventListener("change", (e) => {
  const t = e.target.value;
  document.documentElement.setAttribute("data-theme", t);
  try { localStorage.setItem("vsm-theme", t); } catch (err) {}
  toast("主题已切换为「" + (THEME_NAMES[t] || t) + "」");
});

function openProxyModal(cur) {
  const modal = document.getElementById("proxyModal");
  const input = document.getElementById("proxyInput");
  input.value = /^(off|none|direct|no)$/i.test(cur) ? "" : cur;
  modal.classList.remove("hidden");
  setTimeout(() => input.focus(), 30);
}
function closeProxyModal() {
  document.getElementById("proxyModal").classList.add("hidden");
}
async function applyProxy(value) {
  try {
    await call("SetProxy", value);
    await loadProxy();
    const v = (value || "").trim();
    if (v === "") toast("代理已设为跟随系统环境变量");
    else if (/^(off|none|direct|no)$/i.test(v)) toast("代理已禁用（强制直连）");
    else toast("代理已设为 " + v);
    closeProxyModal();
  } catch (err) {
    toast(err.message, true);
  }
}
document.getElementById("proxyBtn").addEventListener("click", async () => {
  let cur = "";
  try { cur = (await call("GetProxy")) || ""; } catch (e) {}
  openProxyModal(cur);
});
document.getElementById("proxySaveBtn").addEventListener("click", () => {
  applyProxy(document.getElementById("proxyInput").value);
});
document.getElementById("proxyDirectBtn").addEventListener("click", () => applyProxy("off"));
document.getElementById("proxyCancelBtn").addEventListener("click", closeProxyModal);
document.getElementById("proxyModal").addEventListener("click", (e) => {
  if (e.target.id === "proxyModal") closeProxyModal();
});
document.getElementById("proxyInput").addEventListener("keydown", (e) => {
  if (e.key === "Enter") applyProxy(e.target.value);
  else if (e.key === "Escape") closeProxyModal();
});
document.getElementById("pathBtn").addEventListener("click", async () => {
  const button = document.getElementById("pathBtn");
  const label = button.textContent;
  button.disabled = true;
  button.textContent = "正在配置并验证…";
  try {
    const res = (await call("PathInit")) || {};
    toast(res.message || "终端激活已配置");
    await load();
    if (selected) renderDetail(selected);
  } catch (e) {
    toast(errMsg(e), true);
  } finally {
    button.disabled = false;
    button.textContent = label;
  }
});

document.getElementById("setRootBtn").addEventListener("click", async (e) => {
  e.stopPropagation();
  try {
    const d = await call("PickDirectory", "选择安装根目录（所有版本存放处）");
    if (!d) return;
    await call("SetInstallRoot", d);
    toast("安装目录已设为 " + d);
    await load();
    await loadInstallRoot();
  } catch (err) {
    toast(err.message, true);
  }
});

async function loadDataDir() {
  try {
    const dir = await call("DataDir");
    if (!dir) return;
    document.getElementById("dataDir").textContent = dir;
    document.getElementById("dataDirWrap").title = dir;
  } catch (e) {
    // 后端未就绪时忽略，保留占位文案
  }
}

async function loadCurrentDir() {
  projectMode = "";
  try {
    curDir = (await call("CurrentDir")) || "";
    const env = curDir ? await call("ProjectEnvironment") : null;
    projectMode = env ? env.mode : "";
  } catch (e) {
    // Preserve a selected directory even if its environment config is broken,
    // so the project dialog can display the error and let the user choose again.
  }
  if (selected) renderDetail(selected);
}

async function loadMirror() {
  try {
    const m = await call("GetMirror");
    const sel = document.getElementById("mirrorSel");
    if (sel) sel.value = m === "cn" || m === "custom" ? m : "off";
  } catch (e) {
    // 后端未就绪时忽略
  }
}

function loadTheme() {
  let t = "dark";
  try { t = localStorage.getItem("vsm-theme") || "dark"; } catch (e) {}
  document.documentElement.setAttribute("data-theme", t);
  const sel = document.getElementById("themeSel");
  if (sel) sel.value = t;
}

async function loadProxy() {
  try {
    const p = (await call("GetProxy")) || "";
    const btn = document.getElementById("proxyBtn");
    if (!btn) return;
    if (p === "") {
      btn.textContent = "🌐 代理";
      btn.title = "网络代理: 跟随系统环境变量";
    } else if (/^(off|none|direct|no)$/i.test(p)) {
      btn.textContent = "🌐 直连";
      btn.title = "网络代理: 强制直连（忽略系统代理）";
    } else {
      btn.textContent = "🌐 代理 ●";
      btn.title = "网络代理: " + p;
    }
  } catch (e) {
    // 后端未就绪时忽略
  }
}

async function loadInstallRoot() {
  try {
    const r = await call("GetInstallRoot");
    if (!r) return;
    document.getElementById("installRoot").textContent = r;
    document.getElementById("installRootWrap").title = r;
  } catch (e) {
    // 后端未就绪时忽略
  }
}

// 标记运行平台：macOS 隐藏标题栏后需要给左上角红绿灯按钮留位（见 style.css）。
// 先用 UA 立即判断避免首屏跳动，再以后端返回值为准。
function applyPlatform(p) {
  if (!p) return;
  const root = document.documentElement;
  root.classList.remove("platform-windows", "platform-darwin", "platform-linux");
  root.classList.add("platform-" + p);
}
(function () {
  const ua = navigator.userAgent || "";
  if (/Mac OS X|Macintosh/.test(ua)) applyPlatform("darwin");
  else if (/Windows/.test(ua)) applyPlatform("windows");
  else if (/Linux/.test(ua)) applyPlatform("linux");
})();

async function loadPlatform() {
  try {
    applyPlatform(await call("Platform"));
  } catch (e) {
    // 后端未就绪时保留 UA 推断结果
  }
}

async function loadVersion() {
  try {
    document.getElementById("appVersion").textContent = await call("Version");
  } catch (e) {
    // The native window title still identifies the running build.
  }
}

window.addEventListener("DOMContentLoaded", () => {
  bindEvents();
  setTimeout(() => {
    loadPlatform();
    loadVersion();
    load();
    loadDataDir();
    loadCurrentDir();
    loadMirror();
    loadProxy();
    loadTheme();
    loadInstallRoot();
  }, 120);
});
