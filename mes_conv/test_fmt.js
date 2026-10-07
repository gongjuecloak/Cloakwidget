// 验证 webui.html 的 fmt() 占位符替换
const fs = require('fs');
const src = fs.readFileSync('webui.html', 'utf8');

const tSrc = src.match(/function t\(k\)\{[^\n]*\}/);
const fmtSrc = src.match(/function fmt\(k,args\)\{[\s\S]*?\n\}/);
if (!tSrc || !fmtSrc) { console.error('EXTRACT FAIL', !!tSrc, !!fmtSrc); process.exit(1); }

const STR = {
  zh: {
    check_summary: "%d 处需关注",
    check_ok: "未发现问题 ✓",
    hdr_loaded: "表头已读取：%d 列",
    confirm_save: F => "发现 " + F + " 处配置问题：\n\n%S\n\n仍要保存吗？",
    v_dup_target: (i, s) => "第 " + i + " 行：目标列「" + s + "」重复了",
  },
  zht: {
    check_summary: "%d 處需關注",
    check_ok: "未發現問題 ✓",
    hdr_loaded: "表頭已讀取：%d 欄",
    confirm_save: F => "發現 " + F + " 處設定問題：\n\n%S\n\n仍要儲存嗎？",
    v_dup_target: (i, s) => "第 " + i + " 欄：目標欄「" + s + "」重複了",
  },
  vi: { check_summary: "%d mục cần lưu ý" },
  en: { check_summary: "%d item(s) to review" },
};

let currentLang = 'zh';
eval(tSrc[0] + "\n" + fmtSrc[0]);

let fail = 0;
function check(name, got, want) {
  const ok = got === want;
  if (!ok) fail++;
  console.log((ok ? 'PASS' : 'FAIL') + '  ' + name + '\n        got  = ' + JSON.stringify(got) + (ok ? '' : '\n        want = ' + JSON.stringify(want)));
}

for (const [lang, want] of [['zh', '7 处需关注'], ['zht', '7 處需關注'], ['vi', '7 mục cần lưu ý'], ['en', '7 item(s) to review']]) {
  currentLang = lang;
  check('check_summary[' + lang + ']', fmt('check_summary', [7]), want);
}

currentLang = 'zht';
check('hdr_loaded 单占位', fmt('hdr_loaded', [64]), '表頭已讀取：64 欄');
check('函数模板仍可用', fmt('confirm_save', [3]).replace('%S', 'A\nB'), '發現 3 處設定問題：\n\nA\nB\n\n仍要儲存嗎？');
check('函数模板带 2 参', fmt('v_dup_target', [5, '物料编码']), '第 5 欄：目標欄「物料编码」重複了');
check('check_ok 无占位', fmt('check_ok', []), '未發現問題 ✓');
check('未知 key 原样返回', fmt('no_such_key', [1]), 'no_such_key');
check('参数不足不留空', fmt('hdr_loaded', []), '表頭已讀取： 欄');

console.log(fail === 0 ? '\n全部通过 ✓' : '\n失败 ' + fail + ' 项');
process.exit(fail === 0 ? 0 : 1);
