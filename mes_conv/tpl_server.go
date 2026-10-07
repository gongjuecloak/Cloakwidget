package main

// ============================================================================
// 模板基准（tplcheck.go）的接口层
// ============================================================================

import (
	"fmt"
	"net/http"
)

func reqLang(r *http.Request) string {
	return r.FormValue("lang")
}

// handlerTplStatus 模板表头基准状态，供配置中心展示
func handlerTplStatus(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	st := currentTplStatus()
	msg := L["tpl_status_none"]
	if st.Has {
		msg = fmt.Sprintf(L["tpl_status_ok"], st.Count, st.Saved)
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"has":      st.Has,
		"count":    st.Count,
		"saved_at": st.Saved,
		"sheet":    st.Sheet,
		"sample":   st.Sample,
		"file":     st.File,
		"message":  msg,
	})
}

// handlerTplReset 清除模板基准：下次转换会按当时的模板重新记录
func handlerTplReset(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	if err := clearTplBaseline(); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"has":     false,
		"message": msgs(lang)["msg_tpl_cleared"],
	})
}
