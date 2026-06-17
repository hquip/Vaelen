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

let state = [];
let selected = null;
let sysCache = {};
let curDir = "";
let installDir = "";

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
    li.innerHTML = `<span class="tname">${escapeHtml(s.tool)}</span>${badge}`;
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
  const s = statusOf(tool);
  const detail = document.getElementById("detail");
  if (!s) {
    detail.innerHTML = `<div class="placeholder"><p class="ph-title">未找到 ${escapeHtml(tool)}</p></div>`;
    return;
  }

  const isActive = !!s.current;
  const installedCount = (s.installed && s.installed.length) || 0;
  const headBadge = isActive
    ? `<span class="head-badge on">● ${escapeHtml(s.current)} 生效中</span>`
    : `<span class="head-badge off">未启用</span>`;
  const cards = `
    <div class="stat-cards">
      <div class="stat-card">
        <div class="sc-label">当前版本</div>
        <div class="sc-value ${isActive ? "green" : "muted"}">${isActive ? escapeHtml(s.current) : "未设置"}</div>
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
        <div class="sc-label">已安装</div>
        <div class="sc-value">${installedCount} 个</div>
      </div>
      <div class="stat-card">
        <div class="sc-label">当前目录 (local)</div>
        <div class="sc-value ${curDir ? "" : "muted"}" title="${escapeHtml(curDir)}">${curDir ? escapeHtml(curDir) : "—"}</div>
      </div>
    </div>`;

  const hint = installedCount
    ? `<p class="act-hint">💡 终端里让 <code>${escapeHtml(tool)}</code> 用 vsm 选的版本需先激活：点顶部「⚡ 激活PATH」，或终端运行 <code>vsm activate &lt;shell&gt;</code>。快速验证：<code>vsm exec ${escapeHtml(tool)} -- ${escapeHtml(tool)} --version</code></p>`
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
          <button class="mini" data-act="global" data-ver="${escapeHtml(v)}">设为全局</button>
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
    <div class="detail-head"><h2>${escapeHtml(tool)}</h2>${headBadge}</div>
    ${cards}
    ${hint}

    <div class="section">
      <p class="section-title">已安装版本</p>
      ${versHtml}
    </div>

    <div class="section">
      <p class="section-title">系统检测到的版本 <span class="muted-tag">非 vsm 管理</span><button id="adoptToolAllBtn" class="mini" style="margin-left:10px;vertical-align:middle">全部纳管</button></p>
      <div id="sysVers"><div class="empty-hint">检测中…</div></div>
    </div>

    <div class="section">
      <p class="section-title">安装新版本</p>
      <div class="install-row">
        <input id="versionInput" placeholder="latest" autocomplete="off" />
        <select id="remoteSelect"><option value="">远端版本（点击加载）</option></select>
        <button id="installBtn" class="primary-btn">安装</button>
      </div>
      <div class="install-path">
        <button id="pickDirBtn" class="mini">📁 选择安装位置</button>
        <span id="installDirLabel" class="install-dir-label">默认目录</span>
      </div>
      <div id="installProgress" class="progress hidden"><div class="progress-bar" id="installBar"></div><span class="progress-text" id="installPct"></span></div>
      <pre id="installLog" class="log"></pre>
    </div>`;

  detail.querySelectorAll("button[data-act]").forEach((btn) => {
    btn.addEventListener("click", () => onVerAction(tool, btn.dataset.act, btn.dataset.ver));
  });
  const ata = document.getElementById("adoptToolAllBtn");
  if (ata) ata.addEventListener("click", () => onAdoptToolAll(tool));
  const sel = document.getElementById("remoteSelect");
  sel.addEventListener("mousedown", () => loadRemote(tool, sel), { once: true });
  sel.addEventListener("change", () => {
    if (sel.value) document.getElementById("versionInput").value = sel.value;
  });
  document.getElementById("installBtn").addEventListener("click", () => doInstall(tool));
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
  loadSysVers(tool);
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
  const action = !v.version
    ? ""
    : isInstalledVersion(tool, v.version)
      ? `<span class="sys-tag">已纳管</span>`
      : `<button class="mini" data-tool="${escapeHtml(tool)}" data-adopt="${escapeHtml(v.version)}" data-path="${escapeHtml(v.path)}">纳管</button>`;
  return `
      <li class="ver">
        <span class="vname">${escapeHtml(v.version || "?")}</span>
        <span class="sys-right">
          <span class="sys-path" title="${escapeHtml(v.path)}">${escapeHtml(v.path)}</span>
          ${action}
        </span>
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
    await load();
    if (selected === tool) renderDetail(tool);
    else showDetectOverview();
  } catch (e) {
    toast("纳管失败: " + e.message, true);
  }
}

async function onAdoptToolAll(tool) {
  try {
    const n = await call("AdoptToolAll", tool);
    toast(`已纳管 ${tool} ${n} 个版本`);
    await load();
    if (selected === tool) renderDetail(tool);
  } catch (e) {
    toast("全部纳管失败: " + e.message, true);
  }
}

async function onAdoptAll() {
  try {
    const n = await call("AdoptAll");
    toast(`已纳管 ${n} 个系统版本`);
    await load();
    showDetectOverview();
  } catch (e) {
    toast("全部纳管失败: " + e.message, true);
  }
}

async function loadSysVers(tool) {
  let box = document.getElementById("sysVers");
  if (!box) return;
  if (sysCache[tool]) {
    renderSysVers(tool, box, sysCache[tool]);
    return;
  }
  box.innerHTML = `<div class="empty-hint">检测中…</div>`;
  let list;
  try {
    list = await call("DetectTool", tool);
  } catch (e) {
    box = document.getElementById("sysVers");
    if (box && selected === tool) {
      box.innerHTML = `<div class="empty-hint">检测失败：${escapeHtml(e.message)}</div>`;
    }
    return;
  }
  sysCache[tool] = list || [];
  box = document.getElementById("sysVers");
  if (box && selected === tool) renderSysVers(tool, box, sysCache[tool]);
}

async function showDetectOverview() {
  selected = null;
  renderSidebar();
  const detail = document.getElementById("detail");
  detail.innerHTML = `
    <div class="detail-head"><h2>系统检测</h2><span class="head-badge off">非 vsm 管理</span><button id="adoptAllBtn" class="mini" style="margin-left:auto">全部纳管</button></div>
    <p class="overview-sub">扫描系统 PATH，列出所有非 vsm 安装的语言运行时（含多版本）。点「纳管」加入单个，或「全部纳管」一次纳入全部。</p>
    <div id="detectAll"><div class="empty-hint">检测中…</div></div>`;
  const aab = document.getElementById("adoptAllBtn");
  if (aab) aab.addEventListener("click", onAdoptAll);
  let list;
  try {
    list = await call("DetectSystem");
  } catch (e) {
    const b = document.getElementById("detectAll");
    if (b) b.innerHTML = `<div class="empty-hint">检测失败：${escapeHtml(e.message)}</div>`;
    return;
  }
  const box = document.getElementById("detectAll");
  if (!box) return;
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

async function loadRemote(tool, sel) {
  sel.innerHTML = `<option value="">加载中…</option>`;
  try {
    const vers = await call("RemoteVersions", tool);
    sel.innerHTML =
      `<option value="">— 选择版本 —</option>` +
      vers.slice(0, 100).map((v) => `<option value="${escapeHtml(v)}">${escapeHtml(v)}</option>`).join("");
  } catch (e) {
    sel.innerHTML = `<option value="">加载失败（网络 / 无下载源）</option>`;
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
    await call("Install", tool, version, installDir);
    toast(`已安装 ${tool} ${version}`);
    await load();
    renderDetail(tool);
  } catch (e) {
    log.textContent += "失败: " + e.message + "\n";
    btn.disabled = false;
    toast("安装失败: " + e.message, true);
  }
}

async function onVerAction(tool, act, ver) {
  try {
    if (act === "local") {
      await call("SetLocal", tool, ver);
      toast(`已设 ${tool} 当前目录 = ${ver}`);
    } else if (act === "global") {
      await call("SetGlobal", tool, ver);
      toast(`已设 ${tool} 全局 = ${ver}`);
    } else if (act === "uninstall") {
      await call("Uninstall", tool, ver);
      toast(`已卸载 ${tool} ${ver}`);
    }
    await load();
    renderDetail(tool);
  } catch (e) {
    toast(e.message, true);
  }
}

function bindEvents() {
  if (window.runtime && typeof window.runtime.EventsOn === "function") {
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
document.getElementById("refreshBtn").addEventListener("click", load);
document.getElementById("detectBtn").addEventListener("click", showDetectOverview);
document.getElementById("mirrorSel").addEventListener("change", async (e) => {
  try {
    await call("SetMirror", e.target.value);
    toast("下载镜像已设为 " + (e.target.value === "cn" ? "国内加速" : "关闭"));
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
  try {
    const res = await call("PathInit");
    toast(res === "added" ? "已写入用户 PATH 并广播，新终端生效" : "shims 已在用户 PATH 中");
  } catch (e) {
    toast("写入 PATH 失败: " + e.message, true);
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
  try {
    curDir = (await call("CurrentDir")) || "";
  } catch (e) {
    curDir = "";
  }
  if (selected) renderDetail(selected);
}

async function loadMirror() {
  try {
    const m = await call("GetMirror");
    const sel = document.getElementById("mirrorSel");
    if (sel) sel.value = m === "cn" ? "cn" : "off";
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

window.addEventListener("DOMContentLoaded", () => {
  bindEvents();
  setTimeout(() => {
    load();
    loadDataDir();
    loadCurrentDir();
    loadMirror();
    loadProxy();
    loadTheme();
    loadInstallRoot();
  }, 120);
});
