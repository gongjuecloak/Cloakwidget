// 验证「清除已选文件」：用无头 Chrome + CDP 真实触发 change / 点击 ×
// 零依赖：Node 22 自带 fetch 与 WebSocket
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const PORT = 9333;
const HERE = __dirname;
const PAGE = 'file:///' + path.join(HERE, 'mes_conv', 'webui.html').replace(/\\/g, '/');

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const TEST_JS = `(() => {
  const out = [];
  const push = (k, v) => out.push([k, String(v)]);

  function pick(inputId, nameId, fileName) {
    const input = document.getElementById(inputId);
    const nameEl = document.getElementById(nameId);
    const f = new File(['x'], fileName,
      { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' });
    const dt = new DataTransfer();
    dt.items.add(f);
    input.files = dt.files;
    input.dispatchEvent(new Event('change', { bubbles: true }));
    return { input, nameEl };
  }

  // ---------- 物料档案：源文件 ----------
  let el = document.getElementById('srcName');
  push('T1 初始文案', el.textContent.trim());
  push('T2 初始无 ×', !el.querySelector('.fn-clear'));

  let r = pick('srcFile', 'srcName', '20261006 匯入 577筆.xlsx');
  push('T3 选中后含文件名', r.nameEl.textContent.indexOf('20261006 匯入 577筆.xlsx') >= 0);
  push('T4 选中后出现 ×', !!r.nameEl.querySelector('.fn-clear'));
  push('T5 × 带 title', (r.nameEl.querySelector('.fn-clear') || {}).title || '');
  push('T6 data-chosen=1', r.nameEl.dataset.chosen);

  r.nameEl.querySelector('.fn-clear').click();
  push('T7 点击 × 后文案', r.nameEl.textContent.trim());
  push('T8 点击 × 后无 ×', !r.nameEl.querySelector('.fn-clear'));
  push('T9 input.files 已清空', r.input.files.length === 0);
  push('T10 data-chosen=0', r.nameEl.dataset.chosen);

  // T11 清除后重选同一个文件，仍然能触发（这是清除的附带收益）
  r = pick('srcFile', 'srcName', '20261006 匯入 577筆.xlsx');
  push('T11 重选同一文件仍生效', r.nameEl.textContent.indexOf('577筆') >= 0);
  r.nameEl.querySelector('.fn-clear').click();

  // ---------- 物料档案：模板 ----------
  el = document.getElementById('tplName');
  r = pick('tplFile', 'tplName', '物料档案导入模版 (3).xlsx');
  push('T12 模板选中后出现 ×', !!r.nameEl.querySelector('.fn-clear'));
  r.nameEl.querySelector('.fn-clear').click();
  push('T13 模板清除后文案', r.nameEl.textContent.trim());
  push('T14 模板 input 已清空', r.input.files.length === 0);

  // ---------- 订单模块：订单 / 工单 ----------
  el = document.getElementById('ordSrcName');
  r = pick('ordSrcFile', 'ordSrcName', '訂單資料.xlsx');
  push('T15 订单选中后出现 ×', !!r.nameEl.querySelector('.fn-clear'));
  r.nameEl.querySelector('.fn-clear').click();
  push('T16 订单清除后文案', r.nameEl.textContent.trim());

  el = document.getElementById('ordWorkName');
  r = pick('ordWorkFile', 'ordWorkName', '工單資料.xlsx');
  push('T17 工单选中后出现 ×', !!r.nameEl.querySelector('.fn-clear'));
  r.nameEl.querySelector('.fn-clear').click();
  push('T18 工单清除后文案', r.nameEl.textContent.trim());

  // ---------- 四语：× 的提示随语言变化 ----------
  const titles = {};
  ['zh', 'zht', 'vi', 'en'].forEach((L) => {
    currentLang = L;
    const fake = document.getElementById('srcName');
    setFileName('srcName', new File(['x'], 'a.xlsx'));
    titles[L] = (fake.querySelector('.fn-clear') || {}).title || '';
  });
  currentLang = 'zh';
  setFileName('srcName', null);
  push('T19 四语 title 各不相同', new Set(Object.values(titles)).size === 4);
  push('T20 title 内容', JSON.stringify(titles));

  // ---------- 不能用原生 setter 之外的方式绕过；确认没有残留监听重复绑定 ----------
  r = pick('srcFile', 'srcName', 'b.xlsx');
  push('T21 重复清除后仍能重选', (() => {
    r.nameEl.querySelector('.fn-clear').click();
    r.nameEl.querySelector('.fn-clear');
    const again = pick('srcFile', 'srcName', 'c.xlsx');
    return again.nameEl.textContent.indexOf('c.xlsx') >= 0;
  })());

  return JSON.stringify(out);
})()`;

(async () => {
  const prof = path.join(HERE, '_chrome_prof_cdp');
  fs.mkdirSync(prof, { recursive: true });

  const chrome = spawn(CHROME, [
    '--headless=new', '--disable-gpu', '--no-sandbox', '--no-first-run',
    '--remote-debugging-port=' + PORT,
    '--user-data-dir=' + prof,
    '--window-size=1280,900',
    PAGE,
  ], { stdio: 'ignore' });

  let wsUrl = null;
  for (let i = 0; i < 50; i++) {
    try {
      const j = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
      const p = j.find((t) => t.type === 'page' && t.url.startsWith('file://'));
      if (p && p.webSocketDebuggerUrl) { wsUrl = p.webSocketDebuggerUrl; break; }
    } catch (e) { /* 还没起来 */ }
    await sleep(250);
  }
  if (!wsUrl) {
    console.log('✗ CDP 未就绪，无法测试');
    chrome.kill();
    process.exit(1);
  }

  const ws = new WebSocket(wsUrl);
  const reply = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('CDP 超时')), 20000);
    ws.onopen = () => ws.send(JSON.stringify({
      id: 1, method: 'Runtime.evaluate',
      params: { expression: TEST_JS, returnByValue: true, awaitPromise: true },
    }));
    ws.onmessage = (ev) => {
      const m = JSON.parse(ev.data);
      if (m.id === 1) { clearTimeout(timer); resolve(m); }
    };
    ws.onerror = (e) => { clearTimeout(timer); reject(new Error('WS 错误')); };
  });

  let ok = true;
  const res = reply.result;
  if (res.exceptionDetails) {
    console.log('✗ 页面 JS 抛错：', JSON.stringify(res.exceptionDetails.exception || res.exceptionDetails));
    ok = false;
  } else {
    const rows = JSON.parse(res.result.value);
    const expect = {
      'T1 初始文案': /尚未選擇檔案|尚未选择文件|Chưa chọn|No file selected/,
      'T2 初始无 ×': /true/,
      'T3 选中后含文件名': /true/,
      'T4 选中后出现 ×': /true/,
      'T6 data-chosen=1': /^1$/,
      'T8 点击 × 后无 ×': /true/,
      'T9 input.files 已清空': /true/,
      'T10 data-chosen=0': /^0$/,
      'T11 重选同一文件仍生效': /true/,
      'T12 模板选中后出现 ×': /true/,
      'T14 模板 input 已清空': /true/,
      'T15 订单选中后出现 ×': /true/,
      'T17 工单选中后出现 ×': /true/,
      'T19 四语 title 各不相同': /true/,
      'T21 重复清除后仍能重选': /true/,
    };
    for (const [k, v] of rows) {
      const want = expect[k];
      let pass = true;
      if (want) pass = want.test(v);
      // 「清除后文案」应回到未选择态
      if (/清除后文案/.test(k)) pass = /尚未選擇檔案|尚未选择文件|Chưa chọn|No file selected/.test(v);
      console.log(`${pass ? 'PASS' : 'FAIL'}  ${k}  = ${v}`);
      if (!pass) ok = false;
    }
  }

  ws.close();
  chrome.kill();
  await sleep(300);
  console.log('\n结果：', ok ? '清除选择功能全部通过 ✓' : '存在问题 ✗');
  process.exit(ok ? 0 : 1);
})();
