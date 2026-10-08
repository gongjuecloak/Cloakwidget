package main

import "sync"

// ============================================================================
// 两个模块共用的文案表
//
// 物料档案用 logI18n（server.go），订单/工单用 ordersI18n（orders_i18n.go）。
// 但「源档格式识别」「批量转换」「重复检查」「模板变化」「系统设置」这些提示
// 两边都要用 —— 放在这里维护一份，新增词条只改这一个文件，不用两个模块各写一遍。
//
// 覆盖顺序：模块自己的同名 key 优先（mergeMsgs），所以这里放的是「兜底共用值」。
// ============================================================================

var commonI18n = map[string]map[string]string{
	"zh": {
		// ---- 源档识别 ----
		"note_src_fmt":    "源档格式：%s",
		"note_date_fixed": "老 .xls 的日期序列号已还原：%d 处",
		"note_sheets":     "工作表「%s」，表头在第 %d 行",
		"err_read_failed": "读取源档失败",
		"err_no_rows":     "源档没有数据行",
		"err_xls_read":    "这个老 .xls 文件读不出来（可能是 BIFF5 或加密文件），请在 Excel 里另存为 .xlsx 后重试",
		"err_csv_read":    "CSV 解析失败",
		"err_no_source":   "没有可用的源文件",

		// ---- 批量 ----
		"note_batch":      "批量转换：%d 个文件，合计 %d 行",
		"note_batch_name": "批量（%d 个文件）",
		"note_batch_item": "· %s：%d 行",
		"note_merge":      "多个源文件已合并为一个输出",
		"note_split":      "每个源文件各出一个输出文件",

		// ---- 重复检查 ----
		"note_dup_none":  "重复检查（%s）：未发现重复",
		"note_dup_total": "重复检查（%s）：发现 %d 组重复，共 %d 行",
		"warn_dup":       "重复 %s：%s（第 %d 行与第 %d 行重复）",
		"sum_dup":        "%s 重复：%s（共 %d 行）",

		// ---- Excel 错误值 ----
		"warn_excel_err": "Excel 错误值「%s」：源档这一格是坏公式（有人用 = 开头写内容、或引用了已删除的单元格），会原样进 MES",
		"sum_excel_err":  "%s：%d 处 Excel 错误值",

		// ---- 模板列变化 ----
		"note_tpl_first":   "已记录模板表头基准（%d 列）。以后模板变了会自动提示",
		"note_tpl_ok":      "模板表头与基准一致（%d 列）",
		"warn_tpl_new":     "模板新增列「%s」：配置里还没有映射，这个字段会留空",
		"warn_tpl_missing": "模板少了列「%s」：配置里仍在映射它",
		"warn_tpl_order":   "模板列顺序有变化：%s",
		"warn_tpl_count":   "模板列数由 %d 变为 %d",
		"msg_tpl_reset":    "已把当前模板表头记为基准",
		"msg_tpl_cleared":  "已清除模板基准，下次转换会重新记录当前模板",
		"tpl_status_none":  "尚未记录模板基准（下次转换会自动记录）",
		"tpl_status_ok":    "基准：%d 列，记录于 %s",

		// ---- 系统设置 ----
		"msg_autostart_on":      "已设置开机自启（下次登录自动在右下角运行）",
		"msg_autostart_off":     "已关闭开机自启",
		"msg_autostart_fail":    "设置开机自启失败：%s",
		"msg_lan_on":            "已允许局域网访问，保存后需重启程序生效：%s",
		"msg_lan_off":           "已关闭局域网访问，重启后仅本机可访问",
		"msg_pwd_saved":         "口令已保存",
		"msg_pwd_cleared":       "已取消口令保护",
		"err_pwd_needed":        "需要口令",
		"err_pwd_wrong":         "口令不正确",
		"err_pwd_empty":         "口令不能为空",
		"msg_diag_ok":           "排障包已生成：%s",
		"msg_diag_fail":         "生成排障包失败：%s",
		"msg_restart":           "正在重新启动…",
		"msg_update_latest":     "已是最新版本（%s）",
		"msg_update_new":        "发现新版本 %s（当前 %s）",
		"msg_update_fail":       "检查更新失败：%s",
		"msg_update_applied":    "已更新到 %s，程序即将重启",
		"msg_update_apply_fail": "更新失败：%s",
		"note_lan_addr":         "局域网访问地址：%s",
	},
	"zht": {
		"note_src_fmt":    "來源檔案格式：%s",
		"note_date_fixed": "舊 .xls 的日期序列號已還原：%d 處",
		"note_sheets":     "工作表「%s」，表頭在第 %d 行",
		"err_read_failed": "讀取來源檔案失敗",
		"err_no_rows":     "來源檔案沒有資料列",
		"err_xls_read":    "這個舊 .xls 檔案讀不出來（可能是 BIFF5 或加密檔案），請在 Excel 另存為 .xlsx 後重試",
		"err_csv_read":    "CSV 解析失敗",
		"err_no_source":   "沒有可用的來源檔案",

		"note_batch":      "批次轉換：%d 個檔案，合計 %d 列",
		"note_batch_name": "批次（%d 個檔案）",
		"note_batch_item": "· %s：%d 列",
		"note_merge":      "多個來源檔案已合併為一個輸出",
		"note_split":      "每個來源檔案各出一個輸出檔案",

		"note_dup_none":  "重複檢查（%s）：未發現重複",
		"note_dup_total": "重複檢查（%s）：發現 %d 組重複，共 %d 列",
		"warn_dup":       "重複 %s：%s（第 %d 列與第 %d 列重複）",
		"sum_dup":        "%s 重複：%s（共 %d 列）",

		"warn_excel_err": "Excel 錯誤值「%s」：來源檔這一格是壞公式（有人用 = 開頭寫內容、或引用了已刪除的儲存格），會原樣進 MES",
		"sum_excel_err":  "%s：%d 處 Excel 錯誤值",

		"note_tpl_first":   "已記錄模板表頭基準（%d 欄）。以後模板變更會自動提示",
		"note_tpl_ok":      "模板表頭與基準一致（%d 欄）",
		"warn_tpl_new":     "模板新增欄「%s」：設定裡還沒有對應，這個欄位會留空",
		"warn_tpl_missing": "模板少了欄「%s」：設定裡仍在對應它",
		"warn_tpl_order":   "模板欄位順序有變化：%s",
		"warn_tpl_count":   "模板欄數由 %d 變為 %d",
		"msg_tpl_reset":    "已把目前模板表頭記為基準",
		"msg_tpl_cleared":  "已清除模板基準，下次轉換會重新記錄目前模板",
		"tpl_status_none":  "尚未記錄模板基準（下次轉換會自動記錄）",
		"tpl_status_ok":    "基準：%d 欄，記錄於 %s",

		"msg_autostart_on":      "已設定開機自啟（下次登入自動在右下角執行）",
		"msg_autostart_off":     "已關閉開機自啟",
		"msg_autostart_fail":    "設定開機自啟失敗：%s",
		"msg_lan_on":            "已允許區域網路存取，儲存後需重新啟動程式才生效：%s",
		"msg_lan_off":           "已關閉區域網路存取，重新啟動後僅本機可存取",
		"msg_pwd_saved":         "密碼已儲存",
		"msg_pwd_cleared":       "已取消密碼保護",
		"err_pwd_needed":        "需要密碼",
		"err_pwd_wrong":         "密碼不正確",
		"err_pwd_empty":         "密碼不能為空",
		"msg_diag_ok":           "排障包已產生：%s",
		"msg_diag_fail":         "產生排障包失敗：%s",
		"msg_restart":           "正在重新啟動…",
		"msg_update_latest":     "已是最新版本（%s）",
		"msg_update_new":        "發現新版本 %s（目前 %s）",
		"msg_update_fail":       "檢查更新失敗：%s",
		"msg_update_applied":    "已更新到 %s，程式即將重新啟動",
		"msg_update_apply_fail": "更新失敗：%s",
		"note_lan_addr":         "區域網路存取網址：%s",
	},
	"vi": {
		"note_src_fmt":    "Định dạng tệp nguồn: %s",
		"note_date_fixed": "Đã khôi phục %d ô ngày dạng số tuần tự của .xls cũ",
		"note_sheets":     "Trang tính \"%s\", dòng tiêu đề ở dòng %d",
		"err_read_failed": "Đọc tệp nguồn thất bại",
		"err_no_rows":     "Tệp nguồn không có dòng dữ liệu",
		"err_xls_read":    "Không đọc được tệp .xls cũ này (có thể là BIFF5 hoặc tệp mã hóa). Hãy lưu thành .xlsx rồi thử lại",
		"err_csv_read":    "Phân tích CSV thất bại",
		"err_no_source":   "Không có tệp nguồn nào dùng được",

		"note_batch":      "Chuyển hàng loạt: %d tệp, tổng %d dòng",
		"note_batch_name": "Hàng loạt (%d tệp)",
		"note_batch_item": "· %s: %d dòng",
		"note_merge":      "Đã gộp nhiều tệp nguồn thành một tệp đầu ra",
		"note_split":      "Mỗi tệp nguồn xuất ra một tệp riêng",

		"note_dup_none":  "Kiểm tra trùng lặp (%s): không phát hiện",
		"note_dup_total": "Kiểm tra trùng lặp (%s): %d nhóm, tổng %d dòng",
		"warn_dup":       "Trùng %s: %s (dòng %d và dòng %d)",
		"sum_dup":        "Trùng %s: %s (tổng %d dòng)",

		"warn_excel_err": "Giá trị lỗi Excel \"%s\": ô này trong tệp nguồn là công thức hỏng (bắt đầu bằng \"=\" hoặc tham chiếu ô đã xóa), sẽ được đưa nguyên vào MES",
		"sum_excel_err":  "%s: %d ô lỗi Excel",

		"note_tpl_first":   "Đã lưu mốc tiêu đề mẫu (%d cột). Lần sau mẫu đổi sẽ tự cảnh báo",
		"note_tpl_ok":      "Tiêu đề mẫu khớp với mốc (%d cột)",
		"warn_tpl_new":     "Mẫu có cột mới \"%s\": cấu hình chưa có ánh xạ nên cột này sẽ để trống",
		"warn_tpl_missing": "Mẫu thiếu cột \"%s\": cấu hình vẫn đang ánh xạ cột này",
		"warn_tpl_order":   "Thứ tự cột của mẫu đã thay đổi: %s",
		"warn_tpl_count":   "Số cột mẫu đổi từ %d thành %d",
		"msg_tpl_reset":    "Đã lấy tiêu đề mẫu hiện tại làm mốc",
		"msg_tpl_cleared":  "Đã xóa mốc mẫu, lần chuyển tiếp theo sẽ ghi lại mẫu hiện tại",
		"tpl_status_none":  "Chưa lưu mốc mẫu (lần chuyển tiếp theo sẽ tự lưu)",
		"tpl_status_ok":    "Mốc: %d cột, lưu lúc %s",

		"msg_autostart_on":      "Đã bật khởi động cùng Windows",
		"msg_autostart_off":     "Đã tắt khởi động cùng Windows",
		"msg_autostart_fail":    "Bật khởi động cùng Windows thất bại: %s",
		"msg_lan_on":            "Đã cho phép truy cập trong mạng LAN, cần khởi động lại để có hiệu lực: %s",
		"msg_lan_off":           "Đã tắt truy cập LAN, sau khi khởi động lại chỉ máy này truy cập được",
		"msg_pwd_saved":         "Đã lưu mật khẩu",
		"msg_pwd_cleared":       "Đã bỏ bảo vệ bằng mật khẩu",
		"err_pwd_needed":        "Cần mật khẩu",
		"err_pwd_wrong":         "Mật khẩu không đúng",
		"err_pwd_empty":         "Mật khẩu không được để trống",
		"msg_diag_ok":           "Đã tạo gói chẩn đoán: %s",
		"msg_diag_fail":         "Tạo gói chẩn đoán thất bại: %s",
		"msg_restart":           "Đang khởi động lại…",
		"msg_update_latest":     "Đã là phiên bản mới nhất (%s)",
		"msg_update_new":        "Có phiên bản mới %s (hiện tại %s)",
		"msg_update_fail":       "Kiểm tra cập nhật thất bại: %s",
		"msg_update_applied":    "Đã cập nhật lên %s, chương trình sẽ khởi động lại",
		"msg_update_apply_fail": "Cập nhật thất bại: %s",
		"note_lan_addr":         "Địa chỉ truy cập LAN: %s",
	},
	"en": {
		"note_src_fmt":    "Source format: %s",
		"note_date_fixed": "Restored %d date serial cells from the legacy .xls",
		"note_sheets":     "Sheet \"%s\", header on row %d",
		"err_read_failed": "Failed to read the source file",
		"err_no_rows":     "The source file has no data rows",
		"err_xls_read":    "This legacy .xls cannot be read (possibly BIFF5 or encrypted). Please save it as .xlsx and retry",
		"err_csv_read":    "CSV parsing failed",
		"err_no_source":   "No usable source file",

		"note_batch":      "Batch mode: %d files, %d rows in total",
		"note_batch_name": "Batch (%d files)",
		"note_batch_item": "· %s: %d rows",
		"note_merge":      "Multiple sources merged into a single output",
		"note_split":      "One output file per source file",

		"note_dup_none":  "Duplicate check (%s): none found",
		"note_dup_total": "Duplicate check (%s): %d groups, %d rows",
		"warn_dup":       "Duplicate %s: %s (row %d and row %d)",
		"sum_dup":        "Duplicate %s: %s (%d rows)",

		"warn_excel_err": "Excel error value \"%s\": this cell is a broken formula in the source (a note starting with \"=\", or a deleted reference) and will be copied into MES as-is",
		"sum_excel_err":  "%s: %d Excel error cells",

		"note_tpl_first":   "Baseline template header saved (%d columns); future changes will be flagged",
		"note_tpl_ok":      "Template header matches the baseline (%d columns)",
		"warn_tpl_new":     "New template column \"%s\": not mapped in config yet, will stay empty",
		"warn_tpl_missing": "Template column \"%s\" is gone, but config still maps it",
		"warn_tpl_order":   "Template column order changed: %s",
		"warn_tpl_count":   "Template column count changed from %d to %d",
		"msg_tpl_reset":    "Current template header saved as the new baseline",
		"msg_tpl_cleared":  "Template baseline cleared; the next conversion will record the current template",
		"tpl_status_none":  "No template baseline yet (the next conversion will record one)",
		"tpl_status_ok":    "Baseline: %d columns, saved %s",

		"msg_autostart_on":      "Launch at startup enabled",
		"msg_autostart_off":     "Launch at startup disabled",
		"msg_autostart_fail":    "Failed to change startup setting: %s",
		"msg_lan_on":            "LAN access allowed. Restart the program to take effect: %s",
		"msg_lan_off":           "LAN access disabled; after restart only this machine can connect",
		"msg_pwd_saved":         "Password saved",
		"msg_pwd_cleared":       "Password protection removed",
		"err_pwd_needed":        "Password required",
		"err_pwd_wrong":         "Wrong password",
		"err_pwd_empty":         "Password cannot be empty",
		"msg_diag_ok":           "Diagnostic bundle created: %s",
		"msg_diag_fail":         "Failed to create the diagnostic bundle: %s",
		"msg_restart":           "Restarting…",
		"msg_update_latest":     "Already up to date (%s)",
		"msg_update_new":        "New version %s available (current %s)",
		"msg_update_fail":       "Update check failed: %s",
		"msg_update_applied":    "Updated to %s, the app will restart shortly",
		"msg_update_apply_fail": "Update failed: %s",
		"note_lan_addr":         "LAN address: %s",
	},
}

// commonMsgs 取共用文案表；语言未知时回退中文
func commonMsgs(lang string) map[string]string {
	if m, ok := commonI18n[lang]; ok {
		return m
	}
	return commonI18n["zh"]
}

// mergeMsgs 把共用文案叠加到模块文案上：模块自己的同名 key 优先。
// 结果按语言缓存，避免每次转换都重建 map。
var mergedMsgsCache = map[string]map[string]string{}
var mergedMsgsMu sync.Mutex

func mergeMsgs(cacheKey string, module, common map[string]string) map[string]string {
	mergedMsgsMu.Lock()
	defer mergedMsgsMu.Unlock()
	if m, ok := mergedMsgsCache[cacheKey]; ok {
		return m
	}
	out := make(map[string]string, len(module)+len(common))
	for k, v := range common {
		out[k] = v
	}
	for k, v := range module {
		out[k] = v
	}
	mergedMsgsCache[cacheKey] = out
	return out
}
