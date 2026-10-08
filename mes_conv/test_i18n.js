// 校验界面四语词条完整性：
//  1) 四语 key 集合一致（不出现某语言漏词条）
//  2) JS 里用到的 t("xxx") / fmt("xxx") 都有定义
//
// 界面已拆成 webui/ 下多个文件，默认读同目录的 webui/；
// 也接受单个 webui.html（旧结构，向后兼容）。
const fs = require('fs');
const path = require('path');

const arg = process.argv[2] || path.join(__dirname, 'webui');
let js;

if (fs.existsSync(arg) && fs.statSync(arg).isDirectory()) {
  const read = n => fs.readFileSync(path.join(arg, n), 'utf8');
  const index = read('index.html');
  if (!index.includes('<!--@STYLE-->') || !index.includes('<!--@SCRIPT-->')) {
    console.error('index.html 里找不到 <!--@STYLE--> / <!--@SCRIPT--> 占位符');
    process.exit(1);
  }
  // 顺序与 Go 侧 assembleHTML 一致：i18n.js 在前，app.js 在后
  js = read('i18n.js') + '\n' + read('app.js');
} else {
  const html = fs.readFileSync(arg, 'utf8');
  const m = html.match(/<script>([\s\S]*?)<\/script>/);
  if (!m) { console.error('未找到 <script>'); process.exit(1); }
  js = m[1];
}

const start = js.indexOf('const STR =');
const relEnd = js.indexOf('\n};', start);
if (start < 0 || relEnd < 0) { console.error('未定位到 STR 定义'); process.exit(1); }
const src = js.slice(start, relEnd + 3);
const STR = new Function(src + '\nreturn STR;')();

// 本轮新增文案放在 STR2 overlay 里，运行时会并进 STR —— 校验也要跟着并一次
const s2Start = js.indexOf('const STR2 = {');
const s2End = js.indexOf('Object.keys(STR2)');
if (s2Start >= 0 && s2End > s2Start) {
  const s2src = js.slice(s2Start, js.lastIndexOf(';', s2End) + 1);
  const STR2 = new Function(s2src + '\nreturn STR2;')();
  Object.keys(STR2).forEach(l => { STR[l] = Object.assign(STR[l] || {}, STR2[l]); });
  console.log('已并入 STR2 overlay：' + Object.keys(STR2).length + ' 种语言');
} else {
  console.error('未定位到 STR2 overlay'); process.exit(1);
}

const langs = Object.keys(STR);
console.log('语言:', langs.join(', '));
const base = Object.keys(STR.zh);
let bad = 0;

langs.forEach(l => {
  const keys = Object.keys(STR[l]);
  const missing = base.filter(k => !(k in STR[l]));
  const extra = keys.filter(k => !(k in STR.zh));
  const line = '  ' + l.padEnd(5) + ' keys=' + String(keys.length).padStart(3);
  if (missing.length) { bad++; console.log(line + '  ❌ 缺少: ' + missing.join(', ')); }
  else if (extra.length) { console.log(line + '  ⚠ 多余: ' + extra.join(', ')); }
  else console.log(line + '  ✓ 与 zh 一致');
});

// JS 中实际用到的 key
const used = new Set();
for (const mm of js.matchAll(/\bt\("([A-Za-z_][A-Za-z0-9_]*)"\)/g)) used.add(mm[1]);
for (const mm of js.matchAll(/\bfmt\("([A-Za-z_][A-Za-z0-9_]*)"/g)) used.add(mm[1]);
const undef = [...used].filter(k => !(k in STR.zh));
console.log('\nJS 引用的词条数:', used.size);
if (undef.length) { bad++; console.log('  ❌ 未定义: ' + undef.join(', ')); }
else console.log('  ✓ 全部已定义');

// TEXT_MAP 指向的 key 是否存在
const tmStart = js.indexOf('const TEXT_MAP = {');
const tmEnd = js.indexOf('};', tmStart);
const tmSrc = js.slice(tmStart, tmEnd + 2);
const TEXT_MAP = new Function(tmSrc + '\nreturn TEXT_MAP;')();

// 从 openIdx（指向 '{'）起做花括号配对，返回闭合 '}' 的下标（跳过字符串字面量）
function matchBrace(s, openIdx) {
  let depth = 0, q = null;
  for (let i = openIdx; i < s.length; i++) {
    const c = s[i];
    if (q) {                       // 字符串内
      if (c === '\\') { i++; continue; }
      if (c === q) q = null;
      continue;
    }
    if (c === '"' || c === "'" || c === '`') { q = c; continue; }
    if (c === '{') depth++;
    else if (c === '}') { depth--; if (depth === 0) return i; }
  }
  return -1;
}

// 新增的 id -> key 文案以 Object.assign(TEXT_MAP, {...}) 追加，同样要并进来
const oaStart = js.indexOf('Object.assign(TEXT_MAP, {');
if (oaStart >= 0) {
  const oaObjStart = js.indexOf('{', oaStart);
  const oaObjEnd = matchBrace(js, oaObjStart);
  if (oaObjEnd > oaObjStart) {
    const oaSrc = js.slice(oaObjStart, oaObjEnd + 1);
    const extra = new Function('return ' + oaSrc + ';')();
    Object.assign(TEXT_MAP, extra);
    console.log('已并入 TEXT_MAP 追加项：' + Object.keys(extra).length + ' 项');
  } else {
    console.error('TEXT_MAP 追加项花括号未闭合'); bad++;
  }
}

const tmMissing = Object.entries(TEXT_MAP).filter(([id, k]) => !(k in STR.zh));
if (tmMissing.length) { bad++; console.log('  ❌ TEXT_MAP 指向未定义的 key: ' + JSON.stringify(tmMissing)); }
else console.log('  ✓ TEXT_MAP 全部有效（' + Object.keys(TEXT_MAP).length + ' 项）');

console.log('\n' + (bad ? '=== 存在 ' + bad + ' 处问题 ===' : '=== 四语词条全部通过 ==='));
process.exit(bad ? 1 : 0);
