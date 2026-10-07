// 验证 v1.5.0 新增的可访问性与键盘交互
// 形态：复用已在运行的服务实例 → 无头 Chrome + CDP → 真实查询 DOM / 真实按键
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const CDP = Number(process.env.CDP_PORT || 9337);
const BASE = process.env.SHOT_BASE || 'http://127.0.0.1:8731/';
const HERE = __dirname;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let pass = 0, fail = 0;
const check = (name, cond, extra) => {
  if (cond) { pass++; console.log('PASS  ' + name + (extra !== undefined ? '  = ' + extra : '')); }
  else { fail++; console.log('FAIL  ' + name + (extra !== undefined ? '  = ' + extra : '')); }
};

const PROBE = `(() => {
  const out = {};
  const q = s => document.querySelector(s);
  const qa = s => [...document.querySelectorAll(s)];

  // ---- 页签语义 ----
  const tabs = qa('.cfg-tab');
  out.tabCount = tabs.length;
  out.tablist = !!q('.cfg-tabs[role="tablist"]');
  out.tabRoles = tabs.every(t => t.getAttribute('role') === 'tab');
  out.tabControls = tabs.map(t => t.getAttribute('aria-controls'));
  out.controlsExist = out.tabControls.every(id => !!document.getElementById(id));
  out.panelRole = qa('[role="tabpanel"]').length;
  out.panelLabelled = qa('[role="tabpanel"]').every(p => !!document.getElementById(p.getAttribute('aria-labelledby')));
  out.ariaSelected = tabs.map(t => t.getAttribute('aria-selected'));

  // ---- 表格表头 ----
  const ths = qa('th');
  out.thTotal = ths.length;
  out.thScoped = ths.filter(t => t.getAttribute('scope') === 'col').length;

  // ---- 动态删除按钮 ----
  // 纯符号（✕）的按钮必须补 aria-label；有可见文字的（如「删除字典」）靠文字播报，不该被覆盖
  const del = qa('.row-del');
  const symOnly = del.filter(b => { const t = (b.textContent || '').trim(); return !t || t === '✕'; });
  const hasText = del.filter(b => { const t = (b.textContent || '').trim(); return t && t !== '✕'; });
  out.delTotal = del.length;
  out.delSymOnly = symOnly.length;
  out.delLabelled = symOnly.filter(b => b.getAttribute('aria-label')).length;
  out.delTextCount = hasText.length;
  out.delTextUntouched = hasText.every(b => !b.getAttribute('aria-label'));
  const dictDel = q('.d-card-del');
  out.dictDelText = dictDel ? dictDel.textContent.trim() : '';
  out.dictDelKeepsText = dictDel ? (dictDel.textContent.trim().length > 1) : true;
  out.dictDelLabel = dictDel ? (dictDel.getAttribute('aria-label') || '') : '(无按钮)';

  // ---- 状态条播报 ----
  out.bannerLive = [q('#statusBanner'), q('#ordBanner')].every(b => b && b.getAttribute('aria-live') === 'polite');
  out.alertRole = !!(q('#cfgFailBanner') && q('#cfgFailBanner').getAttribute('role') === 'alert');

  // ---- 焦点可见样式已定义（:focus-visible 规则存在）----
  let fv = false;
  for (const ss of document.styleSheets) {
    try { for (const r of ss.cssRules) { if (r.selectorText && r.selectorText.includes(':focus-visible')) fv = true; } } catch (e) {}
  }
  out.focusVisible = fv;

  // ---- 隐藏面板不该能被 Tab 到 ----
  const hiddenPane = qa('.view').filter(v => v.style.display === 'none');
  out.hiddenViews = hiddenPane.length;
  out.hiddenNotFocusable = hiddenPane.every(v => getComputedStyle(v).display === 'none');
  return JSON.stringify(out);
})()`;

const AFTER_TAB_KEY = `(() => {
  const out = {};
  const tabs = [...document.querySelectorAll('.cfg-tab')];
  out.selected = tabs.map(t => t.getAttribute('aria-selected'));
  out.activeId = (document.activeElement && document.activeElement.id) || '';
  return JSON.stringify(out);
})()`;

const MODAL_PROBE = `(() => {
  const out = {};
  const m = document.getElementById('helpModal');
  out.shown = m.style.display === 'flex';
  out.focusId = (document.activeElement && document.activeElement.id) || '';
  out.trapInside = m.contains(document.activeElement);
  return JSON.stringify(out);
})()`;

(async () => {
  const prof = path.join(HERE, '_chrome_prof_a11y');
  fs.rmSync(prof, { recursive: true, force: true });
  const chrome = spawn(CHROME, [
    '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
    '--remote-debugging-port=' + CDP, '--user-data-dir=' + prof,
    '--window-size=1280,1000', 'about:blank',
  ], { stdio: 'ignore' });

  let wsUrl = null;
  for (let i = 0; i < 40 && !wsUrl; i++) {
    await sleep(300);
    try {
      const j = await (await fetch(`http://127.0.0.1:${CDP}/json/list`)).json();
      const t = j.find((x) => x.type === 'page');
      if (t) wsUrl = t.webSocketDebuggerUrl;
    } catch (e) {}
  }
  if (!wsUrl) { console.log('✗ CDP 未就绪'); chrome.kill(); process.exit(1); }

  const ws = new WebSocket(wsUrl);
  await new Promise((r) => (ws.onopen = r));
  let id = 0; const waiters = new Map();
  const errs = [];
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && waiters.has(m.id)) { waiters.get(m.id)(m); waiters.delete(m.id); return; }
    if (m.method === 'Runtime.exceptionThrown') errs.push(m.params.exceptionDetails.text);
    if (m.method === 'Runtime.consoleAPICalled' && m.params.type === 'error')
      errs.push((m.params.args || []).map(a => a.value).join(' '));
  };
  const send = (method, params = {}) => new Promise((res) => {
    const i = ++id; waiters.set(i, res); ws.send(JSON.stringify({ id: i, method, params }));
  });
  const evalJs = async (expression) => {
    const r = await send('Runtime.evaluate', { expression, returnByValue: true });
    return r.result.result.value;
  };

  await send('Runtime.enable');
  await send('Page.enable');

  console.log('\n【静态语义 · 配置中心】');
  await send('Page.navigate', { url: BASE + '?view=config&tab=mat' });
  await sleep(2400);
  let d = JSON.parse(await evalJs(PROBE));
  check('tablist 存在 role=tablist', d.tablist);
  check('两个页签都是 role=tab', d.tabCount === 2 && d.tabRoles, d.tabCount);
  check('页签 aria-controls 指向存在的面板', d.controlsExist, JSON.stringify(d.tabControls));
  check('两个面板都有 role=tabpanel', d.panelRole === 2, d.panelRole);
  check('面板有 aria-labelledby 且指向存在的页签', d.panelLabelled);
  check('aria-selected 默认正确（物料=1）', d.ariaSelected[0] === 'true' && d.ariaSelected[1] === 'false', JSON.stringify(d.ariaSelected));
  check('表头全部带 scope=col', d.thTotal > 0 && d.thScoped === d.thTotal, d.thScoped + '/' + d.thTotal);
  check('纯符号 ✕ 按钮都带 aria-label', d.delSymOnly > 0 && d.delLabelled === d.delSymOnly, d.delLabelled + '/' + d.delSymOnly);
  check('有可见文字的按钮未被 aria-label 覆盖', d.delTextUntouched, d.delTextCount + ' 个带文字按钮');
  check('「删除字典」按钮保留可见文字（未被 aria-label 覆盖）', d.dictDelKeepsText, JSON.stringify(d.dictDelText));
  check('状态条带 aria-live=polite', d.bannerLive);
  check('配置失败告警条 role=alert', d.alertRole);
  check(':focus-visible 规则已定义', d.focusVisible);
  check('隐藏视图确实不可聚焦（display:none）', d.hiddenNotFocusable, d.hiddenViews + ' 个隐藏视图');

  console.log('\n【键盘 · 左右方向键切页签】');
  await send('Runtime.evaluate', { expression: 'document.getElementById("cfgTabMat").focus()' });
  await send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'ArrowRight', code: 'ArrowRight', windowsVirtualKeyCode: 39 });
  await send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'ArrowRight', code: 'ArrowRight', windowsVirtualKeyCode: 39 });
  await sleep(900);
  d = JSON.parse(await evalJs(AFTER_TAB_KEY));
  check('方向键切到订单页签（aria-selected 跟随）', d.selected[1] === 'true' && d.selected[0] === 'false', JSON.stringify(d.selected));
  check('焦点落在订单页签上', d.activeId === 'cfgTabOrd', d.activeId);

  console.log('\n【弹窗 · 焦点进陷阱 / Esc 归还】');
  await send('Page.navigate', { url: BASE });
  await sleep(1800);
  await send('Runtime.evaluate', { expression: 'document.getElementById("helpBtn").focus(); document.getElementById("helpBtn").click();' });
  await sleep(700);
  d = JSON.parse(await evalJs(MODAL_PROBE));
  check('弹窗已打开', d.shown);
  check('打开后焦点进入弹窗', d.trapInside, d.focusId);
  // Tab 循环：从最后一个可聚焦元素按 Tab 应回到第一个
  await send('Runtime.evaluate', { expression: `
    (() => { const m=document.getElementById('helpModal');
      const f=[...m.querySelectorAll('button,a[href],input,select,textarea,[tabindex]:not([tabindex="-1"])')].filter(x=>!x.disabled);
      f[f.length-1].focus(); return f.length; })()` , returnByValue: true });
  await send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
  await send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
  await sleep(400);
  const inside = await evalJs('document.getElementById("helpModal").contains(document.activeElement)');
  check('Tab 焦点锁在弹窗内（未跑到背后页面）', inside === true, inside);

  await send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 });
  await send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 });
  await sleep(500);
  const after = JSON.parse(await evalJs(`JSON.stringify({
    shown: document.getElementById('helpModal').style.display,
    focusId: (document.activeElement && document.activeElement.id) || ''
  })`));
  check('Esc 关闭弹窗', after.shown === 'none', after.shown);
  check('关闭后焦点归还给「使用说明」按钮', after.focusId === 'helpBtn', after.focusId);

  console.log('\n【加载期与操作期 JS 报错】');
  check('运行期间零 JS 报错', errs.length === 0, JSON.stringify(errs.slice(0, 3)));

  ws.close(); chrome.kill();
  await sleep(300);
  console.log('\n结果：' + pass + ' 项通过，' + fail + ' 项失败');
  console.log(fail === 0 ? '=== 可访问性全部通过 ===' : '=== 存在失败 ===');
  process.exit(fail === 0 ? 0 : 1);
})();
