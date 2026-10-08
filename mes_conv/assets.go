package main

import _ "embed"

// 界面源码拆分在 webui/ 目录下，整目录内嵌进 exe。
// 运行时由 indexHTML() 把 CSS / JS 内联回单文件 HTML 再返回，
// 所以对外仍然只发一个 exe，页面也不产生任何外部请求。
//
// 拆分是为了可维护性：原来 3300 多行的单文件里 HTML/CSS/JS 全混在一起，
// 改一处只能靠全文搜索。
//
//go:embed webui/index.html
var embeddedIndex []byte

//go:embed webui/style.css
var embeddedCSS []byte

//go:embed webui/i18n.js
var embeddedI18n []byte

//go:embed webui/app.js
var embeddedApp []byte
