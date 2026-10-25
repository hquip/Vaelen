let workbenchBusy = false;
let workbenchRunning = false;
let workbenchState = null;
const wb = id => document.getElementById("workbench" + id);
function workbenchError(error) { wb("Error").textContent = error ? errMsg(error) : ""; wb("Error").classList.toggle("hidden", !error); }
function workbenchOutput(value) { const out=wb("Output"); out.textContent=(out.textContent+value).slice(-120000);out.scrollTop=out.scrollHeight;wb("OutputWrap").open=true; }
function workbenchControls() {
  for(const id of ["Pick","Refresh","Configure","Edit","Save","Lock","Bump","Install","Add","Task","Jobs","Locked","NoCache","Platforms"])wb(id).disabled=workbenchBusy;
  const configured=workbenchState&&workbenchState.configured;
  wb("Edit").disabled=workbenchBusy||!configured;
  wb("Trust").disabled=workbenchBusy||!configured||!wb("Reviewed").checked;
  wb("Run").disabled=workbenchBusy||!configured||!workbenchState.trusted||!!workbenchState.config_error||!wb("Task").value;
  wb("Install").disabled=workbenchBusy||!workbenchState||!workbenchState.lock||!!workbenchState.lock_error;
  wb("Cancel").disabled=!workbenchRunning;
}
function renderWorkbench(state) {
  workbenchState=state;
 if(state.lock&&!state.lock_error)wb("Locked").checked=true;
 if(state.config_error)workbenchError(state.config_error);
  wb("Root").textContent=state.root||"尚未选择项目";
  wb("Status").textContent=state.configured?(state.trusted?"配置已信任":"配置尚未信任或已变更")+" · "+state.tasks.length+" 个任务 · 环境变量名称: "+(state.environment_keys.join(", ")||"无"):"选择项目后生成 vsm.json，统一管理工具、变量和任务。";
  wb("Configure").textContent=state.configured?"保留当前项目配置":"生成 vsm.json";
  wb("LockStatus").textContent=state.lock_error|| (state.lock?"锁文件包含 "+Object.keys(state.lock.tools).length+" 个工具":"尚未生成 vsm.lock");
  wb("LockSummary").textContent=state.lock?Object.entries(state.lock.tools).map(([name,tool])=>name+" "+tool.requested+" → "+tool.version+"\n  "+Object.keys(tool.platforms).join(", ")).join("\n"):"生成后请提交 vsm.json 与 vsm.lock，团队和 CI 使用同一构建。";
  const previous=wb("Task").value;
  wb("Task").replaceChildren(...state.tasks.map(task=>{const option=document.createElement("option");option.value=task.name;option.textContent=task.name+(task.definition.description?" · "+task.definition.description:"");return option;}));
  if(state.tasks.some(task=>task.name===previous))wb("Task").value=previous;
  wb("TaskGraph").textContent=state.tasks.map(task=>task.name+(task.definition.depends?" ← "+task.definition.depends.join(", "):"")).join("\n")||"在项目配置 tasks 中添加命令参数数组，例如 test: {command: [\"go\", \"test\", \"./...\"]}";
  workbenchControls();
}
async function refreshWorkbench(){renderWorkbench(await call("Workbench"));}
async function workbenchAction(action){
  if(workbenchBusy)return;workbenchBusy=true;workbenchError(null);workbenchControls();
  try{await action();}catch(error){workbenchError(error);}finally{workbenchBusy=false;workbenchRunning=false;workbenchControls();}
}
async function searchWorkbenchCatalog(){
  const query=wb("Search").value;
  const tools=await call("ToolCatalog",query);
  if(wb("Search").value!==query)return;
  wb("Catalog").replaceChildren(...tools.map(tool=>{const row=document.createElement("div");row.className="workbench-tool";for(const [text,cls] of [[tool.support?.display_name||tool.name,""],[tool.installable?"可安装":"纳管已有安装",""],[(tool.support?.languages||[]).join(" / ")||tool.source,"source"]]){const label=document.createElement("span");label.textContent=text;label.className=cls;row.appendChild(label);}return row;}));
}
document.getElementById("workbenchBtn").addEventListener("click",()=>{wb("Modal").classList.remove("hidden");workbenchAction(async()=>{await refreshWorkbench();await searchWorkbenchCatalog();});});
wb("Pick").addEventListener("click",()=>workbenchAction(async()=>{const dir=await call("PickProjectDir");if(dir){wb("Reviewed").checked=false;wb("EditorWrap").classList.add("hidden");await refreshWorkbench();await load();await loadCurrentDir();}}));
wb("Refresh").addEventListener("click",()=>workbenchAction(refreshWorkbench));
wb("Configure").addEventListener("click",()=>workbenchAction(async()=>{if(!workbenchState||!workbenchState.root)throw Error("请先选择项目");renderWorkbench(await call("ConfigureWorkspace"));}));
wb("Edit").addEventListener("click",()=>workbenchAction(async()=>{wb("Editor").value=await call("WorkspaceConfiguration");wb("EditorWrap").classList.remove("hidden");}));
wb("Save").addEventListener("click",()=>workbenchAction(async()=>{renderWorkbench(await call("SaveWorkspaceConfiguration",wb("Editor").value));wb("Reviewed").checked=false;workbenchControls();}));
wb("Reviewed").addEventListener("change",workbenchControls);
wb("Trust").addEventListener("click",()=>workbenchAction(async()=>{if(!wb("Reviewed").checked)throw Error("请先检查配置");renderWorkbench(await call("TrustWorkspace"));}));
function workbenchPlatforms(){return Array.from(wb("Platforms").selectedOptions).map(option=>option.value);}
wb("Lock").addEventListener("click",()=>workbenchAction(async()=>{renderWorkbench(await call("LockWorkspace",workbenchPlatforms(),false));}));
wb("Bump").addEventListener("click",()=>workbenchAction(async()=>{renderWorkbench(await call("LockWorkspace",workbenchPlatforms(),true));}));
wb("Install").addEventListener("click",()=>workbenchAction(async()=>{renderWorkbench(await call("InstallLockedWorkspace"));await load();}));
wb("Run").addEventListener("click",()=>workbenchAction(async()=>{
  const jobs=Number(wb("Jobs").value);if(!Number.isInteger(jobs)||jobs<1||jobs>16)throw Error("并发数为 1–16");
  workbenchRunning=true;workbenchControls();wb("Output").textContent="";
  const report=await call("RunWorkspaceTask",wb("Task").value,wb("Locked").checked,wb("NoCache").checked,jobs);
  workbenchOutput("\n"+report.tasks.map(task=>task.name+": "+task.state+" ("+task.duration_ms+" ms)").join("\n")+"\n");await refreshWorkbench();
}));
wb("Cancel").addEventListener("click",async()=>{try{await call("CancelWorkspaceTask");}catch(error){workbenchError(error);}});
wb("Search").addEventListener("input",()=>searchWorkbenchCatalog().catch(workbenchError));
wb("Add").addEventListener("click",()=>workbenchAction(async()=>{await call("AddGitHubTool",wb("ToolName").value.trim(),wb("Repo").value.trim(),wb("Command").value.trim(),wb("Bin").value.trim(),wb("Match").value.trim());await searchWorkbenchCatalog();await load();workbenchOutput("已添加工具来源，可在左侧选择并安装。\n");}));
for(const tab of document.querySelectorAll("[data-wb-tab]"))tab.addEventListener("click",()=>{wb("TaskActions").classList.toggle("hidden",tab.dataset.wbTab!=="tasks");for(const pane of document.querySelectorAll(".workbench-pane"))pane.classList.toggle("hidden",pane.id!=="workbench-"+tab.dataset.wbTab);for(const button of document.querySelectorAll("[data-wb-tab]"))button.classList.toggle("active",button===tab);});
wb("Close").addEventListener("click",()=>wb("Modal").classList.add("hidden"));
window.addEventListener("DOMContentLoaded",()=>{
  if(window.runtime&&typeof window.runtime.EventsOn==="function"){
    window.runtime.EventsOn("workspace:progress",workbenchOutput);
    window.runtime.EventsOn("workspace:task",event=>workbenchOutput(event.output?"["+event.task+"] "+event.output:"["+event.task+"] "+event.state+"\n"));
  }
});
