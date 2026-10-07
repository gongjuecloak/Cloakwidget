// 校验 webui.html 四语词条完整性：
//  1) 四语 key 集合一致（不出现某语言漏词条）
//  2) JS 里用到的 t("xxx") / fmt("xxx") 都有定义
const fs = require('fs');
const path = process.argv[2] || 'webui.html';
const html = fs.readFileSync(path, 'utf8');
const m = html.match(/<script>([\s\S]*?)<\/script>/);
if (!m) { console.error('未找到 <script>'); process.exit(1); }
const js = m[1];

const start = js.indexOf('const STR =');
const relEnd = js.indexOf('\n};', start);
if (start < 0 || relEnd < 0) { console.error('未定位到 STR 定义'); process.exit(1); }
const src = js.slice(start, relEnd + 3);
const STR = new Function(src + '\nreturn STR;')();

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
const tmMissing = Object.entries(TEXT_MAP).filter(([id, k]) => !(k in STR.zh));
if (tmMissing.length) { bad++; console.log('  ❌ TEXT_MAP 指向未定义的 key: ' + JSON.stringify(tmMissing)); }
else console.log('  ✓ TEXT_MAP 全部有效（' + Object.keys(TEXT_MAP).length + ' 项）');

console.log('\n' + (bad ? '=== 存在 ' + bad + ' 处问题 ===' : '=== 四语词条全部通过 ==='));
process.exit(bad ? 1 : 0);
