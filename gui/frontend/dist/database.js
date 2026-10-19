let databaseBusy = false;
let databaseCancelable = false;
let databaseInstances = [];
let databaseEngines = [];
const db = id => document.getElementById("database" + id);
function databaseError(error) { db("Error").textContent = error ? errMsg(error) : ""; db("Error").classList.toggle("hidden", !error); }
function databaseOutput(text) { const out=db("Output");out.textContent=(out.textContent+String(text)+"\n").slice(-120000);out.classList.remove("hidden");out.scrollTop=out.scrollHeight; }
function clearDatabaseConnection() { db("Connection").value="";db("ConnectionWrap").classList.add("hidden"); }
function databaseControls() {
  const selected=databaseInstances.find(i=>i.name===db("Instance").value);
  const dirty=!!selected&&(db("Version").value!==selected.version||Number(db("Port").value)!==selected.port);
  for(const id of ["Instance","Name","Engine","Backend","Version","Port","Adopt","Save","Start","Stop","Logs","Connect","Refresh","Close"]) db(id).disabled=databaseBusy;
  db("Name").disabled=databaseBusy||!!selected;db("Engine").disabled=databaseBusy||!!selected;db("Backend").disabled=databaseBusy||!!selected;
  db("Start").disabled=databaseBusy||!selected||selected.running||dirty;
  db("Stop").disabled=databaseBusy||!selected||!selected.running;
  db("Logs").disabled=databaseBusy||!selected;db("Connect").disabled=databaseBusy||!selected||dirty;
  db("Cancel").disabled=!databaseBusy||!databaseCancelable;
  const native=db("Backend").value==="native";
  db("Adopt").classList.toggle("hidden",!native);db("NativeHint").classList.toggle("hidden",!native);
}
async function databaseVersionOptions() {
  const selected=databaseInstances.find(i=>i.name===db("Instance").value);
  const engine=db("Engine").value,backend=db("Backend").value;
  let versions=Object.keys(selected?.versions||{});
  if(backend==="native") {
    const installed=await call("DatabaseNativeVersions",engine);
    if(db("Engine").value!==engine||db("Backend").value!==backend)return;
    versions=[...new Set([...versions,...installed])];
    if(!db("Version").value&&installed.length)db("Version").value=installed[installed.length-1];
  } else { const profile=databaseEngines.find(e=>e.name===engine);if(profile)versions.push(profile.default_version); }
  db("Versions").replaceChildren(...[...new Set(versions)].map(v=>{const option=document.createElement("option");option.value=v;return option;}));
}
async function selectDatabaseInstance() {
  clearDatabaseConnection();databaseError(null);
  const selected=databaseInstances.find(i=>i.name===db("Instance").value);
  if(selected) {
    db("Name").value=selected.name;db("Engine").value=selected.engine;db("Backend").value=selected.backend;db("Version").value=selected.version;db("Port").value=selected.port;
    const actual=selected.versions?.[selected.version]?.actual_version;
    db("Status").textContent=(selected.running?"运行中":"已停止")+" · "+selected.engine+" "+selected.version+(actual?"（实际 "+actual+"）":"")+" · "+selected.backend;
    db("Data").textContent="当前版本数据："+selected.data_location;
    if(selected.error)databaseError(selected.error);
  } else {
    const profile=databaseEngines.find(e=>e.name===db("Engine").value)||databaseEngines[0];
    db("Name").value="dev-"+(profile?.name||"postgres");db("Version").value=db("Backend").value==="native"?"":profile?.default_version||"";
    const ports={postgres:15432,mysql:13306,mariadb:13307,redis:16379,valkey:16380};db("Port").value=ports[profile?.name]||15432;
    db("Status").textContent="配置新实例，保存后点击启动。";db("Data").textContent="数据按实例及版本独立保存。";
  }
  databaseControls();await databaseVersionOptions();
}
async function refreshDatabases(preferred) {
  databaseInstances=await call("DatabaseList");
  const previous=preferred===undefined?db("Instance").value:preferred;
  const first=document.createElement("option");first.value="";first.textContent="创建新实例";
  db("Instance").replaceChildren(first,...databaseInstances.map(instance=>{const option=document.createElement("option");option.value=instance.name;option.textContent=instance.name+" · "+instance.engine+" "+instance.version+" · "+(instance.running?"运行中":"已停止");return option;}));
  db("Instance").value=databaseInstances.some(i=>i.name===previous)?previous:"";
  await selectDatabaseInstance();
}
async function databaseAction(action,cancelable=false) {
  if(databaseBusy)return;databaseBusy=true;databaseCancelable=cancelable;databaseError(null);databaseControls();
  try{await action();}catch(error){if(cancelable){try{await refreshDatabases();}catch(_){}}databaseError(error);}finally{databaseBusy=false;databaseCancelable=false;databaseControls();}
}
db("Btn").addEventListener("click",()=>{
  db("Modal").classList.remove("hidden");clearDatabaseConnection();db("Output").textContent="";db("Output").classList.add("hidden");
  databaseAction(async()=>{
    databaseEngines=await call("DatabaseEngines");
    db("Engine").replaceChildren(...databaseEngines.map(profile=>{const option=document.createElement("option");option.value=profile.name;option.textContent=profile.name;return option;}));
    await refreshDatabases();
  });
});
db("Instance").addEventListener("change",()=>databaseAction(selectDatabaseInstance));
for(const id of ["Engine","Backend"])db(id).addEventListener("change",()=>databaseAction(selectDatabaseInstance));
for(const id of ["Version","Port","Name"])db(id).addEventListener("input",()=>{clearDatabaseConnection();databaseControls();});
db("Save").addEventListener("click",()=>databaseAction(async()=>{
  const request={name:db("Name").value.trim(),engine:db("Engine").value,backend:db("Backend").value,version:db("Version").value.trim(),port:Number(db("Port").value)};
  if(!db("Instance").value&&databaseInstances.some(i=>i.name===request.name))throw Error("已有同名实例，请从列表选择后切换，或使用新的名称");
  if(!request.version)throw Error("请选择数据库版本；原生方式需要先纳管程序");
  databaseOutput("正在保存 / 切换版本…");const state=await call("DatabaseUse",request);await refreshDatabases(state.name);databaseOutput(state.running?"版本已切换，服务运行中。":"版本已选择，点击启动即可运行。");
},true));
for(const action of ["Start","Stop"])db(action).addEventListener("click",()=>databaseAction(async()=>{const state=await call("Database"+action,db("Instance").value);await refreshDatabases(state.name);databaseOutput(action==="Start"?"数据库已启动。":"数据库已停止，数据保留。");},true));
db("Refresh").addEventListener("click",()=>databaseAction(()=>refreshDatabases()));
db("Adopt").addEventListener("click",()=>databaseAction(async()=>{
  const path=await call("PickDirectory","选择完整数据库安装目录");if(!path)return;
  const versions=await call("DatabaseAdoptNative",db("Engine").value,path);await databaseVersionOptions();if(versions.length)db("Version").value=versions[versions.length-1];databaseOutput("原生程序已纳管，保存实例后启动。");
}));
db("Logs").addEventListener("click",()=>databaseAction(async()=>{db("Output").textContent=await call("DatabaseLogs",db("Instance").value)||"暂无日志";db("Output").classList.remove("hidden");}));
db("Connect").addEventListener("click",()=>databaseAction(async()=>{const connection=await call("DatabaseConnection",db("Instance").value);db("Connection").value=connection.uri;db("ConnectionWrap").classList.remove("hidden");}));
db("Copy").addEventListener("click",async()=>{try{await navigator.clipboard.writeText(db("Connection").value);toast("连接地址已复制");}catch(_){db("Connection").focus();db("Connection").select();toast("请按 Ctrl+C（macOS 用 ⌘C）复制");}});
db("Cancel").addEventListener("click",async()=>{try{await call("CancelDatabaseOperation");databaseOutput("正在取消；切换操作会尝试恢复旧版本，请等待结果。");}catch(error){databaseError(error);}});
db("Close").addEventListener("click",()=>{if(!databaseBusy){db("Modal").classList.add("hidden");clearDatabaseConnection();}});
if(window.runtime?.EventsOn)window.runtime.EventsOn("database:progress",text=>{if(databaseBusy)databaseOutput(text);});
