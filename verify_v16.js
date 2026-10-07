// v1.6.0 前端行为验证：把本轮 15 项里「有界面」的部分全部真实点一遍。
//   形态：起真实 exe → 无头 Chrome + CDP → 造 File 对象真实选择 / 真实点击 / 断言
//
//   覆盖：
//     A 批量多文件一次转（多选 + 合并提交 + 清单 + 清除）
//     B 配置保存前的 diff 确认（弹窗 / 取消不写 / 确认才写）
//     C 字段映射表 搜索 / 分组 / 拖拽排序
//     D 结果占比小图表 + 转换历史统计面板
//     E 系统页签（局域网 / 口令 / 自启 / 模板基准 / 诊断 / 更新）
//     F 口令弹窗 + 运行时语言切换文案
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const CDP_PORT = Number(process.env.CDP_PORT || 9341);
const APP_PORT = Number(process.env.APP_PORT || 8741);
const HERE = __dirname;
const EXE = path.join(HERE, 'mes_conv', '物料档案转换工具.exe');
const BASE = `http://127.0.0.1:${APP_PORT}/`;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const ERR_SNIFFER = `
window.__errs = [];
window.addEventListener('error', e => window.__errs.push('error: ' + e.message));
window.addEventListener('unhandledrejection', e => window.__errs.push('reject: ' + e.reason));
(function(){ const ce = console.error; console.error = function(){
  window.__errs.push('console.error: ' + Array.prototype.join.call(arguments, ' '));
  return ce.apply(console, arguments); }; })();
`;

// ---------------------------------------------------------------- A 批量多文件
const A_JS = `(async () => {
  const wait = (ms) => new Promise(r => setTimeout(r, ms));
  const out = []; const push = (k,v) => out.push([k, String(v)]);

  // 拦下 /api/convert，只记录表单里挂了几个 source，不去真跑转换
  window.__convCalls = [];
  const _f = window.fetch;
  window.fetch = function(input, init){
    const url = typeof input === 'string' ? input : ((input && input.url) || '');
    if (url.indexOf('/api/convert') >= 0 && init && init.body && init.body.getAll) {
      const srcs = init.body.getAll('source');
      window.__convCalls.push({ n: srcs.length, names: srcs.map(f => f.name) });
      return Promise.resolve(new Response(JSON.stringify({ ok:true, rows:3, file:'x.xlsx', log:[], issues:[], issue_total:0 }),
        { status:200, headers:{ 'content-type':'application/json' } }));
    }
    return _f.apply(this, arguments);
  };

  showModule('materials');
  await wait(200);

  const si = document.getElementById('srcFile');
  push('A01 源文件支持多选', si.multiple === true);
  push('A02 源文件接受 .xls/.csv', /[.]xls([,;]|$)/.test(si.accept) && si.accept.indexOf('.csv') >= 0);
  push('A03 订单/工单也支持多选',
    document.getElementById('ordSrcFile').multiple === true && document.getElementById('ordWorkFile').multiple === true);
  push('A04 源文件清单容器存在', !!document.getElementById('srcList'));

  const dt = new DataTransfer();
  ['甲_物料A.csv','乙_物料B.csv','丙_物料C.csv'].forEach(n => dt.items.add(new File(['物料编码\\nA1\\n'], n, { type:'text/csv' })));
  si.files = dt.files;
  si.dispatchEvent(new Event('change', { bubbles:true }));
  await wait(300);

  const nameEl = document.getElementById('srcName');
  push('A05 文件名区显示合并条数', nameEl.textContent.indexOf('已选择 3 个文件') >= 0);
  push('A06 清单列出 3 个文件', document.querySelectorAll('#srcList .fl-item').length);
  push('A07 清单里带序号文件名', /1\\.甲_物料A\\.csv/.test(document.getElementById('srcList').textContent));

  // 模板也塞一个，doConvert 才肯往下走
  const tdt = new DataTransfer();
  tdt.items.add(new File(['x'], '模板.xlsx', { type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }));
  const ti = document.getElementById('tplFile');
  ti.files = tdt.files;
  ti.dispatchEvent(new Event('change', { bubbles:true }));
  await wait(150);

  await doConvert();
  await wait(250);
  push('A08 提交时带 3 个源文件', window.__convCalls.length && window.__convCalls[0].n);
  push('A09 三个文件名顺序正确', window.__convCalls.length && window.__convCalls[0].names.join('|'));

  // 清除：点文件名右侧的 ×
  document.querySelector('#srcName .fn-clear').click();
  await wait(150);
  push('A10 点 × 后清单清空', document.querySelectorAll('#srcList .fl-item').length);
  push('A11 点 × 后回到未选择', document.getElementById('srcName').textContent.trim());

  push('A99 页面 JS 错误', (window.__errs||[]).length + ' | ' + JSON.stringify((window.__errs||[]).slice(0,3)));
  return JSON.stringify(out);
})()`;

// ---------------------------------------------------------------- B 保存前 diff
const B_JS = `(async () => {
  const wait = (ms) => new Promise(r => setTimeout(r, ms));
  const out = []; const push = (k,v) => out.push([k, String(v)]);

  window.__cfgPosts = 0;
  const _f = window.fetch;
  window.fetch = function(input, init){
    const url = typeof input === 'string' ? input : ((input && input.url) || '');
    const m = (init && init.method || 'GET').toUpperCase();
    if (url.indexOf('/api/config') >= 0 && m === 'POST') {
      window.__cfgPosts++;
      return Promise.resolve(new Response(JSON.stringify({ ok:true }), { status:200, headers:{ 'content-type':'application/json' } }));
    }
    return _f.apply(this, arguments);
  };

  showModule('config');
  document.getElementById('cfgTabMat').click();
  await wait(900);

  push('B01 配置中心可见', document.getElementById('viewConfig').style.display !== 'none');
  push('B02 物料字段表已渲染', document.querySelectorAll('#fieldTable tbody tr').length);

  // 改「输出工作表名称」制造一次真实改动（这个字段就是 cfg.output_sheet，最稳）
  const osi = document.getElementById('outSheetInput');
  window.__origVal = osi.value;
  osi.value = (osi.value || '') + '_体检';
  osi.dispatchEvent(new Event('input', { bubbles:true }));
  osi.dispatchEvent(new Event('change', { bubbles:true }));
  await wait(120);

  document.getElementById('saveConfigBtn').click();
  await wait(250);
  push('B03 保存时弹出确认框', document.getElementById('diffModal').style.display === 'flex');
  push('B04 弹窗标题', ((document.getElementById('diffTitle')||{}).textContent || '').trim());
  push('B05 弹窗列出了改动项', document.querySelectorAll('#diffBody .di').length);
  push('B06 改动项文案', ((document.querySelector('#diffBody .di')||{}).textContent || '').trim());
  push('B07 改动项按组归类', document.querySelectorAll('#diffBody .dg').length > 0);

  document.getElementById('diffCancel').click();
  await wait(200);
  push('B08 取消后弹窗关闭', document.getElementById('diffModal').style.display !== 'flex');
  push('B09 取消后没有写配置', window.__cfgPosts);

  document.getElementById('saveConfigBtn').click();
  await wait(200);
  push('B10 再点保存仍弹确认框', document.getElementById('diffModal').style.display === 'flex');
  document.getElementById('diffOk').click();
  await wait(400);
  push('B11 确认后才真的写配置', window.__cfgPosts);
  push('B12 确认后弹窗关闭', document.getElementById('diffModal').style.display !== 'flex');

  // 还原刚才改的字段，别把现场留下
  try { osi.value = window.__origVal; osi.dispatchEvent(new Event('change', { bubbles:true })); } catch(e){}
  push('B99 页面 JS 错误', (window.__errs||[]).length);
  return JSON.stringify(out);
})()`;

// ---------------------------------------------------------------- C 字段表工具
const C_JS = `(async () => {
  const wait = (ms) => new Promise(r => setTimeout(r, ms));
  const out = []; const push = (k,v) => out.push([k, String(v)]);

  showModule('config');
  document.getElementById('cfgTabMat').click();
  await wait(900);

  const s = document.getElementById('fldSearch'), g = document.getElementById('fldGroup');
  push('C01 搜索框存在', !!s);
  push('C02 分组下拉存在且有选项', g ? g.options.length : 0);

  const rows = Array.from(document.querySelectorAll('#fieldTable tbody tr'));
  const total = rows.length;
  push('C03 初始计数文案', (document.getElementById('fldCount')||{}).textContent || '');

  // 取第一行的目标列名当搜索词
  const firstTarget = (advFields.find(f => f.target) || {}).target || '';
  s.value = firstTarget;
  s.dispatchEvent(new Event('input', { bubbles:true }));
  await wait(150);
  const shown = rows.filter(r => r.style.display !== 'none').length;
  push('C04 搜索后只剩命中行', shown + '/' + total);
  push('C05 搜索后计数同步', (document.getElementById('fldCount')||{}).textContent || '');

  s.value = '';
  s.dispatchEvent(new Event('input', { bubbles:true }));
  await wait(150);
  push('C06 清空后恢复全部', rows.filter(r => r.style.display !== 'none').length);

  // 分组：选一个填充方式
  const ops = [...new Set(advFields.map(f => f.op).filter(Boolean))];
  const anOp = ops[0] || 'copy';
  g.value = anOp;
  g.dispatchEvent(new Event('change', { bubbles:true }));
  await wait(150);
  const shownOp = rows.filter(r => r.style.display !== 'none').length;
  const sameOp = advFields.filter(f => f.op === anOp).length;
  push('C07 按填充方式分组生效', shownOp + '/' + sameOp);
  g.value = '';
  g.dispatchEvent(new Event('change', { bubbles:true }));
  await wait(150);

  push('C08 表头有拖拽列', !!document.querySelector('#fieldTable thead .drag-th'));
  push('C09 每行都有拖拽把手', document.querySelectorAll('#fieldTable tbody .drag-handle').length);

  // 拖拽排序：把第 1 行拖到第 2 行
  const rws = Array.from(document.querySelectorAll('#fieldTable tbody tr'));
  const b0 = advFields[0] && advFields[0].target, b1 = advFields[1] && advFields[1].target;
  const dt = new DataTransfer();
  rws[0].querySelector('.drag-handle').dispatchEvent(new DragEvent('dragstart', { bubbles:true, dataTransfer:dt }));
  rws[1].dispatchEvent(new DragEvent('dragover', { bubbles:true, cancelable:true, dataTransfer:dt }));
  rws[1].dispatchEvent(new DragEvent('drop', { bubbles:true, cancelable:true, dataTransfer:dt }));
  await wait(250);
  const a0 = advFields[0] && advFields[0].target, a1 = advFields[1] && advFields[1].target;
  push('C10 拖拽后前两行互换', a0 === b1 && a1 === b0);
  push('C11 拖拽详情', '原位 ' + b0 + ',' + b1 + ' → 现位 ' + a0 + ',' + a1);

  push('C99 页面 JS 错误', (window.__errs||[]).length);
  return JSON.stringify(out);
})()`;

// ---------------------------------------------------------------- D 图表与统计
const D_JS = `(async () => {
  const wait = (ms) => new Promise(r => setTimeout(r, ms));
  const out = []; const push = (k,v) => out.push([k, String(v)]);

  showModule('materials');
  await wait(1400);

  push('D01 统计面板已显示', (() => { const s = document.getElementById('statsSection');
    return !!s && s.classList.contains('show') && s.offsetHeight > 0; })());
  push('D02 统计卡片数', document.querySelectorAll('#statsGrid .stat').length);
  push('D03 柱状图柱子数', document.querySelectorAll('#statsBars .bar').length);
  push('D04 统计面板文案', (document.getElementById('statsGrid')||{}).textContent.slice(0, 80));
  push('D10 统计面板挂在物料档案视图内', (document.getElementById('statsSection')||{}).parentElement.id);
  push('D11 统计面板有标题', ((document.getElementById('stats_title')||{}).textContent || '').trim());

  // 占比图：直接喂一份结果数据（等价于转换完成后 renderStats 走的那条路）
  renderStats({ ok:true, rows:10, issues:[
    { level:'error',   row:3, msg:'x' },
    { level:'warning', row:5, msg:'y' },
    { level:'warning', row:5, msg:'z' }
  ], issue_total:3 });
  await wait(150);
  const w = document.getElementById('donutWrap');
  push('D05 占比图渲染出 SVG', !!w.querySelector('svg'));
  push('D06 占比图有图例', w.querySelectorAll('.donut-legend > div').length);
  push('D07 图例含 正常/告警/错误', /正常行/.test(w.textContent) && /告警/.test(w.textContent) && /错误/.test(w.textContent));
  push('D08 占比图在结果区内', !!document.querySelector('#resultSection #donutWrap'));

  // 订单模块也有一套
  push('D09 订单结果区也有占比图容器', !!document.querySelector('#ordResultSection #ordDonutWrap'));

  push('D99 页面 JS 错误', (window.__errs||[]).length);
  return JSON.stringify(out);
})()`;

// ---------------------------------------------------------------- E 系统页签
const E_JS = `(async () => {
  const wait = (ms) => new Promise(r => setTimeout(r, ms));
  const out = []; const push = (k,v) => out.push([k, String(v)]);

  showModule('config');
  document.getElementById('cfgTabSys').click();
  await wait(1400);

  push('E01 系统页签可见', document.getElementById('cfgPaneSys').style.display !== 'none');
  push('E02 系统说明为长文案', (document.getElementById('sys_desc')||{}).textContent.length > 30);
  push('E03 三张系统卡齐全',
    !!document.getElementById('sys_lan_title') && !!document.getElementById('sys_pwd_title') && !!document.getElementById('sys_auto_title'));
  push('E04 局域网地址已填充', ((document.getElementById('sysAddrs')||{}).textContent || '').length > 5);
  push('E05 局域网状态胶囊', (document.getElementById('sysLanPill')||{}).textContent);
  push('E06 口令状态胶囊', (document.getElementById('sysPwdPill')||{}).textContent);
  push('E07 自启状态胶囊', (document.getElementById('sysAutoPill')||{}).textContent);
  push('E08 模板基准状态', (document.getElementById('sysTplState')||{}).textContent);

  ['sysLanSave','sysAutoSave','sysPwdSave','sysPwdClear','sysTplReset','sysTplRefresh','sysDiagBtn','sysUpdateBtn']
    .forEach(id => push('E09 按钮存在 ' + id, !!document.getElementById(id)));

  // 刷新模板基准状态
  document.getElementById('sysTplRefresh').click();
  await wait(700);
  push('E10 刷新后基准状态非空', ((document.getElementById('sysTplState')||{}).textContent || '').length > 1);

  // 检查更新（可能没网，只要给出可读结果即可）
  document.getElementById('sysUpdateBtn').click();
  await wait(4000);
  const up = (document.getElementById('sysUpdateState')||{}).textContent || '';
  push('E11 检查更新有可读输出', up.length > 1);
  push('E12 检查更新文案', up);

  push('E99 页面 JS 错误', (window.__errs||[]).length + ' | ' + JSON.stringify((window.__errs||[]).slice(0,3)));
  return JSON.stringify(out);
})()`;

// ---------------------------------------------------------------- F 口令弹窗 + 语言
const F_JS = `(async () => {
  const wait = (ms) => new Promise(r => setTimeout(r, ms));
  const out = []; const push = (k,v) => out.push([k, String(v)]);

  push('F01 口令弹窗存在', !!document.getElementById('authModal'));
  push('F02 requestAuth 是函数', typeof requestAuth === 'function');

  requestAuth();
  await wait(200);
  push('F03 调用后弹窗显示', document.getElementById('authModal').style.display === 'flex');
  push('F04 焦点落在口令框', document.activeElement && document.activeElement.id === 'authPwd');

  document.dispatchEvent(new KeyboardEvent('keydown', { key:'Escape', bubbles:true }));
  await wait(200);
  push('F05 Esc 关闭口令弹窗', document.getElementById('authModal').style.display !== 'flex');

  push('F06 fetch 注入 X-Token', /X-Token/.test(window.fetch.toString()));

  // 运行时切语言：新增的拖拽提示文案要跟着变
  currentLang = 'en'; applyLang(); await wait(150);
  push('F07 切英文后源文件拖拽提示', (document.getElementById('srcDropHint')||{}).textContent);
  currentLang = 'zht'; applyLang(); await wait(150);
  push('F08 切繁中后源文件拖拽提示', (document.getElementById('srcDropHint')||{}).textContent);
  push('F09 繁中下系统页签名', (document.getElementById('cfgTabSys')||{}).textContent);
  currentLang = 'vi'; applyLang(); await wait(150);
  push('F10 切越南语后订单拖拽提示', (document.getElementById('ordSrcDropHint')||{}).textContent);
  currentLang = 'zh'; applyLang(); await wait(150);
  push('F11 切回简中', (document.getElementById('srcDropHint')||{}).textContent);

  push('F99 页面 JS 错误', (window.__errs||[]).length);
  return JSON.stringify(out);
})()`;

async function waitHttp(url, tries = 40) {
  for (let i = 0; i < tries; i++) {
    try { const r = await fetch(url); if (r.ok) return true; } catch (e) {}
    await sleep(300);
  }
  return false;
}

// 期望值表：key -> 判定函数（返回 true 表示通过）
const EXPECT = {
  A01: (v) => v === 'true',
  A02: (v) => v === 'true',
  A03: (v) => v === 'true',
  A04: (v) => v === 'true',
  A05: (v) => v === 'true',
  A06: (v) => v === '3',
  A07: (v) => v === 'true',
  A08: (v) => v === '3',
  A09: (v) => v === '甲_物料A.csv|乙_物料B.csv|丙_物料C.csv',
  A10: (v) => v === '0',
  A11: (v) => v === '尚未选择文件',
  A99: (v) => /^0(\s*\|.*)?$/.test(v),

  B01: (v) => v === 'true',
  B02: (v) => Number(v) > 0,
  B03: (v) => v === 'true',
  B04: (v) => v.length > 0,
  B05: (v) => Number(v) > 0,
  B06: (v) => v.length > 0,
  B07: (v) => v === 'true',
  B08: (v) => v === 'true',
  B09: (v) => v === '0',
  B10: (v) => v === 'true',
  B11: (v) => v === '1',
  B12: (v) => v === 'true',
  B99: (v) => /^0(\s*\|.*)?$/.test(v),

  C01: (v) => v === 'true',
  C02: (v) => Number(v) > 1,
  C03: (v) => /显示 \d+ \/ \d+ 行/.test(v),
  C04: (v) => { const [a, b] = v.split('/').map(Number); return a > 0 && a < b; },
  C05: (v) => /显示 \d+ \/ \d+ 行/.test(v),
  C06: (v) => Number(v) > 0,
  C07: (v) => { const [a, b] = v.split('/').map(Number); return a > 0 && a === b; },
  C08: (v) => v === 'true',
  C09: (v) => Number(v) > 0,
  C10: (v) => v === 'true',
  C11: (v) => v.length > 0,
  C99: (v) => /^0(\s*\|.*)?$/.test(v),

  D01: (v) => v === 'true',
  D02: (v) => Number(v) > 0,
  D03: (v) => Number(v) > 0,
  D04: (v) => v.length > 0,
  D05: (v) => v === 'true',
  D06: (v) => Number(v) === 3,
  D07: (v) => v === 'true',
  D08: (v) => v === 'true',
  D09: (v) => v === 'true',
  D10: (v) => v === 'viewMaterials',
  D11: (v) => v === '转换统计',
  D99: (v) => /^0(\s*\|.*)?$/.test(v),

  E01: (v) => v === 'true',
  E02: (v) => v === 'true',
  E03: (v) => v === 'true',
  E04: (v) => v === 'true',
  E05: (v) => v === '已开启' || v === '已关闭',
  E06: (v) => v === '已启用' || v === '未启用',
  E07: (v) => v === '已开启' || v === '已关闭',
  E08: (v) => v.length > 1,
  E10: (v) => v === 'true',
  E11: (v) => v === 'true',
  E12: (v) => v.length > 0,
  E99: (v) => /^0(\s*\|.*)?$/.test(v),

  F01: (v) => v === 'true',
  F02: (v) => v === 'true',
  F03: (v) => v === 'true',
  F04: (v) => v === 'true',
  F05: (v) => v === 'true',
  F06: (v) => v === 'true',
  F07: (v) => v === 'You can also drag files here; select multiple files and they are merged by column name',
  F08: (v) => v === '也可以直接把檔案拖到這裡；可一次選多個，按欄名合併成一份結果',
  F09: (v) => v === '系統',
  F10: (v) => v === 'Cũng có thể kéo tệp vào đây; có thể chọn nhiều tệp cùng lúc',
  F11: (v) => v === '也可以直接把文件拖到这里；可一次选多个，按列名合并成一份结果',
  F99: (v) => /^0(\s*\|.*)?$/.test(v),
};

// 只做「存在性」判定、不校验值的项
const INFO_ONLY = new Set(['E09']);
const INFO_KEYS = ['B04','B06','C11','D04','E05','E06','E07','E08','E12','A11'];

(async () => {
  let app = null;
  try { const r = await fetch(BASE + 'api/ping'); if (r.ok) { console.log('✗ 端口 ' + APP_PORT + ' 已被占用'); process.exit(1); } } catch (e) {}

  app = spawn(EXE, ['-nobrowser', '-port', String(APP_PORT)], { cwd: path.join(HERE, 'mes_conv'), stdio: 'ignore' });
  if (!await waitHttp(BASE + 'api/ping')) { console.log('✗ 程序未能在 ' + APP_PORT + ' 上启动'); app.kill(); process.exit(1); }
  console.log('程序已启动：' + BASE);

  const prof = path.join(HERE, '_chrome_prof_v16');
  const chrome = spawn(CHROME, [
    '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
    '--remote-debugging-port=' + CDP_PORT, '--user-data-dir=' + prof,
    '--window-size=1360,1100', 'about:blank',
  ], { stdio: 'ignore' });

  let wsUrl = null;
  for (let i = 0; i < 60; i++) {
    try {
      const j = await (await fetch(`http://127.0.0.1:${CDP_PORT}/json/list`)).json();
      const p = j.find((t) => t.type === 'page');
      if (p && p.webSocketDebuggerUrl) { wsUrl = p.webSocketDebuggerUrl; break; }
    } catch (e) {}
    await sleep(250);
  }
  if (!wsUrl) { console.log('✗ CDP 未就绪'); chrome.kill(); if (app) app.kill(); process.exit(1); }

  const ws = new WebSocket(wsUrl);
  let seq = 0; const pending = new Map();
  await new Promise((res) => { ws.onopen = res; });
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
  };
  const send = (method, params = {}) => new Promise((res) => {
    const id = ++seq; pending.set(id, res);
    ws.send(JSON.stringify({ id, method, params }));
  });
  const evaluate = async (expr) => {
    const r = await send('Runtime.evaluate', { expression: expr, returnByValue: true, awaitPromise: true });
    if (r.result && r.result.exceptionDetails) {
      return { err: JSON.stringify(r.result.exceptionDetails.exception || r.result.exceptionDetails) };
    }
    return { value: r.result.result.value };
  };

  await send('Page.enable');
  await send('Runtime.enable');
  await send('Page.addScriptToEvaluateOnNewDocument', { source: ERR_SNIFFER });

  let ok = true, pass = 0, fail = 0;
  const runRound = async (title, js, shot) => {
    console.log('\n【' + title + '】');
    await send('Page.navigate', { url: BASE });
    await sleep(2600);
    const r = await evaluate(js);
    if (r.err) { console.log('✗ 页面 JS 抛错：' + r.err); ok = false; fail++; return; }
    let rows;
    try { rows = JSON.parse(r.value); } catch (e) { console.log('✗ 结果不是 JSON：' + String(r.value).slice(0, 200)); ok = false; fail++; return; }
    for (const [k, v] of rows) {
      const base = k.slice(0, 3);
      if (INFO_ONLY.has(base)) { console.log('  --   ' + k + ' = ' + v); continue; }
      const fn = EXPECT[base];
      if (!fn) { console.log('  --   ' + k + ' = ' + v); continue; }
      const good = fn(v);
      if (INFO_KEYS.indexOf(base) >= 0) console.log('  info ' + k + ' = ' + v);
      console.log((good ? '  PASS ' : '  FAIL ') + k + ' = ' + v);
      if (good) pass++; else { fail++; ok = false; }
    }
    if (shot) {
      const s = await send('Page.captureScreenshot', { format: 'png' });
      if (s.result && s.result.data) {
        fs.writeFileSync(path.join(HERE, shot), Buffer.from(s.result.data, 'base64'));
        console.log('  （已截图 ' + shot + '）');
      }
    }
  };

  await runRound('A 批量多文件一次转', A_JS, null);
  await runRound('B 配置保存前 diff 确认', B_JS, null);
  await runRound('C 字段映射表 搜索/分组/拖拽', C_JS, null);
  await runRound('D 占比小图 + 统计面板', D_JS, '_shot_v16_stats.png');
  await runRound('E 系统页签', E_JS, '_shot_v16_sys.png');
  await runRound('F 口令弹窗 + 语言切换', F_JS, null);

  ws.close();
  chrome.kill();
  if (app) app.kill();
  await sleep(500);
  console.log('\n———— 结果：' + pass + ' 通过 / ' + fail + ' 失败 ————');
  console.log(ok ? 'v1.6.0 前端行为全部通过 ✓' : '存在问题 ✗');
  process.exit(ok ? 0 : 1);
})();
