package main

import _ "embed"

// embeddedHTML 把界面内嵌进 exe。
// 运行时优先读 exe 同目录的 webui.html（方便临时改界面、免重编），
// 找不到才用这份内嵌版本——这样只发一个 exe 也能正常使用。
//
//go:embed webui.html
var embeddedHTML []byte
