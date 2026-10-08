// ---- 文件选择（含拖拽） ----
function setFileName(nameId, f){
  const el=document.getElementById(nameId);
  if(f){
    el.innerHTML=esc(t("file_chosen")+f.name)
      +'<button type="button" class="fn-clear" data-clear="1" title="'+esc(t("clear_file"))+'" aria-label="'+esc(t("clear_file"))+'">&times;</button>';
    el.dataset.chosen="1";
  } else { el.textContent=t("no_file"); el.dataset.chosen="0"; }
}
function bindFile(inputId,nameId,cardId,onPick,onClear){
  const input=document.getElementById(inputId);
  input.addEventListener("change",e=>{
    const f=e.target.files[0];
    setFileName(nameId,f);
    if(f && onPick) onPick(f);
  });
  // 清除选择：点文件名后面的 ×（事件委托，因为文件名区每次都会重建）
  document.getElementById(nameId).addEventListener("click",e=>{
    if(!e.target.closest("[data-clear]")) return;
    e.preventDefault();
    input.value="";                 // 同步清空 input，之后重选同一文件也能触发 change
    setFileName(nameId,null);
    if(onClear) onClear();
  });
  // 拖拽
  const card=document.getElementById(cardId);
  ["dragenter","dragover"].forEach(ev=>card.addEventListener(ev,e=>{ e.preventDefault(); card.classList.add("drag"); }));
  ["dragleave","drop"].forEach(ev=>card.addEventListener(ev,e=>{ e.preventDefault(); card.classList.remove("drag"); }));
  card.addEventListener("drop",e=>{
    const f=e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
    if(!f) return;
    try{ const dt=new DataTransfer(); dt.items.add(f); input.files=dt.files; }catch(err){}
    setFileName(nameId,f);
    if(onPick) onPick(f);
  });
}
function renderDatalist(id, list, isTpl){
  const dl=document.getElementById(id);
  dl.innerHTML = list.map(h=>{
    const val = isTpl ? cleanHdr(h.code) : h.code;
    if(!val) return "";
    return '<option value="'+esc(val)+'">'+esc(h.label||h.code)+'</option>';
  }).join("");
}
async function loadSourceHeaders(f){
  const fd=new FormData(); fd.append("source",f);
  try{
    const r=await fetch("/api/source-headers",{method:"POST",body:fd});
    const d=await r.json();
    if(d.ok){
      srcHeaders=d.headers||[];
      renderDatalist("srcCols",srcHeaders,false);
    }
  }catch(e){ /* 表头读取失败不影响转换 */ }
}
async function loadTemplateHeaders(f){
  const fd=new FormData(); fd.append("source",f);
  try{
    const r=await fetch("/api/source-headers",{method:"POST",body:fd});
    const d=await r.json();
    if(d.ok) renderDatalist("tgtCols", d.headers||[], true);
  }catch(e){}
}
// 源文件改成多选（可一次选多个合并），绑定在「批量多文件」那一段；这里只留模板一份。
bindFile("tplFile","tplName","tplCard",loadTemplateHeaders,
  ()=>{ renderDatalist("tgtCols",[],true); });

// ---- 状态条 ----
// ---- 通用工厂：状态条 / 忙碌按钮 / 进度条（物料与订单模块共用） ----
function makeStatus(id){ return (type,key)=>{
  const b=document.getElementById(id);
  if(!key){ b.className="banner"; b.textContent=""; return; }
  b.className="banner show "+(type==="ok"?"ok":"err");
  b.textContent=(typeof key==="string" && STR[currentLang] && STR[currentLang][key]) ? t(key) : key;
}; }
function makeBusy(btnId,busyKey,idleKey){ return busy=>{
  const btn=document.getElementById(btnId);
  btn.disabled=busy; btn.setAttribute("aria-busy",busy?"true":"false");
  btn.innerHTML = busy ? '<span class="spinner"></span>'+t(busyKey) : t(idleKey);
  window.__mesBusy=busy;
}; }
function fmtSec(ms){
  if(!ms) return "0s";
  const s=ms/1000;
  return s<60 ? s.toFixed(1)+"s" : Math.floor(s/60)+"m"+Math.round(s%60)+"s";
}
function makeProgress(ids,label){
  let timer=null, inflight=false;
  const el=k=>document.getElementById(ids[k]);
  const show=(pct,txt)=>{ el("wrap").style.display="block"; el("bar").style.width=Math.max(0,Math.min(100,pct||0))+"%"; el("text").textContent=txt||""; };
  const hide=()=>{ el("wrap").style.display="none"; };
  const poll=async()=>{           // 上一次请求没回来就不再发，避免请求堆积
    if(inflight) return; inflight=true;
    try{
      const r=await fetch("/api/progress"); if(!r.ok) return;
      const d=await r.json();
      if(d && d.active){
        const pct=d.total>0?d.percent:0;
        show(pct, d.total>0 ? label(d,pct)+" · "+fmt("prog_elapsed",[fmtSec(d.elapsed_ms)]) : t("prog_preparing"));
      }
    }catch(e){} finally{ inflight=false; }
  };
  return { show, hide,
    start(){ show(0,t("prog_preparing")); clearInterval(timer); timer=setInterval(poll,700); },
    finish(doneText){ clearInterval(timer); timer=null; if(doneText){ show(100,doneText); setTimeout(hide,900); } else hide(); } };
}
async function safeJson(r){   // 后端崩溃返回 HTML 时给出明确的 HTTP 状态
  let d=null; try{ d=await r.json(); }catch(e){}
  if(d===null) throw new Error("HTTP "+r.status+(r.statusText?" "+r.statusText:""));
  return d;
}
const CONVERT_TIMEOUT_MS=600000;   // 单次转换最长等 10 分钟
// 老内核没有 AbortSignal.timeout，这里退化为「不设超时」而不是抛错——功能不受影响，只是不自动掐断
function timeoutSignal(ms){
  try{ if(typeof AbortSignal!=="undefined" && typeof AbortSignal.timeout==="function") return AbortSignal.timeout(ms); }catch(e){}
  return undefined;
}
// 把 fetch 抛出的异常翻成用户看得懂的话：超时 / 手动中断 / 连不上服务 / 后端返回了非 JSON
function fetchErrText(e){
  const n=(e&&e.name)||"", msg=(e&&e.message)||"";
  if(n==="TimeoutError") return t("err_timeout");
  if(n==="AbortError") return t("err_aborted");
  if(/Failed to fetch|NetworkError|ERR_CONNECTION/i.test(msg)) return t("err_offline");
  return msg||t("status_error");
}
const showStatus=makeStatus("statusBanner"), setBusy=makeBusy("startBtn","converting","start_btn");
const ordShowStatus=makeStatus("ordBanner"), ordSetBusy=makeBusy("ordStartBtn","ord_converting","ord_start_btn");
const _prog=makeProgress({wrap:"progressWrap",bar:"progressBar",text:"progressText"},(d,p)=>fmt("prog_fmt",[d.done,d.total,p]));
const _oprog=makeProgress({wrap:"ordProgressWrap",bar:"ordProgressBar",text:"ordProgressText"},d=>fmt("ord_progress",[d.done,d.total]));
const showProgress=_prog.show, hideProgress=_prog.hide, startProgressPoll=_prog.start, finishProgress=_prog.finish;
const ordShowProgress=_oprog.show, ordHideProgress=_oprog.hide, ordStartProgressPoll=_oprog.start, ordFinishProgress=_oprog.finish;
function formatDist(o){ if(!o) return "-"; return Object.keys(o).map(k=>k+": "+o[k]).join(", "); }
// ---- 日志渲染（物料档案 / 订单模块结构一致，只有元素 id 不同）----
function makeLogRenderer(ids){
  return function(lines){
    const box=document.getElementById(ids.box);
    box.textContent=(lines&&lines.length)?lines.join("\n"):"(empty)";
    box.classList.add("show");
    document.getElementById(ids.section).classList.add("show");
    document.getElementById(ids.toggle).textContent=t("hide_log");
  };
}
const renderLog=makeLogRenderer({box:"logBox",section:"logSection",toggle:"toggleLogBtn"});
const ordRenderLog=makeLogRenderer({box:"ordLogBox",section:"ordLogSection",toggle:"ordToggleLogBtn"});
function statCard(title,val){
  return '<div class="stat"><div class="t">'+title+'</div><div class="v">'+val+'</div></div>';
}
// 上次转换结果缓存：切换语言时用它重渲染统计卡，无需重新转换
let lastResult=null;
function renderStats(data){
  document.getElementById("stats").innerHTML =
    statCard(t("rows_label"),data.rows)
    + statCard(t("elapsed_label"),(data.elapsed||0)+" ms")
    + statCard(t("type_label"),formatDist(data.types))
    + statCard(t("source_label"),formatDist(data.sources));
}

// ---- 预检结果渲染 ----
// data 既可以是本次转换的响应，也可以是 /api/report 返回的历史报告（字段同名）；
// fromHistory=true 表示这是翻出来的历史记录，改显示「历史转换」提示而不是语言提示。
// 物料档案与订单模块的这一块结构完全一致，只有元素 id、汇总文案 key 和「上次结果」变量不同。
function makeChecksRenderer(c){
  return function(data, fromHistory){
    const sec=document.getElementById(c.sec);
    const ul=document.getElementById(c.list);
    const wrap=document.getElementById(c.wrap);
    const body=document.getElementById(c.body);
    const sum=document.getElementById(c.sum);
    sec.classList.add("show");
    ul.innerHTML=""; body.innerHTML="";
    const errs=data.errors||[], warns=data.warnings||[], issues=data.issues||[];
    const count=(errs.length+warns.length) || (data.issue_total||0);
    sum.textContent = count>0 ? fmt(c.summaryFmt,[count]) : t(c.okKey);
    if(count===0 && issues.length===0){
      ul.innerHTML='<li class="check-ok">'+t(c.okKey)+'</li>';
      wrap.style.display="none";
      return;
    }
    errs.forEach(x=>ul.insertAdjacentHTML("beforeend",'<li class="err">✕ '+esc(x)+'</li>'));
    warns.forEach(x=>ul.insertAdjacentHTML("beforeend",'<li class="warn">! '+esc(x)+'</li>'));
    // 历史报告：提示这是哪一次转换留下的（内容按当时的语言生成）
    // 本次转换：提示内容由后端按「转换时的语言」生成，若界面语言已切换则加一行说明
    if(fromHistory){
      ul.insertAdjacentHTML("beforeend",
        '<li style="color:#6b7280;font-size:12px;margin-top:6px;list-style:none">'+
        esc(fmt("hist_report_hint",[data.time||""]))+'</li>');
    } else {
      const last=c.lastResult();
      if(last && last.lang!==currentLang){
        ul.insertAdjacentHTML("beforeend",
          '<li style="color:#6b7280;font-size:12px;margin-top:6px;list-style:none">'+esc(t("check_lang_hint"))+'</li>');
      }
    }
    if(issues.length){
      wrap.style.display="block";
      body.innerHTML = issues.map(it=>'<tr><td>'+(it.row||"")+'</td><td>'+esc(it.col||"")+'</td><td>'+
        (it.level==="error"?'<span style="color:#dc2626">':'<span style="color:#b45309">')+esc(it.msg||"")+'</span></td></tr>').join("");
    } else { wrap.style.display="none"; }
  };
}
const renderChecks=makeChecksRenderer({
  sec:"checkSection", list:"checkList", wrap:"issueWrap", body:"issueBody", sum:"check_summary",
  summaryFmt:"check_summary", okKey:"check_ok", lastResult:()=>lastResult });

// ---- 转换 ----
async function doConvert(){
  const sfs=srcPicker.get();
  const tf=document.getElementById("tplFile").files[0];
  if(!sfs.length){ showStatus("err","err_no_src"); return; }
  if(!tf){ showStatus("err","err_no_tpl"); return; }
  setBusy(true); showStatus("","");
  startProgressPoll();
  let doneText="";
  const fd=new FormData();
  fd.append("template",tf);
  sfs.forEach(f=>fd.append("source",f));
  fd.append("config", JSON.stringify(cfg||{})); fd.append("lang",currentLang);
  try{
    const resp=await fetch("/api/convert",{method:"POST",body:fd,signal:timeoutSignal(CONVERT_TIMEOUT_MS)});
    const data=await safeJson(resp);
    renderLog(data.log||[]);
    if(data.ok){
      showStatus("ok","status_done");
      const rs=document.getElementById("resultSection"); rs.classList.add("show");
      document.getElementById("downloadLink").href="/download?file="+encodeURIComponent(data.file);
      renderStats(data);
      lastResult={data:data,lang:currentLang};   // 缓存结果，便于切换语言时重渲染
      renderChecks(data);
      loadHistory();
      doneText=fmt("prog_done",[data.rows||0]);
      rs.scrollIntoView({behavior:"smooth",block:"nearest"});
    } else {
      showStatus("err", data.error || t("status_error"));
      if(data.log) renderLog(data.log);
    }
  }catch(e){
    showStatus("err", fetchErrText(e));
  }finally{ setBusy(false); finishProgress(doneText); }
}
document.getElementById("startBtn").addEventListener("click",doConvert);

// ---- 打开输出文件夹 ----
document.getElementById("openFolderBtn").addEventListener("click",async()=>{
  const b=document.getElementById("openFolderBtn"); const old=b.textContent;
  b.textContent=t("opening");
  try{ await fetch("/api/open-folder"); }catch(e){}
  setTimeout(()=>{ b.textContent=old; },900);
});

// ---- 日志折叠 / 复制 ----
document.getElementById("toggleLogBtn").addEventListener("click",()=>{
  const box=document.getElementById("logBox"); box.classList.toggle("show");
  document.getElementById("toggleLogBtn").textContent = box.classList.contains("show")?t("hide_log"):t("view_log");
});
document.getElementById("copyLogBtn").addEventListener("click",()=>{
  const txt=document.getElementById("logBox").textContent||"";
  if(navigator.clipboard) navigator.clipboard.writeText(txt);
});

// ---- 转换历史 ----
// 物料档案与订单模块的历史表结构完全一致，只有接口、元素 id、文案 key 与「查看」回调不同
function makeHistoryLoader(c){
  return async function(){
    try{
      const r=await fetch(c.url); const d=await r.json();
      const body=document.getElementById(c.body);
      const sec=document.getElementById(c.sec);
      const items=(d.items||[]).slice(0,10);
      sec.classList.add("show");
      if(!items.length){
        body.innerHTML='<tr><td colspan="5" style="text-align:center;color:#6b7280">'+t(c.k.none)+'</td></tr>';
        return;
      }
      body.innerHTML = items.map(it=>{
        const bad=(it.issues||0)>0;
        const badge = bad
          ? '<span style="color:#b45309">'+t(c.k.bad)+' ('+it.issues+')</span>'
          : '<span style="color:#059669">'+t(c.k.ok)+'</span>';
        // 「查看」按钮：把那次转换留下的问题明细重新摊开（服务重启过也照样能看）
        const viewBtn = it.report
          ? '<button class="ghost '+c.btnCls+'" data-report="'+esc(it.report)+'" style="padding:3px 9px;font-size:12px;margin-right:6px'+(bad?';color:#b45309':'')+'">'
            +esc(bad?t("hist_view_bad"):t("hist_view"))+'</button>'
          : '';
        return '<tr><td>'+esc(it.time||"")+'</td><td>'+esc(it.file||"")+'</td><td>'+(it.rows||0)+'</td><td>'+badge+'</td>'+
          '<td>'+viewBtn+
          '<a class="ghost" style="padding:3px 9px;font-size:12px" href="/download?file='+encodeURIComponent(it.file||"")+'">'+t(c.k.dl)+'</a></td></tr>';
      }).join("");
      body.querySelectorAll("button."+c.btnCls).forEach(b=>{
        b.addEventListener("click",()=>c.view(b.getAttribute("data-report")));
      });
      // 打开页面时，若最近一次转换就有问题，直接把它摊开
      // —— 否则用户重开服务后根本想不起来上次哪儿有问题
      const latest=items[0];
      if(latest && latest.report && ((latest.issues||0)>0 || (latest.warnings||0)>0)){
        c.view(latest.report,true);
      }
    }catch(e){}
  };
}
const loadHistory=makeHistoryLoader({
  url:"/api/history", body:"histBody", sec:"historySection", btnCls:"jsview", view:viewReport,
  k:{none:"hist_none", bad:"hist_bad", ok:"hist_ok", dl:"hist_dl"} });
document.getElementById("refreshHistBtn").addEventListener("click",loadHistory);

// ---- 查看某一次历史转换的完整问题明细 ----
async function viewReport(id,noScroll){
  if(!id) return;
  try{
    const r=await fetch("/api/report?id="+encodeURIComponent(id));
    const d=await r.json();
    if(!d.ok || !d.report) return;
    renderChecks(d.report,true);
    if(!noScroll){
      document.getElementById("checkSection").scrollIntoView({behavior:"smooth",block:"center"});
    }
  }catch(e){}
}

// ============ 高级设置 ============
function dictOptions(sel){
  const names=Object.keys(advDicts||{});
  if(sel && names.indexOf(sel)<0) names.unshift(sel);
  return '<option value="">'+t('choose_dict')+'</option>'+names.map(n=>`<option value="${esc(n)}" ${n===sel?'selected':''}>${esc(n)}</option>`).join('');
}
function paramControlsHTML(f){
  switch(f.op){
    case "copy":
      return `<div class="pc-line">
        <label><input type="checkbox" data-p="trim" ${f.trim?'checked':''}> ${t('p_trim')}</label>
        <label><input type="checkbox" data-p="raw" ${f.raw?'checked':''}> ${t('p_raw')}</label>
        <span>${t('p_default')} <input type="text" data-p="default" value="${esc(f.default||'')}" style="width:120px"></span>
      </div>`;
    case "dict":
      return `<div class="pc-line"><span>${t('dict_name')}:</span><select data-p="dict">${dictOptions(f.dict)}</select></div>`;
    case "fixed":
      return `<div class="pc-line"><span>${t('p_value')}</span> <input type="text" data-p="value" value="${esc(f.value||'')}" style="width:160px"></div>`;
    case "cond":
      return `<div class="pc-line">
        <span>${t('p_if')} <input type="text" data-p="if" value="${esc(f.if||'')}" style="width:70px"></span>
        <span>${t('p_then')} <input type="text" data-p="then" value="${esc(f.then||'')}" style="width:70px"></span>
        <span>${t('p_else')} <input type="text" data-p="else" value="${esc(f.else||'')}" style="width:70px"></span>
      </div>`;
    case "kwbool":
      return `<div class="pc-line">
        <span>${t('p_keywords')} <input type="text" data-p="keywords" value="${esc((f.keywords||[]).join(','))}" style="width:200px"></span>
      </div>
      <div class="pc-line">
        <span>${t('p_then')} <input type="text" data-p="then" value="${esc(f.then||'')}" style="width:70px"></span>
        <span>${t('p_else')} <input type="text" data-p="else" value="${esc(f.else||'')}" style="width:70px"></span>
      </div>`;
    case "kwmap":
      return renderKwmap(f);
    case "case":
      return renderCase(f);
    default:
      return `<span class="muted">（${t('th_param')}）</span>`;
  }
}
function renderKwmap(f){
  f.kwmap=f.kwmap||{};
  let rows=Object.keys(f.kwmap).map(k=>`<tr><td><input class="km-k" value="${esc(k)}"></td><td><input class="km-v" value="${esc(f.kwmap[k])}"></td><td><button class="row-del km-del">✕</button></td></tr>`).join('');
  return `<div class="nest">
    <div class="nest-title">${t('kwmap_title')}</div>
    <table class="kv"><thead><tr><th>${t('kw_col_kw')}</th><th>${t('kw_col_val')}</th><th></th></tr></thead><tbody class="km-body">${rows}</tbody></table>
    <button class="mini-btn km-add">+ ${t('add_kw')}</button>
    <div class="pc-line" style="margin-top:6px"><span>${t('kwmap_else')}</span> <input class="km-else" type="text" value="${esc(f.else||'')}" style="width:90px"></div>
  </div>`;
}
function renderCase(f){
  f.cases=f.cases||[];
  let rows=f.cases.map(c=>{
    const mopts=MATCHES.map(m=>`<option value="${m[0]}" ${c.match===m[0]?'selected':''}>${matchLabel(m[0])}</option>`).join('');
    return `<tr><td><input class="cs-src" list="srcCols" value="${esc(c.source||'')}" placeholder="MBxxx"></td>
      <td><select class="cs-match">${mopts}</select></td>
      <td><input class="cs-val" value="${esc(c.value||'')}"></td>
      <td><input class="cs-then" value="${esc(c.then||'')}" placeholder="${t('p_then')}"></td>
      <td><button class="row-del cs-del">✕</button></td></tr>`;
  }).join('');
  return `<div class="nest">
    <div class="pc-line"><span>${t('case_default')}</span> <input class="cs-default" type="text" value="${esc(f.default_out||'')}" style="width:90px"></div>
    <div class="nest-title">${t('case_title')}</div>
    <table class="kv"><thead><tr><th>${t('p_source')}</th><th>${t('p_match')}</th><th>${t('case_col_val')}</th><th>${t('p_then')}</th><th></th></tr></thead><tbody class="cs-body">${rows}</tbody></table>
    <button class="mini-btn cs-add">${t('add_cond')}</button>
  </div>`;
}
function bindParam(td,f){
  td.querySelectorAll('[data-p]').forEach(el=>{
    const p=el.dataset.p;
    const evt=(el.tagName==='SELECT'||el.type==='checkbox')?'change':'input';
    el.addEventListener(evt,()=>{
      if(el.type==='checkbox'){ f[p]=el.checked; }
      else if(p==='keywords'){ f.keywords=el.value.split(/[,，]/).map(s=>s.trim()).filter(Boolean); }
      else { f[p]=el.value; }
    });
  });
  if(f.op==='kwmap') bindKwmap(td,f);
  if(f.op==='case') bindCase(td,f);
}
function bindKwmap(td,f){
  const sync=()=>{ const o={}; td.querySelectorAll('.km-body tr').forEach(tr=>{ const k=tr.querySelector('.km-k').value.trim(); const v=tr.querySelector('.km-v').value; if(k) o[k]=v; }); f.kwmap=o; f.keywords=Object.keys(o); f.else=td.querySelector('.km-else').value; };
  td.querySelector('.km-body').addEventListener('input',sync);
  td.querySelector('.km-else').addEventListener('input',sync);
  td.querySelector('.km-add').addEventListener('click',()=>{
    const tb=td.querySelector('.km-body'); const tr=document.createElement('tr');
    tr.innerHTML=`<td><input class="km-k"></td><td><input class="km-v"></td><td><button class="row-del km-del">✕</button></td>`;
    tr.querySelector('.km-del').addEventListener('click',()=>{tr.remove();sync();});
    tr.querySelectorAll('input').forEach(i=>i.addEventListener('input',sync));
    tb.appendChild(tr);
  });
  td.querySelectorAll('.km-del').forEach(b=>b.addEventListener('click',()=>{b.closest('tr').remove();sync();}));
}
function bindCase(td,f){
  const sync=()=>{ const arr=[]; td.querySelectorAll('.cs-body tr').forEach(tr=>{ arr.push({source:tr.querySelector('.cs-src').value.trim(),match:tr.querySelector('.cs-match').value,value:tr.querySelector('.cs-val').value,then:tr.querySelector('.cs-then').value}); }); f.cases=arr; f.default_out=td.querySelector('.cs-default').value; };
  td.querySelector('.cs-body').addEventListener('input',sync);
  td.querySelector('.cs-default').addEventListener('input',sync);
  td.querySelector('.cs-add').addEventListener('click',()=>{
    const tb=td.querySelector('.cs-body'); const tr=document.createElement('tr');
    const mopts=MATCHES.map(m=>`<option value="${m[0]}">${matchLabel(m[0])}</option>`).join('');
    tr.innerHTML=`<td><input class="cs-src" list="srcCols" placeholder="MBxxx"></td><td><select class="cs-match">${mopts}</select></td><td><input class="cs-val"></td><td><input class="cs-then"></td><td><button class="row-del cs-del">✕</button></td>`;
    tr.querySelector('.cs-del').addEventListener('click',()=>{tr.remove();sync();});
    tr.querySelectorAll('input,select').forEach(i=>i.addEventListener('input',sync));
    tb.appendChild(tr);
  });
  td.querySelectorAll('.cs-del').forEach(b=>b.addEventListener('click',()=>{b.closest('tr').remove();sync();}));
}

function renderTable(){
  const tb=document.querySelector("#fieldTable tbody"); tb.innerHTML="";
  advFields.forEach((f,i)=>addFieldRow(f,i));
}
function addFieldRow(f,idx){
  const tr=document.createElement("tr");
  const ops=OPS.map(o=>`<option value="${o}" ${o===f.op?'selected':''}>${esc(opLabel(o))}</option>`).join("");
  tr.innerHTML=`<td><input data-k="target" list="tgtCols" value="${esc(f.target||'')}"></td>
    <td><input data-k="source" list="srcCols" value="${esc(f.source||'')}" placeholder="MBxxx"></td>
    <td><select data-k="op">${ops}</select></td>
    <td class="param-cell"></td>
    <td class="desc-cell">${esc(TARGET_DESC[f.target]||"")}</td>
    <td><button class="row-del" title="${t('del_row')}">✕</button></td>`;
  const tgt=tr.querySelector('[data-k="target"]');
  tgt.addEventListener('input',e=>{
    f.target=e.target.value;
    tr.querySelector('.desc-cell').textContent=TARGET_DESC[f.target]||"";
  });
  tr.querySelector('[data-k="source"]').addEventListener('input',e=>f.source=e.target.value);
  tr.querySelector('[data-k="op"]').addEventListener('change',e=>{ f.op=e.target.value; renderParamCell(tr.querySelector('.param-cell'),f); });
  tr.querySelector('.row-del').addEventListener('click',()=>{ advFields.splice(idx,1); renderTable(); });
  renderParamCell(tr.querySelector('.param-cell'),f);
  document.querySelector("#fieldTable tbody").appendChild(tr);
}
function renderParamCell(td,f){ td.innerHTML=paramControlsHTML(f); bindParam(td,f); }

// ---- 单位表 ----
function renderUnitMap(){
  const tb=document.getElementById("unitBody"); tb.innerHTML="";
  Object.keys(advUnitMap).forEach(k=>{
    const tr=document.createElement("tr");
    tr.innerHTML=`<td><input class="u-from" value="${esc(k)}"></td><td><input class="u-to" value="${esc(advUnitMap[k])}"></td><td><button class="row-del u-del">✕</button></td>`;
    tr.querySelectorAll('input').forEach(i=>i.addEventListener('input',syncUnitMap));
    tr.querySelector('.u-del').addEventListener('click',()=>{tr.remove();syncUnitMap();});
    tb.appendChild(tr);
  });
}
function syncUnitMap(){
  const o={};
  document.querySelectorAll('#unitBody tr').forEach(tr=>{ const f=tr.querySelector('.u-from').value.trim(); const v=tr.querySelector('.u-to').value; if(f) o[f]=v; });
  advUnitMap=o;
}
document.getElementById("addUnitBtn").addEventListener("click",()=>{
  const tb=document.getElementById("unitBody"); const tr=document.createElement("tr");
  tr.innerHTML=`<td><input class="u-from"></td><td><input class="u-to"></td><td><button class="row-del u-del">✕</button></td>`;
  tr.querySelectorAll('input').forEach(i=>i.addEventListener('input',syncUnitMap));
  tr.querySelector('.u-del').addEventListener('click',()=>{tr.remove();syncUnitMap();});
  tb.appendChild(tr);
});

// ---- 字典表 ----
function renderDicts(){
  const wrap=document.getElementById("dictsWrap"); wrap.innerHTML="";
  Object.keys(advDicts).forEach(name=>{
    const card=document.createElement("div"); card.className="dict-card";
    const entries=advDicts[name]||{};
    let rows=Object.keys(entries).map(k=>`<tr><td><input class="d-k" value="${esc(k)}"></td><td><input class="d-v" value="${esc(entries[k])}"></td><td><button class="row-del d-del">✕</button></td></tr>`).join('');
    card.innerHTML=`<div class="dict-head"><span>${t('dict_name')}：</span><input class="d-name" value="${esc(name)}"><button class="row-del d-card-del">${t('del_dict')}</button></div>
      <table class="kv"><thead><tr><th>${t('unit_from')}</th><th>${t('unit_to')}</th><th></th></tr></thead><tbody class="d-body">${rows}</tbody></table>
      <button class="mini-btn d-add">${t('add_entry')}</button>`;
    bindDictCard(card,name);
    wrap.appendChild(card);
  });
}
function bindDictCard(card,oldName){
  const nameInput=card.querySelector('.d-name');
  const syncEntries=()=>{ const e={}; card.querySelectorAll('.d-body tr').forEach(tr=>{ const k=tr.querySelector('.d-k').value.trim(); const v=tr.querySelector('.d-v').value; if(k) e[k]=v; }); const nm=nameInput.value.trim(); if(nm){ advDicts[nm]=e; } };
  nameInput.addEventListener('input',()=>{
    const newName=nameInput.value.trim();
    if(newName && newName!==oldName){ const e=advDicts[oldName]||{}; delete advDicts[oldName]; advDicts[newName]=e; oldName=newName; }
  });
  card.querySelector('.d-body').addEventListener('input',e=>{ if(e.target.classList.contains('d-k')||e.target.classList.contains('d-v')) syncEntries(); });
  card.querySelector('.d-add').addEventListener('click',()=>{
    const tb=card.querySelector('.d-body'); const tr=document.createElement("tr");
    tr.innerHTML=`<td><input class="d-k"></td><td><input class="d-v"></td><td><button class="row-del d-del">✕</button></td>`;
    tr.querySelector('.d-del').addEventListener('click',()=>{tr.remove();syncEntries();});
    tr.querySelectorAll('input').forEach(i=>i.addEventListener('input',syncEntries));
    tb.appendChild(tr);
  });
  card.querySelectorAll('.d-del').forEach(b=>b.addEventListener('click',()=>{b.closest('tr').remove();syncEntries();}));
  card.querySelector('.d-card-del').addEventListener('click',()=>{ delete advDicts[nameInput.value.trim()]; renderDicts(); });
}
document.getElementById("addDictBtn").addEventListener("click",()=>{
  let base="新字典",n=1,name=base;
  while(advDicts[name]){ n++; name=base+n; }
  advDicts[name]={}; renderDicts();
});

// ---- 允许值清单 ----
function renderWl(){
  const wrap=document.getElementById("wlWrap"); wrap.innerHTML="";
  Object.keys(advWl).forEach(col=>{
    const tr=document.createElement("div"); tr.className="pc-line";
    tr.innerHTML=`<input class="wl-col" list="tgtCols" value="${esc(col)}" style="width:160px" placeholder="${t('wl_col')}">
      <input class="wl-vals" value="${esc((advWl[col]||[]).join(','))}" style="width:520px" placeholder="${t('wl_values')}">
      <button class="row-del wl-del">✕</button>`;
    tr.querySelectorAll('input').forEach(i=>i.addEventListener('input',syncWl));
    tr.querySelector('.wl-del').addEventListener('click',()=>{tr.remove();syncWl();});
    wrap.appendChild(tr);
  });
}
function syncWl(){
  const o={};
  document.querySelectorAll('#wlWrap .pc-line').forEach(tr=>{
    const col=tr.querySelector('.wl-col').value.trim();
    const vals=tr.querySelector('.wl-vals').value.split(/[,，]/).map(s=>s.trim()).filter(Boolean);
    if(col) o[col]=vals;
  });
  advWl=o;
}
document.getElementById("addWlBtn").addEventListener("click",()=>{
  const wrap=document.getElementById("wlWrap");
  const tr=document.createElement("div"); tr.className="pc-line";
  tr.innerHTML=`<input class="wl-col" list="tgtCols" style="width:160px" placeholder="${t('wl_col')}">
    <input class="wl-vals" style="width:520px" placeholder="${t('wl_values')}">
    <button class="row-del wl-del">✕</button>`;
  tr.querySelectorAll('input').forEach(i=>i.addEventListener('input',syncWl));
  tr.querySelector('.wl-del').addEventListener('click',()=>{tr.remove();syncWl();});
  wrap.appendChild(tr);
});

// ---- 载入 / 保存 ----
function loadAdvFromCfg(){
  advFields = clone(cfg.fields||[]);
  advUnitMap = clone(cfg.unit_map||{});
  advDicts = clone(cfg.dicts||{});
  advWl = clone(cfg.value_whitelist||{});
  document.getElementById("outSheetInput").value = cfg.output_sheet || "物料档案";
  renderTable(); renderUnitMap(); renderDicts(); renderWl();
  advLoaded=true; if(window.__dirty) window.__dirty.mat=false;
}
function advMsg(msg,kind){ const el=document.getElementById("advMsg"); el.textContent=msg; el.className="adv-msg "+(kind||""); }
function syncAdvToCfg(){
  syncUnitMap(); syncWl();
  cfg.fields=clone(advFields);
  cfg.unit_map=clone(advUnitMap);
  cfg.dicts=clone(advDicts);
  cfg.value_whitelist=clone(advWl);
  cfg.output_sheet=document.getElementById("outSheetInput").value.trim()||"物料档案";
}
function validateAdv(){
  const probs=[];
  const seen={};
  advFields.forEach((f,i)=>{
    const no=i+1;
    if(!f.target) probs.push(fmt("v_no_target",[no]));
    else { if(seen[f.target]) probs.push(fmt("v_dup_target",[no,f.target])); else seen[f.target]=true; }
    if(f.op==="dict" && !f.dict) probs.push(fmt("v_no_dict",[no]));
    if(f.op==="fixed" && !String(f.value||"").trim()) probs.push(fmt("v_no_value",[no]));
    if(f.op==="kwbool" && !(f.keywords||[]).length) probs.push(fmt("v_no_kw",[no]));
    if(f.op==="kwmap" && !Object.keys(f.kwmap||{}).length) probs.push(fmt("v_no_kwmap",[no]));
    if(f.op==="case" && !(f.cases||[]).length) probs.push(fmt("v_no_case",[no]));
    if(f.op==="cond" && !String(f.if||"").trim()) probs.push(fmt("v_no_cond",[no]));
  });
  return probs;
}

// 配置内容改为在「配置中心」按页签懒加载，见 showCfgTab()
document.getElementById("addFieldBtn").addEventListener("click",()=>{ if(!advLoaded&&cfg) loadAdvFromCfg(); advFields.push({target:"",source:"",op:"copy"}); renderTable(); });
document.getElementById("loadConfigBtn").addEventListener("click",async()=>{
  try{
    const r=await fetch("/api/config");
    if(!r.ok) throw new Error("HTTP "+r.status);
    cfg=await r.json(); if(!cfg.value_whitelist) cfg.value_whitelist={};
    loadAdvFromCfg(); advMsg(t("loaded_ok"),"ok"); refreshBackups();
    window.__cfgFail=false; applyCfgGuard();   // 读取成功即解除「禁止保存」，用户不必刷新整页
  }catch(err){ window.__cfgFail=true; applyCfgGuard(); advMsg(t("loaded_err")+fetchErrText(err),"err"); }
});
document.getElementById("saveConfigBtn").addEventListener("click",async()=>{
  syncAdvToCfg();
  const probs=validateAdv();
  if(probs.length){
    const msg=fmt("confirm_save",[probs.length]).replace("%S",probs.join("\n"));
    if(!confirm(msg)) return;
  }
  try{
    const r=await fetch("/api/config",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(cfg)});
    const d=await r.json();
    if(d.ok){ advMsg(t("saved_ok"),"ok"); window.__dirty.mat=false; refreshBackups(); } else advMsg(t("saved_err")+JSON.stringify(d.error),"err");
  }catch(err){ advMsg(err.message,"err"); }
});

// ---- 配置导入 / 导出 ----
document.getElementById("exportConfigBtn").addEventListener("click",()=>{
  if(!advLoaded && cfg) loadAdvFromCfg();
  if(!cfg) return;
  syncAdvToCfg();
  const stamp=new Date().toISOString().slice(0,19).replace(/[:T]/g,"");
  const blob=new Blob([JSON.stringify(cfg,null,2)],{type:"application/json"});
  const a=document.createElement("a");
  a.href=URL.createObjectURL(blob);
  a.download="mapping_"+stamp+".json";
  document.body.appendChild(a); a.click(); document.body.removeChild(a);
  setTimeout(()=>URL.revokeObjectURL(a.href),3000);
});
document.getElementById("importConfigBtn").addEventListener("click",()=>{
  document.getElementById("importCfgFile").click();
});
document.getElementById("importCfgFile").addEventListener("change",e=>{
  const f=e.target.files[0]; if(!f) return;
  const rd=new FileReader();
  rd.onload=()=>{
    try{
      const c=JSON.parse(rd.result);
      if(!c || typeof c!=="object" || !Array.isArray(c.fields)) throw new Error("not a mapping config");
      cfg=c; loadAdvFromCfg(); advMsg(t("imported_ok"),"ok");
    }catch(err){ advMsg(t("imported_err")+err.message,"err"); }
  };
  rd.readAsText(f,"utf-8");
  e.target.value="";
});

// ---- 备份列表 / 恢复 ----
async function refreshBackups(){
  try{
    const r=await fetch("/api/config-backups"); const d=await r.json();
    const sel=document.getElementById("backupSel");
    const items=d.items||[];
    sel.innerHTML = items.length
      ? items.map(it=>'<option value="'+esc(it.name)+'">'+esc(it.name)+'</option>').join("")
      : '<option value="">'+t("no_backup")+'</option>';
  }catch(e){}
}
document.getElementById("refreshBackupBtn").addEventListener("click",refreshBackups);
document.getElementById("restoreBtn").addEventListener("click",async()=>{
  const name=document.getElementById("backupSel").value;
  if(!name){ advMsg(t("no_backup"),"err"); return; }
  try{
    const r=await fetch("/api/config-restore?name="+encodeURIComponent(name),{method:"POST"});
    const d=await r.json();
    if(d.ok){ cfg=d.config; loadAdvFromCfg(); advMsg(t("restored_ok")+"："+name,"ok"); refreshBackups(); }
    else advMsg(t("restored_err")+JSON.stringify(d.error),"err");
  }catch(err){ advMsg(t("restored_err")+err.message,"err"); }
});

// ---- 使用说明 / 关于弹窗 ----
// 打开时把焦点送进弹窗、记下原来的焦点元素；关闭时归还，避免键盘用户丢失位置
let _modalFocus=null;
function showModal(id){
  _modalFocus=document.activeElement;
  const m=document.getElementById(id); m.style.display="flex";
  const f=m.querySelector("[data-autofocus]")||m.querySelector("button,a[href],input,select,textarea");
  if(f) setTimeout(()=>f.focus(),0);
}
function hideModal(id){
  document.getElementById(id).style.display="none";
  if(_modalFocus&&_modalFocus.focus){ try{ _modalFocus.focus(); }catch(e){} }
  _modalFocus=null;
}
document.getElementById("helpBtn").addEventListener("click",()=>{
  document.getElementById("helpBody").innerHTML=t("help_body");
  showModal("helpModal");
});
document.getElementById("helpClose").addEventListener("click",()=>hideModal("helpModal"));
document.getElementById("helpModal").addEventListener("click",e=>{ if(e.target.id==="helpModal") hideModal("helpModal"); });

document.getElementById("aboutBtn").addEventListener("click",async()=>{
  const body=document.getElementById("aboutBody");
  body.innerHTML='<p style="color:#6b7280">'+esc(t("prog_preparing"))+'</p>';
  showModal("aboutModal");
  try{
    const r=await fetch("/api/about"); const d=await r.json();
    const row=(k,v)=>'<tr><td style="padding:6px 16px 6px 0;color:#6b7280;white-space:nowrap;vertical-align:top">'+esc(k)+
      '</td><td style="padding:6px 0;word-break:break-all">'+esc(v==null||v===""?"-":String(v))+'</td></tr>';
    let html='<table style="width:100%;border-collapse:collapse;font-size:14px">';
    html+=row(t("about_version"), (d.app||"")+" v"+(d.version||""));
    html+=row(t("about_build"), d.build_time);
    html+=row(t("about_runtime"), d.runtime);
    html+=row(t("about_addr"), d.addr);
    html+=row(t("about_exe"), d.exe_path);
    html+=row(t("about_config"), d.config);
    html+=row(t("about_log"), d.log);
    html+=row(t("about_out"), d.out);
    html+=row(t("about_backup"), d.backup);
    html+='</table>';
    body.innerHTML=html;
  }catch(e){ body.innerHTML='<p style="color:#dc2626">'+esc(e.message)+'</p>'; }
});
document.getElementById("aboutClose").addEventListener("click",()=>hideModal("aboutModal"));
document.getElementById("aboutModal").addEventListener("click",e=>{ if(e.target.id==="aboutModal") hideModal("aboutModal"); });

// ---- 配置档（多套配置） ----
async function refreshProfiles(){
  try{
    const r=await fetch("/api/profiles"); const d=await r.json();
    const sel=document.getElementById("profileSel");
    const items=d.items||[];
    sel.innerHTML = items.length
      ? items.map(it=>'<option value="'+esc(it.name)+'">'+esc(it.name)+'</option>').join("")
      : '<option value="">'+t("profiles_none")+'</option>';
  }catch(e){}
}
async function profileAction(payload){
  const r=await fetch("/api/profiles",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(payload)});
  return await r.json();
}
document.getElementById("profileLoadBtn").addEventListener("click",async()=>{
  const name=document.getElementById("profileSel").value;
  if(!name){ advMsg(t("profiles_none"),"err"); return; }
  try{
    const d=await profileAction({action:"load",name:name});
    if(d.ok){ cfg=d.config; if(!cfg.value_whitelist) cfg.value_whitelist={}; loadAdvFromCfg(); advMsg(t("profiles_loaded"),"ok"); }
    else advMsg(t("profiles_err")+JSON.stringify(d.error),"err");
  }catch(err){ advMsg(t("profiles_err")+err.message,"err"); }
});
document.getElementById("profileSaveBtn").addEventListener("click",async()=>{
  const name=prompt(t("profiles_prompt"));
  if(!name) return;
  if(!advLoaded && cfg) loadAdvFromCfg();
  syncAdvToCfg();
  try{
    const d=await profileAction({action:"save",name:name,config:cfg});
    if(d.ok){ advMsg(t("profiles_saved")+d.name,"ok"); refreshProfiles(); }
    else advMsg(t("profiles_err")+JSON.stringify(d.error),"err");
  }catch(err){ advMsg(t("profiles_err")+err.message,"err"); }
});
document.getElementById("profileDelBtn").addEventListener("click",async()=>{
  const name=document.getElementById("profileSel").value;
  if(!name){ advMsg(t("profiles_none"),"err"); return; }
  if(!confirm(fmt("profiles_del_confirm",[name]))) return;
  try{
    const d=await profileAction({action:"delete",name:name});
    if(d.ok){ advMsg(t("profiles_saved")+d.name,"ok"); refreshProfiles(); }
    else advMsg(t("profiles_err")+JSON.stringify(d.error),"err");
  }catch(err){ advMsg(t("profiles_err")+err.message,"err"); }
});

// ---- 值域表 CSV 导出 / 导入 ----
document.getElementById("valuesExportBtn").addEventListener("click",()=>{
  const a=document.createElement("a");
  a.href="/api/values-export"; a.download="values.csv";
  document.body.appendChild(a); a.click(); document.body.removeChild(a);
});
document.getElementById("valuesImportBtn").addEventListener("click",()=>{
  document.getElementById("valuesFile").click();
});
document.getElementById("valuesFile").addEventListener("change",e=>{
  const f=e.target.files[0]; if(!f) return;
  if(!confirm(t("values_confirm"))){ e.target.value=""; return; }
  const rd=new FileReader();
  rd.onload=async()=>{
    if(!advLoaded && cfg) loadAdvFromCfg();
    syncAdvToCfg();
    try{
      const r=await fetch("/api/values-import",{method:"POST",headers:{"Content-Type":"application/json"},
        body:JSON.stringify({config:cfg,csv:String(rd.result||"")})});
      const d=await r.json();
      if(d.ok){
        cfg=d.config; if(!cfg.value_whitelist) cfg.value_whitelist={};
        loadAdvFromCfg();
        const s=d.stats||{};
        advMsg(t("values_loaded")+"  ("+(s.dict||0)+" / "+(s.unit||0)+" / "+(s.whitelist||0)+")","ok");
      } else advMsg(t("values_err")+JSON.stringify(d.error),"err");
    }catch(err){ advMsg(t("values_err")+err.message,"err"); }
  };
  rd.readAsText(f,"utf-8");
  e.target.value="";
});

// ---- 转换进度 ----
// 进度轮询已收进 makeProgress 工厂（见上方「通用工厂」一节），这里不再保留独立定时器变量

// ==================== 模块切换 ====================
// 打开先看到模块选择页。四个视图：home（选模块）/ materials / orders / config（配置中心）。
// 配置统一收在 config 视图，操作页面只负责选文件与转换。
let currentModule="home";
const VIEW_IDS={home:"viewHome",materials:"viewMaterials",orders:"viewOrders",config:"viewConfig"};

function showModule(name){
  if(!VIEW_IDS[name]) name="home";
  currentModule=name;
  Object.keys(VIEW_IDS).forEach(k=>{
    const el=document.getElementById(VIEW_IDS[k]);
    if(el) el.style.display = (name===k)?"":"none";
  });
  document.getElementById("backHomeBtn").style.display = name==="home"?"none":"";
  applyModuleChrome();
  document.getElementById("cfgNavBtn").classList.toggle("active-nav",name==="config");
  window.scrollTo({top:0,behavior:"smooth"});
  if(name==="orders"){
    if(!ordCfgLoaded) loadOrdersCfg();
    loadOrdersHistory();
  }
  if(name==="config"){
    showCfgTab(cfgTab,true);
  }
}

// ---------- 配置中心：两个模块的配置集中在这里维护 ----------
let cfgTab="mat";
function showCfgTab(which,keepScroll){
  cfgTab = (which==="ord"||which==="sys") ? which : "mat";
  document.getElementById("cfgPaneMat").style.display = cfgTab==="mat"?"":"none";
  document.getElementById("cfgPaneOrd").style.display = cfgTab==="ord"?"":"none";
  const sp=document.getElementById("cfgPaneSys");
  if(sp) sp.style.display = cfgTab==="sys"?"":"none";
  document.querySelectorAll(".cfg-tab").forEach(b=>{
    b.classList.toggle("active", b.dataset.tab===cfgTab); b.setAttribute("aria-selected", b.dataset.tab===cfgTab?"true":"false");
  });
  // 懒加载：第一次看到某个面板时才去读它那份配置
  if(cfgTab==="mat"){ if(!advLoaded && cfg) loadAdvFromCfg(); }
  else if(cfgTab==="ord"){ if(!ordCfgLoaded) loadOrdersCfg(); }
  else { loadSysInfo(); }
  if(!keepScroll) window.scrollTo({top:0,behavior:"smooth"});
}
function applyModuleChrome(){
  const title=document.getElementById("app_title");
  const sub=document.getElementById("app_subtitle");
  const logo=document.getElementById("appLogo");
  if(currentModule==="orders"){
    title.textContent=t("ord_app_title");
    sub.textContent=t("ord_app_subtitle");
    logo.textContent="ORD";
    document.title=t("ord_app_title");
  }else if(currentModule==="config"){
    title.textContent=t("cfg_title");
    sub.textContent=t("cfg_subtitle");
    logo.textContent="CFG";
    document.title=t("cfg_title");
  }else{
    title.textContent=t("app_title");
    sub.textContent=t("app_subtitle");
    logo.textContent="MES";
    document.title=t("app_title");
  }
}
document.getElementById("modMaterials").addEventListener("click",()=>showModule("materials"));
document.getElementById("modOrders").addEventListener("click",()=>showModule("orders"));
document.getElementById("cfgEntry").addEventListener("click",()=>{ if(window.__cfgReady) showModule("config"); });
document.getElementById("cfgNavBtn").addEventListener("click",()=>showModule("config"));
document.getElementById("cfgTabMat").addEventListener("click",()=>showCfgTab("mat"));
document.getElementById("cfgTabOrd").addEventListener("click",()=>showCfgTab("ord"));
const _cfgTabSysBtn=document.getElementById("cfgTabSys");
if(_cfgTabSysBtn) _cfgTabSysBtn.addEventListener("click",()=>showCfgTab("sys"));
document.getElementById("backHomeBtn").addEventListener("click",()=>{
  if(currentModule==="config" && (window.__dirty.mat||window.__dirty.ord) && !confirm(t("cfg_leave_confirm"))) return;
  showModule("home");
});
// 切语言后订单模块要跟着重渲染（配置编辑器是动态生成的，不会自己更新）
document.getElementById("langSel").addEventListener("change",()=>{
  if(ordCfgLoaded){ const bak=ordCfg; ordCfg=collectOrdCfg(); ordRenderCfg(); ordCfg=bak; }
  if(currentModule==="orders") loadOrdersHistory();
  if(ordLastResult && document.getElementById("ordResultSection").classList.contains("show")){
    ordRenderStats(ordLastResult.data);
    ordRenderChecks(ordLastResult.data);
  }
});

// ==================== 订单 / 工单模块 ====================
let ordCfg=null, ordCfgLoaded=false, ordLastResult=null;

function ordVal(id){ const e=document.getElementById(id); return e?(e.value||"").trim():""; }
function ordRenderStats(data){
  document.getElementById("ordStats").innerHTML =
    statCard(t("rows_label"),data.rows)
    + statCard(t("elapsed_label"),(data.elapsed||0)+" ms")
    + statCard(t("ord_block_map"),formatDist(data.types))
    + statCard(t("ord_check_title"),(data.issue_total||0)+"");
}
// 预检渲染：复用物料档案那套（makeChecksRenderer），只是元素 id 与文案 key 换成订单的
const ordRenderChecks=makeChecksRenderer({
  sec:"ordCheckSection", list:"ordCheckList", wrap:"ordIssueWrap", body:"ordIssueBody", sum:"ordCheckSummary",
  summaryFmt:"ord_check_summary", okKey:"ord_all_ok", lastResult:()=>ordLastResult });
const loadOrdersHistory=makeHistoryLoader({
  url:"/api/orders/history", body:"ordHistBody", sec:"ordHistSection", btnCls:"jsordview", view:ordViewReport,
  k:{none:"ord_hist_none", bad:"ord_hist_bad", ok:"ord_hist_ok", dl:"ord_hist_dl"} });
async function ordViewReport(id,noScroll){
  if(!id) return;
  try{
    const r=await fetch("/api/report?id="+encodeURIComponent(id));
    const d=await r.json();
    if(!d.ok || !d.report) return;
    ordRenderChecks(d.report,true);
    if(!noScroll) document.getElementById("ordCheckSection").scrollIntoView({behavior:"smooth",block:"center"});
  }catch(e){}
}
document.getElementById("ordRefreshHistBtn").addEventListener("click",loadOrdersHistory);

// ---- 转换 ----
async function doOrdersConvert(){
  const ofs=ordSrcPicker.get();
  const wfs=ordWorkPicker.get();
  if(!ofs.length && !wfs.length){ ordShowStatus("err","ord_no_file"); return; }
  ordSetBusy(true); ordShowStatus("","");
  ordStartProgressPoll();
  let doneText="";
  const fd=new FormData();
  ofs.forEach(f=>fd.append("order",f));
  wfs.forEach(f=>fd.append("work",f));
  fd.append("config", JSON.stringify(ordCfg||{}));
  fd.append("lang",currentLang);
  try{
    const resp=await fetch("/api/orders/convert",{method:"POST",body:fd,signal:timeoutSignal(CONVERT_TIMEOUT_MS)});
    const data=await safeJson(resp);
    ordRenderLog(data.log||[]);
    if(data.ok){
      ordShowStatus("ok","status_done");
      const rs=document.getElementById("ordResultSection"); rs.classList.add("show");
      document.getElementById("ordDownloadLink").href="/download?file="+encodeURIComponent(data.file);
      ordRenderStats(data);
      ordLastResult={data:data,lang:currentLang};
      ordRenderChecks(data);
      loadOrdersHistory();
      doneText=fmt("prog_done",[data.rows||0]);
      rs.scrollIntoView({behavior:"smooth",block:"nearest"});
    }else{
      ordShowStatus("err", data.error || t("status_error"));
      if(data.log) ordRenderLog(data.log);
    }
  }catch(e){
    ordShowStatus("err",fetchErrText(e));
  }finally{ ordSetBusy(false); ordFinishProgress(doneText); }
}
document.getElementById("ordStartBtn").addEventListener("click",doOrdersConvert);
document.getElementById("ordOpenFolderBtn").addEventListener("click",async()=>{
  const b=document.getElementById("ordOpenFolderBtn"); const old=b.textContent;
  b.textContent=t("opening");
  try{ await fetch("/api/open-folder"); }catch(e){}
  setTimeout(()=>{ b.textContent=old; },900);
});
document.getElementById("ordToggleLogBtn").addEventListener("click",()=>{
  const box=document.getElementById("ordLogBox"); box.classList.toggle("show");
  document.getElementById("ordToggleLogBtn").textContent = box.classList.contains("show")?t("hide_log"):t("ord_view_log");
});
document.getElementById("ordCopyLogBtn").addEventListener("click",()=>{
  const txt=document.getElementById("ordLogBox").textContent||"";
  if(navigator.clipboard) navigator.clipboard.writeText(txt);
});
bindFile("ordSrcFile","ordSrcName","ordSrcCard",null);
bindFile("ordWorkFile","ordWorkName","ordWorkCard",null);

// ---- 订单配置编辑器（全表单，无 JSON） ----
const ORD_CATS=[
  {key:"order_master", labelKey:"ord_cat_order_master"},
  {key:"order_detail", labelKey:"ord_cat_order_detail"},
  {key:"work_order",   labelKey:"ord_cat_work_order"}
];
function ordEnsureCfg(c){
  c=c||{};
  c.field_mapping=c.field_mapping||{};
  c.default_values=c.default_values||{};
  c.derived_columns=c.derived_columns||{};
  c.required_columns=c.required_columns||{};
  c.column_aliases=c.column_aliases||{};
  c.column_order=c.column_order||{};
  c.template_headers=c.template_headers||{};
  c.tag_materials=c.tag_materials||{enabled:false,column:"",source_field:"",prefixes:[],yes:"是",no:"否"};
  c.settings=c.settings||{};
  return c;
}
async function loadOrdersCfg(){
  try{
    const r=await fetch("/api/orders/config"); const d=await r.json();
    if(!d.ok) throw new Error("not ok");
    ordCfg=ordEnsureCfg(d.config);
    ordCfgLoaded=true; window.__ordFail=false;
    ordRenderCfg();
  }catch(e){ window.__ordFail=true; }
  applyCfgGuard();
}
function ordRenderCfg(){
  if(!ordCfg) return;
  // 字段映射：每个 category 一张表，行序 = 输出列顺序
  document.getElementById("ordMapWrap").innerHTML = ORD_CATS.map(cat=>{
    const order=ordCfg.column_order[cat.key]||[];
    const map=ordCfg.field_mapping[cat.key]||{};
    const defs=ordCfg.default_values[cat.key]||{};
    const t2s={};
    Object.keys(map).forEach(s=>{ if(map[s]) t2s[map[s]]=s; });
    const rows=order.map(tgt=>'<tr data-target="'+esc(tgt)+'">'
      +'<td>'+esc(tgt)+'</td>'
      +'<td><input class="om-src" value="'+esc(t2s[tgt]||"")+'"></td>'
      +'<td><input class="om-def" value="'+esc(defs[tgt]||"")+'"></td>'
      +'<td><button class="row-del om-del">✕</button></td></tr>').join("");
    return '<div class="adv-sub">'+esc(t(cat.labelKey))+'</div>'
      +'<table class="ord-map" data-cat="'+cat.key+'"><thead><tr>'
      +'<th style="width:24%">'+t("ord_th_target")+'</th>'
      +'<th style="width:30%">'+t("ord_th_source")+'</th>'
      +'<th>'+t("ord_th_default")+'</th>'
      +'<th style="width:70px">'+t("ord_th_op")+'</th>'
      +'</tr></thead><tbody>'+rows+'</tbody></table>'
      +'<button class="mini-btn om-add" data-cat="'+cat.key+'">'+t("ord_add_row")+'</button>';
  }).join("");
  // 打标判定
  const tg=ordCfg.tag_materials||{};
  document.getElementById("ordTagWrap").innerHTML =
    '<label><input type="checkbox" id="ordTagEnabled" '+(tg.enabled?"checked":"")+'> '+t("ord_block_tag")+'</label>'
    +'<span class="pc-inline">'+t("ord_tag_column")+' <input type="text" id="ordTagColumn" value="'+esc(tg.column||"")+'" style="width:130px"></span>'
    +'<span class="pc-inline">'+t("ord_tag_source")+' <input type="text" id="ordTagSource" value="'+esc(tg.source_field||"")+'" style="width:130px"></span>'
    +'<span class="pc-inline">'+t("ord_tag_prefixes")+' <input type="text" id="ordTagPrefixes" value="'+esc((tg.prefixes||[]).join(","))+'" style="width:280px"></span>'
    +'<span class="pc-inline">'+t("ord_tag_yes")+' <input type="text" id="ordTagYes" value="'+esc(tg.yes||"")+'" style="width:60px"></span>'
    +'<span class="pc-inline">'+t("ord_tag_no")+' <input type="text" id="ordTagNo" value="'+esc(tg.no||"")+'" style="width:60px"></span>';
  // 运行开关
  const st=ordCfg.settings||{};
  document.getElementById("ordSwitches").innerHTML =
    '<label><input type="checkbox" id="ordSwHeader" '+(st.use_template_header?"checked":"")+'> '+t("ord_sw_header")+'</label>'
    +'<label><input type="checkbox" id="ordSwLine" '+(st.auto_fill_order_line?"checked":"")+'> '+t("ord_sw_autoline")+'</label>'
    +'<label><input type="checkbox" id="ordSwSum" '+(st.filter_summary_rows?"checked":"")+'> '+t("ord_sw_filtersum")+'</label>'
    +'<label><input type="checkbox" id="ordSwVal" '+(st.enable_validation?"checked":"")+'> '+t("ord_sw_validate")+'</label>';
  // 表头别名表
  const al=ordCfg.column_aliases||{};
  document.getElementById("ordAliasWrap").innerHTML =
    '<table><thead><tr><th style="width:30%">'+t("ord_th_target")+'</th><th>'+t("ord_th_source")+'</th><th style="width:70px">'+t("ord_th_op")+'</th></tr></thead><tbody>'
    +Object.keys(al).map(k=>'<tr><td><input class="oa-k" value="'+esc(k)+'"></td>'
      +'<td><input class="oa-v" value="'+esc((al[k]||[]).join(", "))+'"></td>'
      +'<td><button class="row-del oa-del">✕</button></td></tr>').join("")
    +'</tbody></table><button class="mini-btn oa-add">'+t("ord_add_row")+'</button>';
  bindOrdCfgEvents();
}
function bindOrdCfgEvents(){
  document.querySelectorAll("#ordMapWrap button.om-del").forEach(b=>{
    b.addEventListener("click",()=>b.closest("tr").remove());
  });
  document.querySelectorAll("#ordMapWrap button.om-add").forEach(b=>{
    b.addEventListener("click",()=>{
      const cat=b.getAttribute("data-cat");
      const tbody=document.querySelector('#ordMapWrap table.ord-map[data-cat="'+cat+'"] tbody');
      const tr=document.createElement("tr");
      tr.setAttribute("data-target","");
      tr.innerHTML='<td><input class="om-newtgt" placeholder="'+t("ord_th_target")+'"></td>'
        +'<td><input class="om-src"></td><td><input class="om-def"></td>'
        +'<td><button class="row-del om-del">✕</button></td>';
      tbody.appendChild(tr);
      tr.querySelector(".om-del").addEventListener("click",()=>tr.remove());
    });
  });
  document.querySelectorAll("#ordAliasWrap button.oa-del").forEach(b=>{
    b.addEventListener("click",()=>b.closest("tr").remove());
  });
  const oa=document.querySelector("#ordAliasWrap button.oa-add");
  if(oa) oa.addEventListener("click",()=>{
    const tbody=document.querySelector("#ordAliasWrap tbody");
    const tr=document.createElement("tr");
    tr.innerHTML='<td><input class="oa-k"></td><td><input class="oa-v"></td><td><button class="row-del oa-del">✕</button></td>';
    tbody.appendChild(tr);
    tr.querySelector(".oa-del").addEventListener("click",()=>tr.remove());
  });
}
// 从表单收集配置（表格行序即输出列顺序）
function collectOrdCfg(){
  const c=ordEnsureCfg(clone(ordCfg||{}));
  document.querySelectorAll("#ordMapWrap table.ord-map").forEach(tb=>{
    const cat=tb.getAttribute("data-cat");
    const map={}, defs={}, order=[];
    tb.querySelectorAll("tbody tr").forEach(tr=>{
      let tgt=tr.getAttribute("data-target")||"";
      const newInp=tr.querySelector(".om-newtgt");
      if(newInp) tgt=(newInp.value||"").trim();
      if(!tgt) return;
      order.push(tgt);
      const src=(tr.querySelector(".om-src").value||"").trim();
      const def=tr.querySelector(".om-def").value||"";
      if(src) map[src]=tgt;
      if(def!=="") defs[tgt]=def;
    });
    c.field_mapping[cat]=map;
    c.default_values[cat]=defs;
    c.column_order[cat]=order;
  });
  const tagEl=document.getElementById("ordTagEnabled");
  c.tag_materials={
    enabled: !!(tagEl && tagEl.checked),
    column: ordVal("ordTagColumn"),
    source_field: ordVal("ordTagSource"),
    prefixes: ordVal("ordTagPrefixes").split(/[,，\s]+/).filter(Boolean),
    yes: ordVal("ordTagYes"),
    no: ordVal("ordTagNo")
  };
  const swH=document.getElementById("ordSwHeader");
  if(swH){
    c.settings.use_template_header=swH.checked;
    c.settings.auto_fill_order_line=document.getElementById("ordSwLine").checked;
    c.settings.filter_summary_rows=document.getElementById("ordSwSum").checked;
    c.settings.enable_validation=document.getElementById("ordSwVal").checked;
  }
  const al={};
  document.querySelectorAll("#ordAliasWrap tbody tr").forEach(tr=>{
    const k=(tr.querySelector(".oa-k").value||"").trim();
    const v=(tr.querySelector(".oa-v").value||"").trim();
    if(k) al[k]=v.split(/[,，]/).map(s=>s.trim()).filter(Boolean);
  });
  if(Object.keys(al).length) c.column_aliases=al;
  return c;
}
function ordAdvMsg(text,ok){
  const el=document.getElementById("ordAdvMsg");
  el.className="adv-msg "+(ok?"ok":"err");
  el.textContent=text;
}
document.getElementById("ordCfgReloadBtn").addEventListener("click",()=>{
  ordCfgLoaded=false; window.__dirty.ord=false;
  loadOrdersCfg().then(()=>ordAdvMsg(t("loaded_ok"),true));
});
document.getElementById("ordCfgSaveBtn").addEventListener("click",async()=>{
  const c=collectOrdCfg();
  try{
    const r=await fetch("/api/orders/config",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(c)});
    const d=await r.json();
    if(d.ok){ ordCfg=c; window.__dirty.ord=false; ordAdvMsg(t("ord_cfg_saved"),true); }
    else ordAdvMsg(t("ord_cfg_fail")+(d.error||""),false);
  }catch(e){ ordAdvMsg(t("ord_cfg_fail")+((e&&e.message)||""),false); }
});

// ============================================================================
// 本轮新增：批量多文件 / 口令锁 / 系统页签 / 配置变更确认 / 字段表交互 / 结果小图表
//
// 文案单独放一块 overlay 再并进 STR，避免在既有四语大表里改动时误伤上下文。
// 这一段整体插在 init() 之前，所以合并动作一定先于第一次 t() 调用。
// ============================================================================
const STR2 = {
  zh:{
    src_drop_hint:"也可以直接把文件拖到这里；可一次选多个，按列名合并成一份结果",
    ord_drop_hint_multi:"也可以直接把文件拖到这里；可一次选多个",
    src_multi:"已选择 %d 个文件，将合并成一份结果", src_more:"… 另有 %d 个文件",
    donut_title:"本次结果占比", donut_ok:"正常行", donut_warn:"告警", donut_err:"错误",
    donut_rows:"共 %d 行", donut_none:"无问题",
    stats_title:"转换统计", stats_runs:"转换次数", stats_rows:"累计输出行数",
    stats_clean:"零问题次数", stats_with_issues:"有问题次数", stats_issues:"问题条数",
    stats_warns:"告警条数", stats_mat:"物料档案", stats_ord:"订单 / 工单",
    stats_none:"还没有转换记录", stats_bars:"按天（最近 %d 天）",
    diff_title:"确认配置变更", diff_hint:"下面是这次保存会带来的改动，确认无误后再保存；取消则不会写入。",
    diff_none:"没有检测到改动", diff_ok:"确认保存", diff_cancel:"取消",
    diff_add:"新增", diff_del:"删除", diff_chg:"修改", diff_col:"第 %d 行",
    dg_fields:"字段映射", dg_dicts:"字典表", dg_unit_map:"单位表", dg_value_whitelist:"允许值清单",
    dg_output_sheet:"输出工作表名称", dg_field_mapping:"字段映射", dg_default_values:"默认值",
    dg_column_order:"输出列顺序", dg_required_columns:"必填列", dg_column_aliases:"表头别名表",
    dg_date_columns:"日期列", dg_template_headers:"表头模板", dg_tag_materials:"打标判定",
    dg_settings:"运行选项", dg_duplicate_keys:"重复检查键",
    cfg_tab_sys:"系统",
    sys_desc:"程序自身的运行方式：谁能访问、开机是否自动启动、MES 模板有没有变、出问题怎么取证。与字段映射无关；涉及监听的改动需要重启程序。",
    sys_block_access:"访问与安全",
    sys_block_access_hint:"默认只监听本机 127.0.0.1，同事访问不到。开启局域网后建议同时设一个口令。",
    sys_lan_title:"局域网访问",
    sys_lan_desc:"打开后，同一局域网内的同事用浏览器即可访问本工具（改完需重启程序生效）。",
    sys_lan_sw:"允许局域网访问",
    sys_pwd_title:"访问口令",
    sys_pwd_desc:"设了口令后，除探活外的所有接口都要先验证；口令只存加盐哈希，不存明文。",
    sys_auto_title:"开机自启",
    sys_auto_desc:"登录 Windows 后自动在右下角托盘运行，不弹浏览器。只写当前用户的启动项，不需要管理员权限。",
    sys_auto_sw:"开机自动运行",
    sys_block_tpl:"物料档案模板基准",
    sys_block_tpl_hint:"第一次转换会把 MES 模板的表头记下来；以后模板被换过（加列 / 删列 / 调顺序）转换时会告警。",
    sys_block_diag:"诊断与更新",
    sys_block_diag_hint:"程序会在启动时自动检查 GitHub 上的新版本并后台更新（下次启动生效）；也可点右侧按钮立即更新。排障包用于出问题时把日志 + 配置 + 环境信息（口令已脱敏）发给维护的人。",
    sys_save:"保存", sys_on:"已开启", sys_off:"已关闭", sys_pwd_set:"已启用", sys_pwd_unset:"未启用",
    sys_pwd_new_ph:"新口令", sys_pwd_old_ph:"原口令",
    sys_pwd_save:"设置口令", sys_pwd_clear:"取消口令",
    sys_tpl_reset:"清除基准", sys_refresh:"刷新",
    sys_diag:"生成排障包", sys_diag_dl:"下载排障包", sys_update:"检查并更新", sys_update_restarting:"正在重启以生效新版本…",
    sys_addr:"局域网地址：", sys_autocmd:"启动命令：",
    tbl_search_ph:"搜索目标列 / 源列…", tbl_group_all:"全部填充方式", tbl_shown:"显示 %d / %d 行",
    tbl_drag_hint:"拖动左侧 ⠿ 调整顺序（顺序即输出列顺序）",
    auth_title:"需要口令", auth_hint:"本工具已开启口令保护，请先输入口令后再操作。",
    auth_ok:"进入", auth_bad:"口令不正确", auth_pwd_ph:"请输入口令", need_auth:"需要口令才能操作",
    wc_title:"欢迎使用 MES 物料档案转换工具",
    wc_desc:"第一次运行，先花 10 秒了解三件事。",
    wc_lang:"界面语言",
    wc_tip1:"结果文件会保存到程序目录下的 out 文件夹",
    wc_tip2:"请把程序放在可写目录，不要放进 Program Files 这类需要管理员权限的位置",
    wc_tip3:"程序常驻右下角托盘，关掉浏览器标签页不会退出；要完全关闭请用托盘右键菜单的「退出」",
    wc_ok:"开始使用",
  },
  zht:{
    src_drop_hint:"也可以直接把檔案拖到這裡；可一次選多個，按欄名合併成一份結果",
    ord_drop_hint_multi:"也可以直接把檔案拖到這裡；可一次選多個",
    src_multi:"已選擇 %d 個檔案，將合併成一份結果", src_more:"… 另有 %d 個檔案",
    donut_title:"本次結果佔比", donut_ok:"正常列", donut_warn:"告警", donut_err:"錯誤",
    donut_rows:"共 %d 列", donut_none:"無問題",
    stats_title:"轉換統計", stats_runs:"轉換次數", stats_rows:"累計輸出列數",
    stats_clean:"零問題次數", stats_with_issues:"有問題次數", stats_issues:"問題條數",
    stats_warns:"告警條數", stats_mat:"物料檔案", stats_ord:"訂單 / 工單",
    stats_none:"還沒有轉換記錄", stats_bars:"按天（最近 %d 天）",
    diff_title:"確認設定變更", diff_hint:"下面是這次儲存會帶來的改動，確認無誤後再儲存；取消則不會寫入。",
    diff_none:"沒有偵測到改動", diff_ok:"確認儲存", diff_cancel:"取消",
    diff_add:"新增", diff_del:"刪除", diff_chg:"修改", diff_col:"第 %d 列",
    dg_fields:"欄位對應", dg_dicts:"字典表", dg_unit_map:"單位表", dg_value_whitelist:"允許值清單",
    dg_output_sheet:"輸出工作表名稱", dg_field_mapping:"欄位對應", dg_default_values:"預設值",
    dg_column_order:"輸出欄順序", dg_required_columns:"必填欄", dg_column_aliases:"表頭別名表",
    dg_date_columns:"日期欄", dg_template_headers:"表頭樣板", dg_tag_materials:"打標判定",
    dg_settings:"執行選項", dg_duplicate_keys:"重複檢查鍵",
    cfg_tab_sys:"系統",
    sys_desc:"程式本身的執行方式：誰能存取、開機是否自動啟動、MES 樣板有沒有變、出問題怎麼蒐證。與欄位對應無關；涉及監聽的改動需要重新啟動程式。",
    sys_block_access:"存取與安全",
    sys_block_access_hint:"預設只監聽本機 127.0.0.1，同事存取不到。開啟區域網路後建議同時設一組密碼。",
    sys_lan_title:"區域網路存取",
    sys_lan_desc:"開啟後，同一區域網路內的同事用瀏覽器即可存取本工具（改完需重新啟動才生效）。",
    sys_lan_sw:"允許區域網路存取",
    sys_pwd_title:"存取密碼",
    sys_pwd_desc:"設了密碼後，除探活外的所有介面都要先驗證；密碼只存加鹽雜湊，不存明文。",
    sys_auto_title:"開機自啟",
    sys_auto_desc:"登入 Windows 後自動在右下角托盤執行，不彈瀏覽器。只寫目前使用者的啟動項，不需要管理員權限。",
    sys_auto_sw:"開機自動執行",
    sys_block_tpl:"物料檔案樣板基準",
    sys_block_tpl_hint:"第一次轉換會把 MES 樣板的表頭記下來；以後樣板被換過（加欄 / 刪欄 / 調順序）轉換時會告警。",
    sys_block_diag:"診斷與更新",
    sys_block_diag_hint:"程式會在啟動時自動檢查 GitHub 上的新版本並背景更新（下次啟動生效）；也可點右側按鈕立即更新。排障包用於出問題時把日誌 + 設定 + 環境資訊（密碼已去識別化）發給維護的人。",
    sys_save:"儲存", sys_on:"已開啟", sys_off:"已關閉", sys_pwd_set:"已啟用", sys_pwd_unset:"未啟用",
    sys_pwd_new_ph:"新密碼", sys_pwd_old_ph:"原密碼",
    sys_pwd_save:"設定密碼", sys_pwd_clear:"取消密碼",
    sys_tpl_reset:"清除基準", sys_refresh:"重新整理",
    sys_diag:"產生排障包", sys_diag_dl:"下載排障包", sys_update:"檢查並更新", sys_update_restarting:"正在重新啟動以生效新版本…",
    sys_addr:"區域網路網址：", sys_autocmd:"啟動命令：",
    tbl_search_ph:"搜尋目標欄 / 來源欄…", tbl_group_all:"全部填入方式", tbl_shown:"顯示 %d / %d 列",
    tbl_drag_hint:"拖曳左側 ⠿ 調整順序（順序即輸出欄順序）",
    auth_title:"需要密碼", auth_hint:"本工具已開啟密碼保護，請先輸入密碼後再操作。",
    auth_ok:"進入", auth_bad:"密碼不正確", auth_pwd_ph:"請輸入密碼", need_auth:"需要密碼才能操作",
    wc_title:"歡迎使用 MES 物料檔案轉換工具",
    wc_desc:"第一次執行，先花 10 秒了解三件事。",
    wc_lang:"介面語言",
    wc_tip1:"結果檔案會儲存到程式目錄下的 out 資料夾",
    wc_tip2:"請把程式放在可寫入的目錄，不要放進 Program Files 這類需要管理員權限的位置",
    wc_tip3:"程式常駐右下角系統匣，關掉瀏覽器分頁不會結束；要完全關閉請用系統匣右鍵選單的「結束」",
    wc_ok:"開始使用",
  },
  vi:{
    src_drop_hint:"Cũng có thể kéo tệp vào đây; có thể chọn nhiều tệp và gộp theo tên cột thành một kết quả",
    ord_drop_hint_multi:"Cũng có thể kéo tệp vào đây; có thể chọn nhiều tệp cùng lúc",
    src_multi:"Đã chọn %d tệp — sẽ gộp thành một kết quả", src_more:"… và %d tệp nữa",
    donut_title:"Tỷ lệ kết quả lần này", donut_ok:"Dòng bình thường", donut_warn:"Cảnh báo", donut_err:"Lỗi",
    donut_rows:"Tổng %d dòng", donut_none:"Không có vấn đề",
    stats_title:"Thống kê chuyển đổi", stats_runs:"Số lần chuyển", stats_rows:"Tổng số dòng",
    stats_clean:"Lần sạch", stats_with_issues:"Lần có vấn đề", stats_issues:"Số vấn đề",
    stats_warns:"Số cảnh báo", stats_mat:"Hồ sơ vật liệu", stats_ord:"Đơn hàng / Lệnh sản xuất",
    stats_none:"Chưa có lần chuyển đổi nào", stats_bars:"Theo ngày (%d ngày gần nhất)",
    diff_title:"Xác nhận thay đổi cấu hình", diff_hint:"Đây là những thay đổi lần lưu này sẽ tạo ra. Hủy thì không ghi gì cả.",
    diff_none:"Không phát hiện thay đổi", diff_ok:"Lưu", diff_cancel:"Hủy",
    diff_add:"Thêm", diff_del:"Xóa", diff_chg:"Sửa", diff_col:"dòng %d",
    dg_fields:"Ánh xạ trường", dg_dicts:"Bảng từ điển", dg_unit_map:"Bảng đơn vị", dg_value_whitelist:"Giá trị cho phép",
    dg_output_sheet:"Tên trang tính đầu ra", dg_field_mapping:"Ánh xạ trường", dg_default_values:"Giá trị mặc định",
    dg_column_order:"Thứ tự cột đầu ra", dg_required_columns:"Cột bắt buộc", dg_column_aliases:"Bảng tên thay thế",
    dg_date_columns:"Cột ngày", dg_template_headers:"Tiêu đề mẫu", dg_tag_materials:"Quy tắc đánh dấu",
    dg_settings:"Tùy chọn chạy", dg_duplicate_keys:"Khóa kiểm tra trùng",
    cfg_tab_sys:"Hệ thống",
    sys_desc:"Cách chương trình tự chạy: ai truy cập được, có khởi động cùng Windows không, mẫu MES có đổi không, và cách thu thập chứng cứ khi có sự cố. Không liên quan ánh xạ trường; thay đổi cổng nghe cần khởi động lại.",
    sys_block_access:"Truy cập & bảo mật",
    sys_block_access_hint:"Mặc định chỉ nghe 127.0.0.1 nên đồng nghiệp không vào được. Sau khi mở mạng LAN nên đặt thêm mật khẩu.",
    sys_lan_title:"Truy cập mạng LAN",
    sys_lan_desc:"Khi bật, đồng nghiệp cùng mạng LAN có thể mở công cụ này bằng trình duyệt (cần khởi động lại).",
    sys_lan_sw:"Cho phép truy cập LAN",
    sys_pwd_title:"Mật khẩu truy cập",
    sys_pwd_desc:"Sau khi đặt, mọi API trừ kiểm tra sống đều cần xác thực. Chỉ lưu hash có muối, không lưu mật khẩu thô.",
    sys_auto_title:"Khởi động cùng Windows",
    sys_auto_desc:"Chạy im lặng ở khay sau khi đăng nhập Windows. Chỉ ghi mục khởi động của người dùng hiện tại, không cần quyền admin.",
    sys_auto_sw:"Chạy cùng Windows",
    sys_block_tpl:"Mốc tiêu đề mẫu vật liệu",
    sys_block_tpl_hint:"Lần chuyển đầu tiên sẽ ghi lại tiêu đề mẫu MES; các thay đổi sau (thêm / bớt / đổi thứ tự cột) sẽ được cảnh báo.",
    sys_block_diag:"Chẩn đoán & cập nhật",
    sys_block_diag_hint:"Chương trình sẽ tự kiểm tra bản mới trên GitHub khi khởi động và cập nhật nền (có hiệu lực lần khởi động sau); cũng có thể bấm nút bên phải để cập nhật ngay. Gói chẩn đoán dùng để gửi log + cấu hình + môi trường (mật khẩu đã ẩn) cho người bảo trì khi có lỗi.",
    sys_save:"Lưu", sys_on:"Đang bật", sys_off:"Đang tắt", sys_pwd_set:"Đã bật", sys_pwd_unset:"Chưa đặt",
    sys_pwd_new_ph:"Mật khẩu mới", sys_pwd_old_ph:"Mật khẩu hiện tại",
    sys_pwd_save:"Đặt mật khẩu", sys_pwd_clear:"Bỏ mật khẩu",
    sys_tpl_reset:"Xóa mốc", sys_refresh:"Làm mới",
    sys_diag:"Tạo gói chẩn đoán", sys_diag_dl:"Tải gói chẩn đoán", sys_update:"Kiểm tra và cập nhật", sys_update_restarting:"Đang khởi động lại để áp dụng bản mới…",
    sys_addr:"Địa chỉ LAN: ", sys_autocmd:"Lệnh khởi động: ",
    tbl_search_ph:"Tìm cột đích / cột nguồn…", tbl_group_all:"Mọi cách điền", tbl_shown:"Hiện %d / %d dòng",
    tbl_drag_hint:"Kéo ⠿ bên trái để đổi thứ tự (thứ tự = thứ tự cột đầu ra)",
    auth_title:"Cần mật khẩu", auth_hint:"Công cụ đã bật bảo vệ bằng mật khẩu. Hãy nhập mật khẩu để tiếp tục.",
    auth_ok:"Tiếp tục", auth_bad:"Mật khẩu không đúng", auth_pwd_ph:"Nhập mật khẩu", need_auth:"Cần mật khẩu",
    wc_title:"Chào mừng dùng công cụ chuyển đổi hồ sơ vật tư MES",
    wc_desc:"Lần chạy đầu tiên — dành 10 giây để biết ba điều sau.",
    wc_lang:"Ngôn ngữ giao diện",
    wc_tip1:"Tệp kết quả được lưu vào thư mục out trong thư mục chương trình",
    wc_tip2:"Hãy đặt chương trình ở thư mục ghi được, đừng đặt trong Program Files (cần quyền quản trị)",
    wc_tip3:"Chương trình thường trú ở khay hệ thống; đóng thẻ trình duyệt không thoát chương trình — dùng menu chuột phải ở khay để thoát hẳn",
    wc_ok:"Bắt đầu",
  },
  en:{
    src_drop_hint:"You can also drag files here; select multiple files and they are merged by column name",
    ord_drop_hint_multi:"You can also drag files here; multiple files can be selected at once",
    src_multi:"%d files selected — merged into a single result", src_more:"… and %d more",
    donut_title:"Result breakdown", donut_ok:"Clean rows", donut_warn:"Warnings", donut_err:"Errors",
    donut_rows:"%d rows in total", donut_none:"No issues",
    stats_title:"Conversion statistics", stats_runs:"Conversions", stats_rows:"Total rows",
    stats_clean:"Clean runs", stats_with_issues:"Runs with issues", stats_issues:"Issues",
    stats_warns:"Warnings", stats_mat:"Materials", stats_ord:"Orders / work orders",
    stats_none:"No conversions recorded yet", stats_bars:"By day (last %d days)",
    diff_title:"Confirm config changes", diff_hint:"These are the changes this save will make. Cancel writes nothing.",
    diff_none:"No changes detected", diff_ok:"Save", diff_cancel:"Cancel",
    diff_add:"Added", diff_del:"Removed", diff_chg:"Changed", diff_col:"row %d",
    dg_fields:"Field mapping", dg_dicts:"Dictionaries", dg_unit_map:"Unit table", dg_value_whitelist:"Allowed values",
    dg_output_sheet:"Output sheet name", dg_field_mapping:"Field mapping", dg_default_values:"Default values",
    dg_column_order:"Output column order", dg_required_columns:"Required columns",
    dg_column_aliases:"Header aliases", dg_date_columns:"Date columns", dg_template_headers:"Template headers",
    dg_tag_materials:"Tagging rule", dg_settings:"Run options", dg_duplicate_keys:"Duplicate-check keys",
    cfg_tab_sys:"System",
    sys_desc:"How the program itself runs: who can access it, whether it starts with Windows, whether the MES template changed, and how to collect evidence when something breaks. Unrelated to field mapping; changes affecting listening need a restart.",
    sys_block_access:"Access & security",
    sys_block_access_hint:"By default it listens on 127.0.0.1 only, so colleagues cannot reach it. After enabling LAN access, set a password too.",
    sys_lan_title:"LAN access",
    sys_lan_desc:"When on, colleagues on the same LAN can open this tool in a browser (restart required).",
    sys_lan_sw:"Allow LAN access",
    sys_pwd_title:"Access password",
    sys_pwd_desc:"Once set, every endpoint except the health check requires it. Only a salted hash is stored.",
    sys_auto_title:"Launch at startup",
    sys_auto_desc:"Runs quietly in the tray after you sign in to Windows. Writes only the current user's startup entry; no admin rights needed.",
    sys_auto_sw:"Run at Windows startup",
    sys_block_tpl:"Material template baseline",
    sys_block_tpl_hint:"The first conversion records the MES template header; later changes (added / removed / reordered columns) will be flagged.",
    sys_block_diag:"Diagnostics & updates",
    sys_block_diag_hint:"The app auto-checks GitHub for new versions on startup and updates in the background (applied on next launch); or click the button to update now. The diagnostic bundle packages logs + config + environment (password redacted) to send to whoever maintains it.",
    sys_save:"Save", sys_on:"On", sys_off:"Off", sys_pwd_set:"Enabled", sys_pwd_unset:"Not set",
    sys_pwd_new_ph:"New password", sys_pwd_old_ph:"Current password",
    sys_pwd_save:"Set password", sys_pwd_clear:"Remove password",
    sys_tpl_reset:"Clear baseline", sys_refresh:"Refresh",
    sys_diag:"Build bundle", sys_diag_dl:"Download bundle", sys_update:"Check & update", sys_update_restarting:"Restarting to apply the new version…",
    sys_addr:"LAN address: ", sys_autocmd:"Command: ",
    tbl_search_ph:"Search target / source column…", tbl_group_all:"All fill methods", tbl_shown:"Showing %d / %d rows",
    tbl_drag_hint:"Drag ⠿ on the left to reorder (order = output column order)",
    auth_title:"Password required", auth_hint:"This tool is password-protected. Enter the password to continue.",
    auth_ok:"Continue", auth_bad:"Wrong password", auth_pwd_ph:"Enter password", need_auth:"A password is required",
    wc_title:"Welcome to MES Material Master Converter",
    wc_desc:"First run — 10 seconds on the three things worth knowing.",
    wc_lang:"Interface language",
    wc_tip1:"Output files are saved to the out folder next to the program",
    wc_tip2:"Keep the program in a writable folder — not Program Files, which needs administrator rights",
    wc_tip3:"The program stays in the system tray; closing the browser tab does not quit it — use the tray menu to exit",
    wc_ok:"Get started",
  }
};
Object.keys(STR2).forEach(l=>{ STR[l]=Object.assign(STR[l]||{}, STR2[l]); });

// 新增的静态文案挂到既有 TEXT_MAP 上（id -> 文案 key）
Object.assign(TEXT_MAP, {
  step_source_desc:"step_source_desc",
  srcDropHint:"src_drop_hint", ordSrcDropHint:"ord_drop_hint_multi", ordWorkDropHint:"ord_drop_hint_multi",
  ord_step_source_desc:"ord_step_source_desc", ord_step_work_desc:"ord_step_work_desc",
  cfgTabSys:"cfg_tab_sys",
  sys_desc:"sys_desc", sys_block_access:"sys_block_access", sys_block_access_hint:"sys_block_access_hint",
  sys_lan_title:"sys_lan_title", sys_lan_desc:"sys_lan_desc", sys_lan_sw:"sys_lan_sw",
  sys_pwd_title:"sys_pwd_title", sys_pwd_desc:"sys_pwd_desc",
  sys_auto_title:"sys_auto_title", sys_auto_desc:"sys_auto_desc", sys_auto_sw:"sys_auto_sw",
  sys_block_tpl:"sys_block_tpl", sys_block_tpl_hint:"sys_block_tpl_hint",
  sys_block_diag:"sys_block_diag", sys_block_diag_hint:"sys_block_diag_hint",
  sysLanSave:"sys_save", sysPwdSave:"sys_pwd_save", sysPwdClear:"sys_pwd_clear", sysAutoSave:"sys_save",
  sysTplReset:"sys_tpl_reset", sysTplRefresh:"sys_refresh",
  sysDiagBtn:"sys_diag", sysDiagLink:"sys_diag_dl", sysUpdateBtn:"sys_update",
  diffTitle:"diff_title", diffHint:"diff_hint", diffOk:"diff_ok", diffClose:"diff_cancel", diffCancel:"diff_cancel",
  authTitle:"auth_title", authHint:"auth_hint", authOk:"auth_ok",
  stats_title:"stats_title", refreshStatsBtn:"refresh",
});

// 需要写进 placeholder 的文案（applyLang 里统一刷新）
const PH_MAP = { sysPwdNew:"sys_pwd_new_ph", sysPwdOld:"sys_pwd_old_ph", authPwd:"auth_pwd_ph", fldSearch:"tbl_search_ph" };
const _applyLangBase = applyLang;
applyLang = function(){
  _applyLangBase();
  Object.keys(PH_MAP).forEach(id=>{ const el=document.getElementById(id); if(el) el.placeholder=t(PH_MAP[id]); });
  if(lastResult) renderDonut("donutWrap", lastResult.data);
  if(ordLastResult) renderDonut("ordDonutWrap", ordLastResult.data);
  if(typeof currentModule!=="undefined" && currentModule==="config" && cfgTab==="sys") loadSysInfo();
};

// ---------------------------------------------------------------- 口令锁
// token 放 sessionStorage：刷新页面不用重输，关掉标签页即失效。
try{ window.__authToken = sessionStorage.getItem("mesToken")||""; }catch(e){ window.__authToken=""; }

const _fetchBase = window.fetch.bind(window);
window.fetch = function(input, init){
  init = init || {};
  if(window.__authToken){
    init.headers = Object.assign({}, init.headers||{}, {"X-Token": window.__authToken});
  }
  return _fetchBase(input, init).then(res=>{
    try{
      const ct = res.headers.get("content-type")||"";
      if(ct.indexOf("application/json")>=0){
        res.clone().json().then(d=>{ if(d && d.need_auth) requestAuth(); }).catch(()=>{});
      }
    }catch(e){}
    return res;
  });
};
// /download 是浏览器直链（<a download>），带不了自定义请求头。
// 不再把长期 token 拼到 URL（会进历史/日志/Referer）；改为点击时先换一个
// 一次性下载令牌 dt（60 秒、用一次即废），再拼到 href 上触发下载。
document.addEventListener("click",e=>{
  const a = e.target.closest && e.target.closest('a[href^="/download"]');
  if(!a || !window.__authToken) return;
  const raw = a.getAttribute("href")||"";
  if(raw.indexOf("dt=")>=0) return;           // 已换过令牌，交给浏览器直接下载
  if(a.dataset.dtBusy==="1") return;
  e.preventDefault();
  a.dataset.dtBusy="1";
  fetch("/api/download-token",{method:"POST"})
    .then(r=>r.json())
    .then(d=>{
      a.dataset.dtBusy="0";
      if(!d || !d.ok || !d.dt) return;       // 失败就静默放弃，避免错误跳转
      const sep = raw.indexOf("?")<0?"?":"&";
      a.setAttribute("href", raw+sep+"dt="+encodeURIComponent(d.dt));
      a.click();                              // 重新触发一次，这次带 dt
    })
    .catch(()=>{ a.dataset.dtBusy="0"; });
}, true);

let _authPending=false;
function requestAuth(){
  if(_authPending) return;
  _authPending=true;
  showModal("authModal");
  const inp=document.getElementById("authPwd");
  if(inp){ inp.value=""; inp.focus(); }
}
document.getElementById("authOk").addEventListener("click",doAuth);
document.getElementById("authPwd").addEventListener("keydown",e=>{ if(e.key==="Enter") doAuth(); });
async function doAuth(){
  const inp=document.getElementById("authPwd");
  const msg=document.getElementById("authMsg");
  const pwd=(inp&&inp.value)||"";
  if(!pwd){ msg.className="adv-msg err"; msg.textContent=t("auth_pwd_ph"); return; }
  try{
    const fd=new FormData(); fd.append("password",pwd); fd.append("lang",currentLang);
    const r=await _fetchBase("/api/auth",{method:"POST",body:fd});
    const d=await r.json();
    if(d.ok && d.token){
      window.__authToken=d.token;
      try{ sessionStorage.setItem("mesToken",d.token); }catch(e){}
      hideModal("authModal"); _authPending=false;
      location.reload();   // 重新走一遍初始化，配置/历史都带着 token 重新拉
      return;
    }
    msg.className="adv-msg err"; msg.textContent=d.error||t("auth_bad");
  }catch(e){ msg.className="adv-msg err"; msg.textContent=fetchErrText(e); }
}

// ---------------------------------------------------------------- 多文件选择
function renderFileSet(nameId, listId, files){
  const nameEl=document.getElementById(nameId), listEl=document.getElementById(listId);
  if(!files || !files.length){
    nameEl.textContent=t("no_file"); nameEl.dataset.chosen="0";
    if(listEl) listEl.innerHTML="";
    return;
  }
  const head = files.length===1 ? t("file_chosen")+files[0].name : fmt("src_multi",[files.length]);
  nameEl.innerHTML=esc(head)
    +'<button type="button" class="fn-clear" data-clear="1" title="'+esc(t("clear_file"))+'" aria-label="'+esc(t("clear_file"))+'">&times;</button>';
  nameEl.dataset.chosen="1";
  if(!listEl) return;
  const shown=files.slice(0,8).map((f,i)=>'<div class="fl-item"><span class="fl-n">'+(i+1)+'.</span>'+esc(f.name)+'</div>').join("");
  listEl.innerHTML = shown + (files.length>8 ? '<div class="fl-item">'+esc(fmt("src_more",[files.length-8]))+'</div>' : "");
}
function bindMultiFile(inputId,nameId,listId,cardId,onPick,onClear){
  const input=document.getElementById(inputId);
  const card=document.getElementById(cardId);
  let files=[];
  const apply=()=>{ renderFileSet(nameId,listId,files); if(onPick) onPick(files); };
  input.addEventListener("change",e=>{ files=Array.from(e.target.files||[]); apply(); });
  document.getElementById(nameId).addEventListener("click",e=>{
    if(!e.target.closest("[data-clear]")) return;
    e.preventDefault(); input.value=""; files=[]; apply(); if(onClear) onClear();
  });
  ["dragenter","dragover"].forEach(ev=>card.addEventListener(ev,e=>{ e.preventDefault(); card.classList.add("drag"); }));
  ["dragleave","drop"].forEach(ev=>card.addEventListener(ev,e=>{ e.preventDefault(); card.classList.remove("drag"); }));
  card.addEventListener("drop",e=>{
    const fl=e.dataTransfer && e.dataTransfer.files;
    if(!fl || !fl.length) return;
    files=Array.from(fl);
    try{ const dt=new DataTransfer(); files.forEach(f=>dt.items.add(f)); input.files=dt.files; }catch(err){}
    apply();
  });
  return { get:()=>files, set:fs=>{ files=fs; apply(); } };
}
const srcPicker = bindMultiFile("srcFile","srcName","srcList","srcCard",
  fs=>{ if(fs.length) loadSourceHeaders(fs[0]); else { srcHeaders=[]; renderDatalist("srcCols",[],false); } },
  ()=>{ srcHeaders=[]; renderDatalist("srcCols",[],false); });
const ordSrcPicker = bindMultiFile("ordSrcFile","ordSrcName","ordSrcList","ordSrcCard", null, null);
const ordWorkPicker = bindMultiFile("ordWorkFile","ordWorkName","ordWorkList","ordWorkCard", null, null);

// ---------------------------------------------------------------- 结果小图表
function donutSVG(parts, size){
  size = size||134;
  const r=size/2-14, cx=size/2, c=2*Math.PI*r;
  const total=parts.reduce((s,p)=>s+p.n,0);
  let off=0;
  const segs=parts.map(p=>{
    const frac = total>0 ? p.n/total : 0;
    const len = c*frac;
    const seg='<circle cx="'+cx+'" cy="'+cx+'" r="'+r+'" fill="none" stroke="'+p.color+'" stroke-width="16"'
      +' stroke-dasharray="'+len.toFixed(2)+' '+(c-len).toFixed(2)+'" stroke-dashoffset="'+(-off).toFixed(2)+'"'
      +' transform="rotate(-90 '+cx+' '+cx+')"></circle>';
    off += len;
    return seg;
  }).join("");
  const ring='<circle cx="'+cx+'" cy="'+cx+'" r="'+r+'" fill="none" stroke="#e5e7eb" stroke-width="16"></circle>';
  const label='<text x="50%" y="47%" text-anchor="middle" font-size="21" font-weight="700" fill="#1f2937">'+total+'</text>'
    +'<text x="50%" y="64%" text-anchor="middle" font-size="11" fill="#6b7280">'+esc(t("donut_rows").replace("%d","").trim()||"rows")+'</text>';
  return '<svg width="'+size+'" height="'+size+'" viewBox="0 0 '+size+' '+size+'" role="img" aria-label="'+esc(t("donut_title"))+'">'
    +ring+segs+label+'</svg>';
}
// 占比按「行」算：问题明细里带行号，能数出有多少行被点名；超出行数时按行数封顶
function renderDonut(wrapId, data){
  const wrap=document.getElementById(wrapId);
  if(!wrap || !data) return;
  const rows=data.rows||0;
  const issues=data.issues||[];
  let e=0,w=0;
  const rowSet=new Set();
  issues.forEach(it=>{ if(it.level==="error") e++; else w++; if(it.row) rowSet.add(it.row); });
  let errRows=0, warnRows=0;
  rowSet.forEach(rn=>{
    const has=issues.filter(it=>it.row===rn);
    if(has.some(it=>it.level==="error")) errRows++; else warnRows++;
  });
  // 明细被截断（issue_total 大于明细条数）时，按比例把剩余摊到两类里，避免「看着很少其实很多」
  const totalIssue = data.issue_total||0;
  if(totalIssue > issues.length && issues.length>0){
    const k=totalIssue/issues.length;
    errRows=Math.round(errRows*k); warnRows=Math.round(warnRows*k);
  }
  const used=Math.min(rows, errRows+warnRows);
  const clean=Math.max(0, rows-used);
  const parts=[
    {n:clean, color:"#10b981", label:t("donut_ok"), v:clean},
    {n:Math.max(0,warnRows), color:"#f59e0b", label:t("donut_warn"), v:warnRows},
    {n:Math.max(0,errRows), color:"#ef4444", label:t("donut_err"), v:errRows},
  ];
  const active=parts.filter(p=>p.n>0);
  const legend = parts.map(p=>'<div><i style="background:'+p.color+'"></i>'+esc(p.label)+'：'+p.v+'</div>').join("");
  wrap.innerHTML =
    '<div><div style="font-size:12px;color:var(--muted);margin-bottom:6px">'+esc(t("donut_title"))+'</div>'
    + donutSVG(active.length?active:[{n:1,color:"#e5e7eb"}])
    + '</div><div class="donut-legend">'+legend+'</div>'
    + (active.length===0 ? '<div class="donut-legend" style="color:var(--ok)">✓ '+esc(t("donut_none"))+'</div>' : '');
}
const _renderStatsBase=renderStats;
renderStats=function(data){ _renderStatsBase(data); renderDonut("donutWrap", data); };
const _ordRenderStatsBase=ordRenderStats;
ordRenderStats=function(data){ _ordRenderStatsBase(data); renderDonut("ordDonutWrap", data); };

// ---------------------------------------------------------------- 配置变更确认
function diffGroupLabel(k){
  const key="dg_"+k;
  const got=STR[currentLang] && STR[currentLang][key];
  return got || (STR.zh && STR.zh[key]) || k;
}
function pushDiff(items, group, kind, text){ items.push({group:group, kind:kind, text:text}); }
function brief(v){ const s=typeof v==="string"?v:JSON.stringify(v); return s==null?"":(s.length>90?s.slice(0,90)+"…":s); }

// 一对「名字 -> 内容」的表（字典表 / 单位表 / 允许值清单）
function diffNameMap(group, a, b, items, descFn){
  a=a||{}; b=b||{};
  Object.keys(b).filter(k=>!(k in a)).sort().forEach(k=>pushDiff(items,group,"add",t("diff_add")+" "+k+(descFn?" · "+descFn(b[k]):"")));
  Object.keys(a).filter(k=>!(k in b)).sort().forEach(k=>pushDiff(items,group,"del",t("diff_del")+" "+k+(descFn?" · "+descFn(a[k]):"")));
  Object.keys(a).filter(k=>k in b).sort().forEach(k=>{
    const sa=JSON.stringify(a[k]), sb=JSON.stringify(b[k]);
    if(sa!==sb) pushDiff(items,group,"chg",t("diff_chg")+" "+k+" · "+brief(sa)+" → "+brief(sb));
  });
}
function opDiffText(f){
  const bits=[];
  if((f.source||"")!==undefined) bits.push((f.source||"-"));
  bits.push(f.op||"copy");
  return bits.join(" / ");
}
// 字段映射：按目标列名对齐（而不是按行号），这样「插了一行」不会被误报成一堆修改
function diffFields(group, a, b, items, targetKey, srcKey, kindFn){
  a=a||[]; b=b||[];
  const ka={}, kb={};
  a.forEach((f,i)=>{ const k=(f[targetKey]||"").trim()||fmt("diff_col",[i+1]); ka[k]=f; });
  b.forEach((f,i)=>{ const k=(f[targetKey]||"").trim()||fmt("diff_col",[i+1]); kb[k]=f; });
  Object.keys(kb).filter(k=>!(k in ka)).sort().forEach(k=>pushDiff(items,group,"add",t("diff_add")+" "+k+(kindFn?" · "+kindFn(kb[k]):"")));
  Object.keys(ka).filter(k=>!(k in kb)).sort().forEach(k=>pushDiff(items,group,"del",t("diff_del")+" "+k+(kindFn?" · "+kindFn(ka[k]):"")));
  Object.keys(ka).filter(k=>k in kb).sort().forEach(k=>{
    if(JSON.stringify(ka[k])!==JSON.stringify(kb[k]))
      pushDiff(items,group,"chg",t("diff_chg")+" "+k+" · "+brief(kindFn?kindFn(ka[k]):ka[k])+" → "+brief(kindFn?kindFn(kb[k]):kb[k]));
  });
}
function diffMatConfig(a, b){
  const items=[];
  a=a||{}; b=b||{};
  const oa=a.output_sheet||"", ob=b.output_sheet||"";
  if(oa!==ob) pushDiff(items,"output_sheet", ob?"chg":"del", t("diff_chg")+" "+(oa||"-")+" → "+(ob||"-"));
  diffFields("fields", a.fields, b.fields, items, "target", "source", opDiffText);
  diffNameMap("dicts", a.dicts, b.dicts, items, v=>Object.keys(v||{}).length+" 项");
  diffNameMap("unit_map", a.unit_map, b.unit_map, items, v=>brief(v));
  diffNameMap("value_whitelist", a.value_whitelist, b.value_whitelist, items, v=>brief((v||[]).join(", ")));
  return items;
}
function diffOrdConfig(a, b){
  const items=[];
  a=a||{}; b=b||{};
  const cats=new Set(Object.keys(a.field_mapping||{}).concat(Object.keys(b.field_mapping||{})));
  cats.forEach(cat=>{
    const fa=((a.field_mapping||{})[cat])||{}, fb=((b.field_mapping||{})[cat])||{};
    Object.keys(fb).filter(k=>!(k in fa)).forEach(k=>pushDiff(items,"field_mapping","add",t("diff_add")+" ["+cat+"] "+k+" → "+fb[k]));
    Object.keys(fa).filter(k=>!(k in fb)).forEach(k=>pushDiff(items,"field_mapping","del",t("diff_del")+" ["+cat+"] "+k+" → "+fa[k]));
    Object.keys(fa).filter(k=>k in fb).forEach(k=>{ if(fa[k]!==fb[k]) pushDiff(items,"field_mapping","chg",t("diff_chg")+" ["+cat+"] "+k+" · "+fa[k]+" → "+fb[k]); });
  });
  ["default_values","column_order","required_columns","date_columns"].forEach(k=>diffNameMap(k, a[k], b[k], items, v=>brief(v)));
  diffNameMap("column_aliases", a.column_aliases, b.column_aliases, items, v=>brief((v||[]).join(", ")));
  diffNameMap("template_headers", a.template_headers, b.template_headers, items, v=>brief(v));
  const ta=JSON.stringify(a.tag_materials||{}), tb=JSON.stringify(b.tag_materials||{});
  if(ta!==tb) pushDiff(items,"tag_materials","chg",t("diff_chg")+" "+brief(ta)+" → "+brief(tb));
  const sa=JSON.stringify(a.settings||{}), sb=JSON.stringify(b.settings||{});
  if(sa!==sb) pushDiff(items,"settings","chg",t("diff_chg")+" "+brief(sa)+" → "+brief(sb));
  return items;
}
function groupDiff(items){
  const order=[], map={};
  items.forEach(it=>{
    if(!map[it.group]){ map[it.group]={label:diffGroupLabel(it.group), items:[]}; order.push(it.group); }
    map[it.group].items.push(it);
  });
  return order.map(g=>map[g]);
}
let _diffOnOk=null;
function showDiffModal(items, onOk){
  const body=document.getElementById("diffBody");
  const groups=groupDiff(items);
  body.innerHTML = groups.length
    ? groups.map(g=>'<div class="dg">'+esc(g.label)+'</div>'+g.items.map(it=>'<div class="di '+it.kind+'">'+esc(it.text)+'</div>').join("")).join("")
    : '<div class="diff-empty">'+esc(t("diff_none"))+'</div>';
  _diffOnOk=onOk||null;
  showModal("diffModal");
}
function closeDiff(){ _diffOnOk=null; hideModal("diffModal"); }
document.getElementById("diffOk").addEventListener("click",()=>{ const f=_diffOnOk; closeDiff(); if(f) f(); });
document.getElementById("diffCancel").addEventListener("click",closeDiff);
document.getElementById("diffClose").addEventListener("click",closeDiff);

// 在「保存配置」上做一层拦截：先把表单同步进配置对象，算出 diff 给用户看，确认后才放行原本的保存逻辑。
// 用捕获阶段拦，靠 stopImmediatePropagation 挡住后面那个 bubble 监听器；放行时置标志重新 click 一次。
const _diffBypass={mat:false, ord:false};
document.addEventListener("click",e=>{
  const btn = e.target.closest && e.target.closest("#saveConfigBtn,#ordCfgSaveBtn");
  if(!btn) return;
  const which = btn.id==="saveConfigBtn" ? "mat" : "ord";
  if(_diffBypass[which]){ _diffBypass[which]=false; return; }
  if(window.__cfgFail && which==="mat") return;   // 配置没读到就别拦，交给原有逻辑报错
  if(window.__ordFail && which==="ord") return;
  let before, after, items;
  try{
    if(which==="mat"){
      syncAdvToCfg();
      before = window.__cfgSnapMat || clone(cfg);
      after = clone(cfg);
      items = diffMatConfig(before, after);
    } else {
      const c=collectOrdCfg();
      before = window.__ordCfgSnap || clone(ordCfg||{});
      after = clone(c);
      items = diffOrdConfig(before, after);
    }
  }catch(err){ return; }   // 同步失败就让原逻辑自己处理
  if(!items.length) return;  // 没改动不用打扰
  e.preventDefault();
  e.stopImmediatePropagation();
  showDiffModal(items, ()=>{ _diffBypass[which]=true; btn.click(); });
}, true);

// 配置快照：每次成功载入后记一份，供上面算 diff
const _loadAdvFromCfgBase=loadAdvFromCfg;
loadAdvFromCfg=function(){ _loadAdvFromCfgBase(); try{ window.__cfgSnapMat=clone(cfg); }catch(e){} };
const _loadOrdersCfgBase=loadOrdersCfg;
loadOrdersCfg=function(){ return Promise.resolve(_loadOrdersCfgBase()).then(r=>{ try{ window.__ordCfgSnap=clone(ordCfg||{}); }catch(e){} return r; }); };

// ---------------------------------------------------------------- 字段映射：搜索 / 筛选 / 拖拽排序
(function fieldTableTools(){
  const tbl=document.getElementById("fieldTable");
  if(!tbl) return;
  const bar=document.createElement("div");
  bar.className="tbl-tools";
  bar.innerHTML='<input type="search" id="fldSearch">'
    +'<select id="fldGroup"></select>'
    +'<span class="sys-tpl" id="fldCount"></span>'
    +'<span class="sys-tpl">'+esc(t("tbl_drag_hint"))+'</span>';
  tbl.parentNode.insertBefore(bar, tbl);
  const sel=bar.querySelector("#fldGroup");
  sel.innerHTML='<option value="">'+esc(t("tbl_group_all"))+'</option>'
    + OPS.map(o=>'<option value="'+o+'">'+esc(opLabel(o))+'</option>').join("");
  bar.querySelector("#fldSearch").addEventListener("input",applyFieldFilter);
  sel.addEventListener("change",applyFieldFilter);
})();
function applyFieldFilter(){
  const tbl=document.getElementById("fieldTable");
  if(!tbl) return;
  const q=((document.getElementById("fldSearch")||{}).value||"").trim().toLowerCase();
  const op=(document.getElementById("fldGroup")||{}).value||"";
  const rows=Array.from(tbl.querySelectorAll("tbody tr"));
  let shown=0;
  rows.forEach(tr=>{
    const idx=+tr.dataset.idx;
    const f=advFields[idx]||{};
    const hay=((f.target||"")+" "+(f.source||"")+" "+opLabel(f.op||"")).toLowerCase();
    const okQ = !q || hay.indexOf(q)>=0;
    const okO = !op || f.op===op;
    const ok = okQ && okO;
    tr.style.display = ok ? "" : "none";
    if(ok) shown++;
  });
  const c=document.getElementById("fldCount");
  if(c) c.textContent=fmt("tbl_shown",[shown,rows.length]);
}
const _renderTableBase=renderTable;
renderTable=function(){
  _renderTableBase();
  const tbl=document.getElementById("fieldTable");
  if(!tbl) return;
  // 表头补一列（拖拽把手）
  const htr=tbl.querySelector("thead tr");
  if(htr && !htr.querySelector(".drag-th")){
    const th=document.createElement("th"); th.className="drag-th"; th.style.width="34px";
    th.setAttribute("aria-hidden","true"); th.textContent="";
    htr.insertBefore(th, htr.firstChild);
  }
  const rows=Array.from(tbl.querySelectorAll("tbody tr"));
  rows.forEach((tr,i)=>{
    tr.dataset.idx=i;
    if(!tr.querySelector(".drag-td")){
      const td=document.createElement("td"); td.className="drag-td";
      td.innerHTML='<span class="drag-handle" title="'+esc(t("tbl_drag_hint"))+'" aria-hidden="true">⠿</span>';
      tr.insertBefore(td, tr.firstChild);
    }
  });
  applyFieldFilter();
};
(function dragSort(){
  const tbl=document.getElementById("fieldTable");
  if(!tbl) return;
  const tbody=tbl.querySelector("tbody");
  let from=null;
  tbody.addEventListener("mousedown",e=>{
    const h=e.target.closest && e.target.closest(".drag-handle");
    if(h && h.closest("tr")) h.closest("tr").setAttribute("draggable","true");
  });
  document.addEventListener("mouseup",()=>{
    tbody.querySelectorAll('tr[draggable="true"]').forEach(x=>x.removeAttribute("draggable"));
  });
  tbody.addEventListener("dragstart",e=>{
    const tr=e.target.closest("tr"); if(!tr) return;
    from=+tr.dataset.idx; tr.classList.add("dragging");
    try{ e.dataTransfer.effectAllowed="move"; e.dataTransfer.setData("text/plain",String(from)); }catch(err){}
  });
  tbody.addEventListener("dragover",e=>{
    const tr=e.target.closest("tr"); if(!tr || from==null) return;
    e.preventDefault();
    tbody.querySelectorAll("tr.drop-target").forEach(x=>x.classList.remove("drop-target"));
    tr.classList.add("drop-target");
  });
  tbody.addEventListener("drop",e=>{
    const tr=e.target.closest("tr"); if(!tr || from==null) return;
    e.preventDefault();
    const to=+tr.dataset.idx;
    if(!isNaN(to) && to!==from && advFields[from]){
      const [it]=advFields.splice(from,1);
      advFields.splice(to,0,it);
      window.__dirty.mat=true;
      renderTable();
    }
    from=null;
  });
  tbody.addEventListener("dragend",()=>{
    from=null;
    tbody.querySelectorAll("tr").forEach(x=>x.classList.remove("dragging","drop-target"));
  });
})();

// ---------------------------------------------------------------- 系统页签
let sysInfo=null;
function sysPill(id,on,onText,offText){
  const el=document.getElementById(id); if(!el) return;
  el.className="pill "+(on?"on":"off");
  el.textContent=on?onText:offText;
}
async function loadTplStatus(){
  const el=document.getElementById("sysTplState"); if(!el) return;
  try{
    const r=await fetch("/api/tpl/status?lang="+encodeURIComponent(currentLang));
    const d=await r.json();
    el.textContent=d.message||"";
    el.style.color = d.has ? "var(--primary-d)" : "var(--muted)";
  }catch(e){ el.textContent=""; }
}
async function loadSysInfo(){
  try{
    const r=await fetch("/api/system?lang="+encodeURIComponent(currentLang));
    const d=await r.json();
    if(!d.ok) return;
    sysInfo=d;
    const lanChk=document.getElementById("sysLanChk"), autoChk=document.getElementById("sysAutoChk");
    if(lanChk) lanChk.checked=!!d.lan;
    if(autoChk) autoChk.checked=!!d.autostart;
    sysPill("sysLanPill", !!d.lan, t("sys_on"), t("sys_off"));
    sysPill("sysAutoPill", !!d.autostart, t("sys_on"), t("sys_off"));
    sysPill("sysPwdPill", !!d.has_password, t("sys_pwd_set"), t("sys_pwd_unset"));
    const addrs=document.getElementById("sysAddrs");
    if(addrs) addrs.textContent = (d.addrs&&d.addrs.length) ? (d.lan? t("sys_addr")+d.addrs.join("  ") : d.addrs.join("  ")) : t("sys_off");
    const cmd=document.getElementById("sysAutoCmd");
    if(cmd) cmd.textContent = d.autostart_cmd ? t("sys_autocmd")+d.autostart_cmd : "";
    const old=document.getElementById("sysPwdOld");
    if(old) old.style.display = d.has_password ? "" : "none";
    const clearBtn=document.getElementById("sysPwdClear");
    if(clearBtn) clearBtn.disabled = !d.has_password;
    const st=document.getElementById("sysUpdateState");
    if(st) st.textContent = t("about_version")+" "+d.update_current;
  }catch(e){}
  loadTplStatus();
}
function sysMsg(text,ok){
  const el=document.getElementById("sysMsg");
  el.className="adv-msg "+(ok?"ok":"err");
  el.textContent=text||"";
}
async function sysPost(path, fields){
  const fd=new FormData();
  Object.keys(fields||{}).forEach(k=>fd.append(k,fields[k]));
  fd.append("lang",currentLang);
  const r=await fetch(path,{method:"POST",body:fd});
  return r.json();
}
const sysLanSave=document.getElementById("sysLanSave");
if(sysLanSave) sysLanSave.addEventListener("click",async()=>{
  try{
    const on=document.getElementById("sysLanChk").checked?"1":"0";
    const d=await sysPost("/api/system/lan",{on:on});
    sysMsg(d.ok?d.message:(d.error||""), d.ok);
    loadSysInfo();
  }catch(e){ sysMsg(fetchErrText(e),false); }
});
const sysAutoSave=document.getElementById("sysAutoSave");
if(sysAutoSave) sysAutoSave.addEventListener("click",async()=>{
  try{
    const on=document.getElementById("sysAutoChk").checked?"1":"0";
    const d=await sysPost("/api/system/autostart",{on:on});
    sysMsg(d.ok?d.message:(d.error||""), d.ok);
    loadSysInfo();
  }catch(e){ sysMsg(fetchErrText(e),false); }
});
const sysPwdSave=document.getElementById("sysPwdSave");
if(sysPwdSave) sysPwdSave.addEventListener("click",async()=>{
  try{
    const pwd=(document.getElementById("sysPwdNew").value||"");
    const old=(document.getElementById("sysPwdOld").value||"");
    const d=await sysPost("/api/system/password",{password:pwd, old_password:old});
    sysMsg(d.ok?d.message:(d.error||""), d.ok);
    if(d.ok && d.token){ window.__authToken=d.token; try{ sessionStorage.setItem("mesToken",d.token); }catch(e){} }
    if(d.ok){ document.getElementById("sysPwdNew").value=""; document.getElementById("sysPwdOld").value=""; }
    loadSysInfo();
  }catch(e){ sysMsg(fetchErrText(e),false); }
});
const sysPwdClear=document.getElementById("sysPwdClear");
if(sysPwdClear) sysPwdClear.addEventListener("click",async()=>{
  if(!confirm(t("sys_pwd_clear")+"?")) return;
  try{
    const old=(document.getElementById("sysPwdOld").value||"");
    const d=await sysPost("/api/system/password",{clear:"1", old_password:old});
    sysMsg(d.ok?d.message:(d.error||""), d.ok);
    loadSysInfo();
  }catch(e){ sysMsg(fetchErrText(e),false); }
});
const sysTplReset=document.getElementById("sysTplReset");
if(sysTplReset) sysTplReset.addEventListener("click",async()=>{
  try{ const d=await sysPost("/api/tpl/reset",{}); sysMsg(d.message||d.error, d.ok); loadTplStatus(); }
  catch(e){ sysMsg(fetchErrText(e),false); }
});
const sysTplRefresh=document.getElementById("sysTplRefresh");
if(sysTplRefresh) sysTplRefresh.addEventListener("click",loadTplStatus);
const sysDiagBtn=document.getElementById("sysDiagBtn");
if(sysDiagBtn) sysDiagBtn.addEventListener("click",async()=>{
  sysMsg(t("sys_diag")+"…",true);
  try{
    const d=await sysPost("/api/system/diag",{});
    sysMsg(d.message||d.error, d.ok);
    const a=document.getElementById("sysDiagLink");
    if(d.ok && a){ a.style.display=""; a.href="/download?file="+encodeURIComponent(d.file); }
  }catch(e){ sysMsg(fetchErrText(e),false); }
});
const sysUpdateBtn=document.getElementById("sysUpdateBtn");
if(sysUpdateBtn) sysUpdateBtn.addEventListener("click",async()=>{
  const st=document.getElementById("sysUpdateState");
  if(st) st.textContent=t("sys_update")+"…";
  try{
    const r=await fetch("/api/system/update/apply",{method:"POST"});
    const d=await r.json();
    if(st) st.textContent=d.message||d.error||"";
    // 后端会在约 0.6s 后自行重启以生效新版本；这里给个过渡提示
    if(d.ok && d.restart){ setTimeout(()=>{ if(st) st.textContent=t("sys_update_restarting"); }, 1000); }
  }catch(e){ if(st) st.textContent=fetchErrText(e); }
});

// ---------------------------------------------------------------- 转换统计面板
async function loadStats(){
  const grid=document.getElementById("statsGrid");
  const bars=document.getElementById("statsBars");
  const labels=document.getElementById("statsBarLabels");
  const sec=document.getElementById("statsSection");
  if(!grid) return;
  try{
    const r=await fetch("/api/stats");
    const d=await r.json();
    if(!d.ok) return;
    if(sec) sec.classList.add("show");   // .history 默认隐藏，拿到数据才亮出来
    if(!d.runs){
      grid.innerHTML='<div class="stat" style="grid-column:1/-1">'+esc(t("stats_none"))+'</div>';
      bars.innerHTML=""; labels.innerHTML="";
      return;
    }
    grid.innerHTML =
      statCard(t("stats_runs"), d.runs)
      + statCard(t("stats_rows"), d.rows)
      + statCard(t("stats_clean"), d.clean)
      + statCard(t("stats_with_issues"), d.with_issues)
      + statCard(t("stats_issues"), d.issue_total)
      + statCard(t("stats_warns"), d.warn_total)
      + statCard(t("stats_mat"), d.materials)
      + statCard(t("stats_ord"), d.orders);
    const by=d.by_day||[];
    const max=Math.max(1, ...by.map(x=>x.runs));
    bars.innerHTML = by.map(x=>'<div class="bar" style="height:'+Math.max(6,Math.round(x.runs/max*100))+'%" title="'+esc(x.day+" · "+x.runs+" · "+x.rows)+'"><span>'+x.runs+'</span></div>').join("");
    labels.innerHTML = by.map(x=>'<span>'+esc(x.day.slice(5))+'</span>').join("");
  }catch(e){}
}
const refreshStatsBtn=document.getElementById("refreshStatsBtn");
if(refreshStatsBtn) refreshStatsBtn.addEventListener("click",loadStats);

// 启动时先问一句「要不要口令」，需要就先弹窗；不需要再拉系统信息与统计。
(async function sysBoot(){
  try{
    const r=await _fetchBase("/api/auth/state");
    const d=await r.json();
    if(d.need_auth && !d.valid){ requestAuth(); return; }
  }catch(e){}
  loadStats();
  loadSysInfo();
})();

// ---------------------------------------------------------------- 首次运行引导
// 后端在 exe 同级写 welcomed 标记；没有标记即第一次运行，弹一次引导。
// 主要把「结果落到哪、别放 Program Files、关标签页不等于退出」这三件事说清楚。
function renderWcLangs(){
  const box=document.getElementById("wcLangs"); if(!box) return;
  const langs=[["zh","简体中文"],["zht","繁體中文"],["vi","Tiếng Việt"],["en","English"]];
  box.innerHTML="";
  langs.forEach(function(pair){
    const code=pair[0], label=pair[1];
    const b=document.createElement("button");
    b.type="button";
    b.className="wc-lang"+(currentLang===code?" on":"");
    b.textContent=label;
    b.addEventListener("click",function(){
      currentLang=code;
      localStorage.setItem("mes_lang",currentLang);
      applyLang();
      renderWcLangs();
    });
    box.appendChild(b);
  });
}

async function maybeShowWelcome(){
  const m=document.getElementById("welcomeModal"); if(!m) return;
  try{
    const r=await fetch("/api/system?lang="+encodeURIComponent(currentLang));
    const d=await r.json();
    if(!d.ok || !d.first_run) return;
  }catch(e){ return; }
  renderWcLangs();
  m.style.display="flex";
}

(function(){
  const ok=document.getElementById("wcOk");
  if(ok) ok.addEventListener("click",async function(){
    document.getElementById("welcomeModal").style.display="none";
    try{ await fetch("/api/system/welcomed",{method:"POST"}); }catch(e){}
  });
})();

// ---- 初始化 ----
(async function init(){
  // 支持 ?lang=zh|zht|vi|en 直接指定界面语言（分享链接 / 自动化验证用）
  try{
    const ql=new URLSearchParams(location.search).get("lang");
    if(ql && STR[ql]) currentLang=ql;
  }catch(e){}
  applyLang();
  try{ const r=await fetch("/api/config"); if(!r.ok) throw new Error("HTTP "+r.status); cfg=await r.json(); }
  catch(e){ window.__cfgFail=true; cfg={fields:[],unit_map:{},dicts:{},value_whitelist:{}}; }
  if(!cfg.value_whitelist) cfg.value_whitelist={};
  applyCfgGuard(); window.__cfgReady=true; document.getElementById("cfgNavBtn").disabled=false;
  showStatus("","");
  loadHistory();
  refreshBackups();
  refreshProfiles();
  // 支持 ?view=materials|orders|config 直接进入某个视图（也便于自动化验证）
  //      ?view=config&tab=ord 可直达配置中心的订单页签
  try{
    const q=new URLSearchParams(location.search);
    const qv=q.get("view");
    const qt=q.get("tab");
    if(qt==="ord"||qt==="mat"||qt==="sys") cfgTab=qt;
    if(qv && VIEW_IDS[qv]) showModule(qv);
  }catch(e){}
  maybeShowWelcome();
})();

// ---- 配置保护 / 未保存提示 / 键盘与拖拽体验 ----
window.__dirty={mat:false,ord:false};
function applyCfgGuard(){
  const f=!!window.__cfgFail, o=!!window.__ordFail;
  ["saveConfigBtn","importConfigBtn","restoreBtn","profileSaveBtn","valuesImportBtn"].forEach(id=>{ const b=document.getElementById(id); if(b) b.disabled=f; });
  const ob=document.getElementById("ordCfgSaveBtn"); if(ob) ob.disabled=o;
  document.getElementById("cfgFailBanner").classList.toggle("show",f||o);
}
(function(){
  const vc=document.getElementById("viewConfig");
  const mark=e=>{
    const tg=e.target; if(!tg.closest) return;
    if(tg.matches("#profileSel,#backupSel,input[type=file]")) return;
    if(tg.closest("#cfgPaneMat")) window.__dirty.mat=true; else if(tg.closest("#cfgPaneOrd")) window.__dirty.ord=true;
  };
  vc.addEventListener("input",mark); vc.addEventListener("change",mark);
  vc.addEventListener("click",e=>{ if(e.target.closest&&e.target.closest(".row-del,.mini-btn,#addFieldBtn")) mark(e); });
})();
window.addEventListener("beforeunload",e=>{ if(window.__mesBusy||window.__dirty.mat||window.__dirty.ord){ e.preventDefault(); e.returnValue=""; } });
const _openModal=()=>[...document.querySelectorAll('[id$="Modal"]')].find(m=>m.style.display==="flex");
document.addEventListener("keydown",e=>{
  if(e.key==="Escape"){
    const m=_openModal(); if(m) hideModal(m.id);
    return;
  }
  if(e.key!=="Tab") return;
  const m=_openModal(); if(!m) return;
  // 焦点陷阱：Tab 在弹窗内循环，不会跑到背后的页面
  const f=[...m.querySelectorAll('button,a[href],input,select,textarea,[tabindex]:not([tabindex="-1"])')]
    .filter(x=>!x.disabled&&x.offsetParent!==null);
  if(!f.length) return;
  const first=f[0], last=f[f.length-1], cur=document.activeElement;
  if(!m.contains(cur)){ e.preventDefault(); first.focus(); return; }
  if(e.shiftKey&&cur===first){ e.preventDefault(); last.focus(); }
  else if(!e.shiftKey&&cur===last){ e.preventDefault(); first.focus(); }
});
// 页签支持左右方向键切换（ARIA tablist 惯例）
document.querySelector(".cfg-tabs").addEventListener("keydown",e=>{
  if(e.key!=="ArrowLeft"&&e.key!=="ArrowRight") return;
  e.preventDefault();
  const next=cfgTab==="mat"?"ord":"mat";
  showCfgTab(next);
  document.getElementById(next==="mat"?"cfgTabMat":"cfgTabOrd").focus();
});
// 动态生成的表格/图标按钮统一补语义：表头加 scope、纯符号的 ✕ 按钮加 aria-label
(function(){
  const fix=root=>{
    if(root.nodeType!==1) return;
    if(root.tagName==="TH"&&!root.hasAttribute("scope")) root.setAttribute("scope","col");
    if(root.classList&&root.classList.contains("row-del")&&!root.hasAttribute("aria-label")){
      const txt=(root.textContent||"").trim();
      if(!txt||txt==="✕") root.setAttribute("aria-label",t("del_row"));   // 有文字的（如「删除字典」）不动
    }
    if(root.querySelectorAll){
      root.querySelectorAll("th:not([scope])").forEach(th=>th.setAttribute("scope","col"));
      root.querySelectorAll(".row-del:not([aria-label])").forEach(b=>{
        const txt=(b.textContent||"").trim();
        if(!txt||txt==="✕") b.setAttribute("aria-label",t("del_row"));
      });
    }
  };
  new MutationObserver(ms=>ms.forEach(m=>m.addedNodes.forEach(fix)))
    .observe(document.body,{childList:true,subtree:true});
  document.querySelectorAll("th:not([scope])").forEach(th=>th.setAttribute("scope","col"));
})();
["dragover","drop"].forEach(ev=>window.addEventListener(ev,e=>{ if(!(e.target.closest&&e.target.closest(".card"))) e.preventDefault(); }));
