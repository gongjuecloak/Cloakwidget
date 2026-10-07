// 验证「配置中心」：配置从操作页面全部搬走后，页面结构与交互是否正确
// 形态：起真实 exe（拿真配置）→ 无头 Chrome + CDP → 真实点击 + 断言
const { spawn } = require('child_process');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const CDP_PORT = Number(process.env.CDP_PORT || 9335);
// 端口可用环境变量覆盖：本机若已有实例在跑（用户自己开着界面），换一个端口独立验证
const APP_PORT = Number(process.env.APP_PORT || 8731);
const HERE = __dirname;
const EXE = path.join(HERE, 'mes_conv', '物料档案转换工具.exe');
const BASE = `http://127.0.0.1:${APP_PORT}/`;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// 页面内错误收集（在文档创建前注入，能抓到加载期报错）
const ERR_SNIFFER = `
window.__errs = [];
window.addEventListener('error', e => window.__errs.push('error: ' + e.message));
window.addEventListener('unhandledrejection', e => window.__errs.push('reject: ' + e.reason));
(function(){ const ce = console.error; console.error = function(){
  window.__errs.push('console.error: ' + Array.prototype.join.call(arguments, ' '));
  return ce.apply(console, arguments); }; })();
`;

const ASSERT_JS = `(async () => {
  const wait = (ms) => new Promise(r => setTimeout(r, ms));
  const out = [];
  const push = (k, v) => out.push([k, String(v)]);
  const vis  = (id) => { const e = document.getElementById(id); return !!e && e.style.display !== 'none'; };

  // ---------- 首页 ----------
  showModule('home');
  push('T01 首页可见', vis('viewHome'));
  push('T02 首页有配置中心入口', !!document.getElementById('cfgEntry'));
  push('T03 首页仍是两张模块卡', document.querySelectorAll('.mod-grid .mod-card').length);

  // ---------- 点入口进配置中心 ----------
  document.getElementById('cfgEntry').click();
  push('T04 配置视图可见', vis('viewConfig'));
  push('T05 两个操作视图已隐藏', !vis('viewMaterials') && !vis('viewOrders'));
  push('T06 头部标题切为配置中心', document.getElementById('app_title').textContent);
  push('T07 logo 切为 CFG', document.getElementById('appLogo').textContent);
  push('T08 默认显示物料面板', vis('cfgPaneMat'));
  push('T09 订单面板默认隐藏', !vis('cfgPaneOrd'));
  push('T10 物料面板含配置控件', !!document.querySelector('#cfgPaneMat #fieldTable')
      && !!document.querySelector('#cfgPaneMat #unitBody')
      && !!document.querySelector('#cfgPaneMat #dictsWrap')
      && !!document.querySelector('#cfgPaneMat #wlWrap'));
  push('T11 物料面板字段表已渲染行', document.querySelectorAll('#cfgPaneMat #fieldTable tbody tr').length > 0);

  // ---------- 切到订单页签（订单配置是异步 fetch 的，要等它渲染完）----------
  document.getElementById('cfgTabOrd').click();
  await wait(1200);
  push('T12 订单面板可见', vis('cfgPaneOrd'));
  push('T13 物料面板已隐藏', !vis('cfgPaneMat'));
  push('T14 订单面板含配置控件', !!document.querySelector('#cfgPaneOrd #ordMapWrap')
      && !!document.querySelector('#cfgPaneOrd #ordAliasWrap')
      && !!document.querySelector('#cfgPaneOrd #ordSwitches'));
  push('T15 订单面板字段表已渲染行', document.querySelectorAll('#cfgPaneOrd #ordMapWrap table tbody tr').length > 0);
  push('T16 页签激活态正确', document.getElementById('cfgTabOrd').classList.contains('active')
      && !document.getElementById('cfgTabMat').classList.contains('active'));

  // ---------- 关键：操作页面里不能再有任何配置 ----------
  push('T17 物料操作视图无配置控件', !document.querySelector('#viewMaterials #fieldTable')
      && !document.querySelector('#viewMaterials #unitBody')
      && !document.querySelector('#viewMaterials #dictsWrap')
      && !document.querySelector('#viewMaterials #wlWrap'));
  push('T18 订单操作视图无配置控件', !document.querySelector('#viewOrders #ordMapWrap')
      && !document.querySelector('#viewOrders #ordAliasWrap')
      && !document.querySelector('#viewOrders #ordSwitches'));
  push('T19 操作视图无高级设置折叠块', document.querySelectorAll('main details').length);
  push('T20 操作视图无 .advanced 区块', document.querySelectorAll('#viewMaterials .advanced, #viewOrders .advanced').length);

  // ---------- 操作视图功能仍在 ----------
  showModule('materials');
  push('T21 物料视图可见且无配置', vis('viewMaterials') && document.querySelectorAll('#viewMaterials .advanced').length === 0);
  push('T22 物料视图仍有选文件/转换/结果/日志', !!document.getElementById('srcFile')
      && !!document.getElementById('tplFile') && !!document.getElementById('startBtn')
      && !!document.getElementById('resultSection') && !!document.getElementById('logBox'));

  showModule('orders');
  push('T23 订单视图可见且无配置', vis('viewOrders') && document.querySelectorAll('#viewOrders .advanced').length === 0);
  push('T24 订单视图仍有选文件/转换/结果', !!document.getElementById('ordSrcFile')
      && !!document.getElementById('ordWorkFile') && !!document.getElementById('ordStartBtn')
      && !!document.getElementById('ordResultSection'));

  // ---------- header 的配置按钮 ----------
  document.getElementById('cfgNavBtn').click();
  push('T25 header 配置按钮能进配置中心', vis('viewConfig'));
  push('T26 进配置中心时保持上次页签（订单）', vis('cfgPaneOrd') && !vis('cfgPaneMat'));
  document.getElementById('backHomeBtn').click();
  push('T27 返回按钮回首页', vis('viewHome'));

  // ---------- 页签记忆：切到订单后再离开回来，仍停在订单 ----------
  showModule('config');
  document.getElementById('cfgTabOrd').click();
  showModule('home');
  showModule('config');
  push('T28 页签状态被记住', vis('cfgPaneOrd'));

  push('T29 页面 JS 错误', (window.__errs || []).length + ' | ' + JSON.stringify((window.__errs || []).slice(0, 3)));
  return JSON.stringify(out);
})()`;

const DEEP_LINK_JS = `(() => {
  const out = [];
  const push = (k, v) => out.push([k, String(v)]);
  const vis = (id) => { const e = document.getElementById(id); return !!e && e.style.display !== 'none'; };
  push('D1 深链直达配置中心', vis('viewConfig'));
  push('D2 深链直达订单页签', vis('cfgPaneOrd') && !vis('cfgPaneMat'));
  push('D3 页签激活态', document.getElementById('cfgTabOrd').classList.contains('active'));
  push('D4 头部标题', document.getElementById('app_title').textContent);
  push('D5 JS 错误', (window.__errs || []).length);
  return JSON.stringify(out);
})()`;

async function waitHttp(url, tries = 40) {
  for (let i = 0; i < tries; i++) {
    try { const r = await fetch(url); if (r.ok) return true; } catch (e) {}
    await sleep(300);
  }
  return false;
}

(async () => {
  // 先确认端口上有没有实例在跑
  let occupied = false;
  try { const r = await fetch(BASE + 'api/ping'); if (r.ok) occupied = true; } catch (e) {}
  // 程序是全局单实例（互斥量），换端口也起不了第二份。
  // REUSE=1 时改为复用已在运行的实例 —— 它磁盘优先加载 webui.html，测到的同样是当前磁盘上的界面。
  const REUSE = process.env.REUSE === '1';
  let app = null;
  if (occupied && !REUSE) {
    console.log('✗ 端口 ' + APP_PORT + ' 上已有程序在跑，请先退出它再测（避免测到旧界面），或用 REUSE=1 复用。');
    process.exit(1);
  }
  if (occupied) {
    console.log('复用已在运行的实例：' + BASE);
  } else {
    app = spawn(EXE, ['-nobrowser', '-port', String(APP_PORT)], { cwd: path.join(HERE, 'mes_conv'), stdio: 'ignore' });
    if (!await waitHttp(BASE + 'api/ping')) {
      console.log('✗ 程序未能在 ' + APP_PORT + ' 上启动');
      app.kill(); process.exit(1);
    }
    console.log('程序已启动：' + BASE);
  }

  const prof = path.join(HERE, '_chrome_prof_cfg');
  const chrome = spawn(CHROME, [
    '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
    '--remote-debugging-port=' + CDP_PORT, '--user-data-dir=' + prof,
    '--window-size=1280,1000', 'about:blank',
  ], { stdio: 'ignore' });

  let wsUrl = null;
  for (let i = 0; i < 50; i++) {
    try {
      const j = await (await fetch(`http://127.0.0.1:${CDP_PORT}/json/list`)).json();
      const p = j.find((t) => t.type === 'page');
      if (p && p.webSocketDebuggerUrl) { wsUrl = p.webSocketDebuggerUrl; break; }
    } catch (e) {}
    await sleep(250);
  }
  if (!wsUrl) { console.log('✗ CDP 未就绪'); chrome.kill(); if (app) app.kill(); process.exit(1); }

  const ws = new WebSocket(wsUrl);
  let seq = 0; const pending = new Map(); const events = [];
  await new Promise((res) => { ws.onopen = res; });
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
    else if (m.method) events.push(m);
  };
  const send = (method, params = {}) => new Promise((res) => {
    const id = ++seq; pending.set(id, res);
    ws.send(JSON.stringify({ id, method, params }));
  });

  await send('Page.enable');
  await send('Runtime.enable');
  await send('Page.addScriptToEvaluateOnNewDocument', { source: ERR_SNIFFER });

  const evaluate = async (expr) => {
    const r = await send('Runtime.evaluate', { expression: expr, returnByValue: true, awaitPromise: true });
    if (r.result && r.result.exceptionDetails) {
      return { err: JSON.stringify(r.result.exceptionDetails.exception || r.result.exceptionDetails) };
    }
    return { value: r.result.result.value };
  };

  const fail = (m) => { console.log('FAIL ' + m); ok = false; };
  let ok = true;

  const report = (rows, expectKeys) => {
    for (const [k, v] of rows) {
      let pass = true;
      if (/JS 错误/.test(k)) pass = /^0(\s*\|.*)?$/.test(v) || /^0$/.test(v);
      else if (/T06|D4/.test(k)) pass = v.indexOf('配置中心') >= 0;
      else if (/T07/.test(k)) pass = v === 'CFG';
      else if (/T03/.test(k)) pass = v === '2';
      else if (/T19|T20/.test(k)) pass = v === '0';
      else if (/^T\d\d|^D\d/.test(k)) pass = v === 'true';
      console.log((pass ? 'PASS ' : 'FAIL ') + ' ' + k + ' = ' + v);
      if (!pass) ok = false;
    }
  };

  // ---------- 第一轮：默认入口 ----------
  console.log('\n【第一轮 · 交互流程】');
  await send('Page.navigate', { url: BASE });
  await sleep(2200);
  let r = await evaluate(ASSERT_JS);
  if (r.err) { console.log('✗ 页面 JS 抛错：' + r.err); ok = false; }
  else report(JSON.parse(r.value));

  // ---------- 第二轮：深链 ----------
  console.log('\n【第二轮 · 深链 ?view=config&tab=ord】');
  await send('Page.navigate', { url: BASE + '?view=config&tab=ord' });
  await sleep(2200);
  r = await evaluate(DEEP_LINK_JS);
  if (r.err) { console.log('✗ 页面 JS 抛错：' + r.err); ok = false; }
  else report(JSON.parse(r.value));

  ws.close();
  chrome.kill();
  if (app) app.kill();   // 复用模式（REUSE=1）下不能杀用户自己开着的实例
  await sleep(400);
  console.log('\n结果：', ok ? '配置中心结构与交互全部通过 ✓' : '存在问题 ✗');
  process.exit(ok ? 0 : 1);
})();
