const STR = {
  zh:{
    home_title:"请选择要使用的模块", home_desc:"两个模块各自独立，配置与输出互不影响",
    mod_mat_name:"物料档案", mod_mat_desc:"把鼎新 ERP 导出的物料档（INVMB）转换成 MES 的「物料档案导入模板」，自动做字段映射、单位归一与预检。",
    mod_mat_t1:"字段映射", mod_mat_t2:"单位归一", mod_mat_t3:"导入前预检", mod_mat_go:"进入 →",
    mod_ord_name:"订单 / 工单", mod_ord_desc:"把鼎新 ERP 导出的订单档与製令（工单）档，转换成 MES 的「销售订单主表 / 销售订单明细 / 工单」三张表，自动回填销售订单行号与打标判定。",
    mod_ord_t1:"主表 + 明细 + 工单", mod_ord_t2:"行号回填", mod_ord_t3:"打标判定", mod_ord_go:"进入 →",
    back_btn:"← 返回",
    cfg_title:"配置中心", cfg_subtitle:"字段映射与转换规则的维护页面",
    cfg_desc:"两个模块的字段映射与规则都在这里维护。选文件、转换这些日常操作在各自的模块页面完成，这里只改配置。",
    cfg_entry_name:"配置中心",
    cfg_entry_desc:"字段映射、单位表、字典表、表头别名都在这里维护；操作页面只负责选文件与转换",
    cfg_entry_go:"进入 →", cfg_tab_mat:"物料档案", cfg_tab_ord:"订单 / 工单", cfg_nav_btn:"⚙ 配置",
    cfg_load_fail:"配置读取失败，已禁止保存配置，以免覆盖服务端已有配置。请检查服务后刷新页面。", cfg_leave_confirm:"配置有未保存的修改，确定离开吗？",
    err_timeout:"转换超时（超过 10 分钟）。文件可能过大，可拆分后再试，或查看 out\\ 目录下是否已生成部分结果。",
    err_aborted:"请求已中断。", err_offline:"连不上本地服务，请确认程序仍在运行（看右下角托盘图标），然后刷新页面重试。",
    ord_app_title:"MES 订单 / 工单转换工具", ord_app_subtitle:"鼎新 ERP 订单·製令档 → MES 导入表，一键转换",
    ord_step_source:"订单文件（可选）", ord_step_source_desc:"鼎新 ERP 导出的订单档（.xlsx），用于生成销售订单主表与明细",
    ord_step_work:"工单文件（可选）", ord_step_work_desc:"鼎新 ERP 导出的製令（工单）档（.xlsx），用于生成工单表",
    ord_choose_file:"选择文件", ord_drop_hint:"也可以直接把文件拖到这里",
    ord_start_btn:"开始转换", ord_converting:"正在转换，请稍候…",
    ord_no_file:"请先选择订单文件或工单文件（至少一个）",
    ord_result_title:"转换完成！", ord_result_desc:"文件已生成，点击下载即可导入 MES。",
    ord_download:"下载结果文件", ord_open_folder:"打开输出文件夹",
    ord_check_title:"预检结果", ord_history_title:"最近转换", ord_log_title:"转换日志",
    ord_copy_log:"复制日志", ord_view_log:"查看日志",
    ord_hist_none:"（暂无记录）", ord_hist_bad:"有问题", ord_hist_ok:"正常", ord_hist_dl:"下载", ord_hist_view:"查看问题",
    ord_all_ok:"未发现问题 ✓", ord_check_summary:"%d 处需要关注",
    ord_advanced_title:"高级设置（管理员）",
    ord_advanced_desc:"订单模块的字段映射与判定规则。所有设置都以表单填写，无需写代码。",
    ord_cfg_reload:"载入当前配置", ord_cfg_save:"保存配置",
    ord_cfg_saved:"配置已保存", ord_cfg_fail:"保存失败：",
    ord_block_map:"字段映射",
    ord_block_map_hint:"每行 = 一个输出列：源列（可留空表示不导出）+ 源档为空时填入的默认值。表格行序就是输出列顺序。",
    ord_th_target:"输出列", ord_th_source:"源列", ord_th_default:"默认值", ord_th_op:"操作", ord_add_row:"+ 添加一列",
    ord_block_tag:"打标判定", ord_block_tag_hint:"产品编码以任一前缀开头 → 「是」，否则「否」",
    ord_tag_column:"输出列", ord_tag_source:"判断依据列", ord_tag_prefixes:"前缀（逗号分隔）", ord_tag_yes:"命中填", ord_tag_no:"未命中填",
    ord_block_sw:"运行选项",
    ord_sw_header:"输出表头带必填星号（* 列名）", ord_sw_autoline:"工单的销售订单行号从订单明细自动匹配",
    ord_sw_filtersum:"过滤小计 / 合计行", ord_sw_validate:"转换前做数据校验",
    ord_block_alias:"表头别名表", ord_block_alias_hint:"源档表头五花八门时靠它识别。匹配时已做简繁归一，写简写繁都认。",
    ord_cat_order_master:"销售订单主表", ord_cat_order_detail:"销售订单明细", ord_cat_work_order:"工单",
    ord_progress:"已处理 %d / %d 行", ord_tag_disabled:"（未启用打标）",
    app_title:"MES 物料档案转换工具", app_subtitle:"鼎新 ERP 物料档 → MES 导入模板，一键转换",
    lang_label:"语言",
    step_source:"第 1 步 · 选择源文件", step_source_desc:"选择从鼎新 ERP 导出的物料档（.xlsx）",
    step_template:"第 2 步 · 选择模板", step_template_desc:"选择 MES 的「物料档案导入模板」（.xlsx）",
    drop_hint:"也可以直接把文件拖到这里",
    choose_file:"选择文件", no_file:"尚未选择文件", file_chosen:"已选择：", clear_file:"清除选择",
    start_btn:"开始转换", converting:"正在转换，请稍候…",
    status_ready:"请选择源文件和模板后开始", status_pick:"请先选择两个文件",
    result_title:"转换完成！", result_desc:"文件已生成，点击下载即可导入 MES。",
    download_btn:"下载结果文件", open_folder:"打开输出文件夹", opening:"正在打开…",
    log_title:"转换日志", view_log:"查看日志", hide_log:"收起日志", copy_log:"复制日志",
    check_title:"预检结果", check_ok:"未发现问题 ✓", check_summary:"%d 处需关注",
    issue_row:"行号", issue_col:"列", issue_msg:"说明",
    check_lang_hint:"提示内容按转换时的语言生成；如需其他语言，请重新转换。",
    history_title:"最近转换", refresh:"刷新", hist_time:"时间", hist_file:"文件", hist_rows:"行数", hist_status:"预检",
    hist_none:"（暂无记录）", hist_dl:"下载", hist_bad:"有问题", hist_ok:"正常",
    hist_view:"查看", hist_view_bad:"查看问题", hist_report_hint:"以下是历史转换（%s）的问题明细：",
    advanced_title:"高级设置（管理员）", advanced_desc:"以下为字段映射配置，仅管理员维护。普通用户无需修改。所有设置都以表单填写，无需写代码。",
    lbl_outsheet:"输出工作表名称",
    load_config:"载入当前配置", save_config:"保存配置", add_field:"新增字段", del_row:"删除",
    export_config:"导出配置", import_config:"导入配置", imported_ok:"配置已导入，请点「保存配置」使其生效", imported_err:"导入失败：",
    block_backup:"配置备份", block_backup_hint:"每次保存都会自动留一份备份，改坏了可以还原",
    restore:"恢复此备份", refresh_backup:"刷新列表", no_backup:"（暂无备份）",
    restored_ok:"已恢复备份", restored_err:"恢复失败：",
    th_target:"目标列", th_source:"源列(MBxxx)", th_op:"填充方式", th_param:"参数设置", th_desc:"说明", th_tpl:"模板列：",
    block_fields:"字段映射", block_fields_hint:"每一行 = 一个目标列怎么填。带 * 的是模板必填列",
    block_unit:"单位翻译表", block_unit_hint:"左边填原符号，右边填中文单位名，例如 kg → 千克",
    unit_from:"原符号 / 原单位", unit_to:"翻译为", add_unit:"+ 添加单位",
    block_dicts:"字典表（原值 → 翻译）", block_dicts_hint:"把源文件里的代码翻译成人能看懂的中文，例如 110 → 成品",
    add_dict:"+ 新增字典", del_dict:"删除字典", add_entry:"+ 添加条目", dict_name:"字典名称", choose_dict:"（选择字典）",
    block_wl:"允许值清单", block_wl_hint:"某列的值不在清单里时，预检会告警（不改写数据）。例如「基本单位」只允许 MES 单位档案里的单位名",
    add_wl:"+ 添加一列的允许值", wl_col:"目标列", wl_values:"允许的值（逗号分隔）",
    adv_help:"「填充方式」决定这一列怎么得到值：直接复制=把源列原样搬过去；字典翻译=按字典把代码换成中文；固定值=每次都写同一个内容；条件填充=满足某条件才填；关键词判断=名称里含某些词就填「是」否则「否」；关键词映射=名称含某词就填对应中文；单位翻译=按单位表翻译；多条件判断=按多条规则依次判断。",
    saved_ok:"配置已保存", saved_err:"保存失败：", loaded_ok:"配置已载入", loaded_err:"载入失败：",
    confirm_save:F=>"发现 "+F+" 处配置问题：\n\n%S\n\n仍要保存吗？",
    rows_label:"数据行数", type_label:"物料类型分布", source_label:"物料来源分布", elapsed_label:"耗时",
    footer:"本工具离线运行，数据不会上传任何服务器。",
    err_no_src:"请先选择源文件", err_no_tpl:"请先选择模板文件",
    status_done:"转换成功", status_error:"转换出错",
    v_no_target:i=>"第 "+i+" 行：没有填目标列",
    v_no_dict:i=>"第 "+i+" 行：选了「字典翻译」但没选择字典",
    v_no_value:i=>"第 "+i+" 行：选了「固定值」但没填内容",
    v_no_kw:i=>"第 "+i+" 行：选了「关键词判断」但没填关键词",
    v_no_kwmap:i=>"第 "+i+" 行：选了「关键词映射」但没有任何对应关系",
    v_no_case:i=>"第 "+i+" 行：选了「多条件判断」但没有任何条件",
    v_no_cond:i=>"第 "+i+" 行：选了「条件填充」但没填比较值",
    v_dup_target:(i,s)=>"第 "+i+" 行：目标列「"+s+"」重复了",
    kwmap_title:"关键词对应表（名称含某词 → 写入对应值）", kwmap_else:"都不命中时填写：", kw_col_kw:"关键词", kw_col_val:"写入值", case_col_val:"值",
    case_title:"条件列表（从上到下，命中第一条就停）", case_default:"默认（都不命中）填写：", add_cond:"+ 添加条件", add_kw:"+ 添加关键词",
    p_trim:"去首尾空格", p_raw:"保留原样", p_default:"空时默认填：", p_value:"固定写入：",
    p_if:"当源值等于", p_then:"则填", p_else:"否则填", p_keywords:"关键词(逗号分隔)", p_source:"源列", p_match:"匹配",
    hdr_loaded:"表头已读取：%d 列", hdr_fail:"读取源文件表头失败（不影响转换）：",
    prog_title:"正在转换", prog_preparing:"正在读取文件…", prog_fmt:"已处理 %d / %d 行 · %d%%", prog_elapsed:"已用 %s", prog_done:"转换完成，共 %d 行",
    block_profiles:"配置档（多套配置）", block_profiles_hint:"把当前配置存成一份命名档案，不同工厂 / 模板一键载入",
    profiles_none:"（暂无配置档）", profiles_load:"载入", profiles_del:"删除", profiles_saveas:"另存为配置档",
    profiles_prompt:"给这份配置起个名字（可用中英文、数字、-、_）", profiles_saved:"配置档已保存：", profiles_err:"配置档操作失败：",
    profiles_loaded:"配置档已载入到表单，请点「保存配置」使其生效", profiles_del_confirm:"确定删除配置档「%s」吗？",
    block_values:"值域表（导出给业务维护）", block_values_hint:"把字典表 / 单位表导出成 CSV，交给懂业务的人改，再导回来，不用碰 JSON",
    values_export:"导出值域表 CSV", values_import:"导入值域表 CSV", values_confirm:"导入会按 CSV 内容整体替换字典表与单位表，继续吗？",
    values_loaded:"值域表已导入到表单，请点「保存配置」使其生效", values_err:"值域表导入失败：",
    help_btn:"? 使用说明", help_title:"使用说明", about_btn:"关于", about_title:"关于本工具", close_btn:"关闭",
    about_version:"版本", about_build:"构建时间", about_runtime:"运行环境", about_addr:"访问地址",
    about_exe:"程序位置", about_config:"配置文件", about_log:"日志文件", about_out:"输出目录", about_backup:"备份目录",
    help_body:`<h4>三步完成转换</h4>
<ol><li><b>选源文件</b>：从鼎新 ERP 导出的物料档（.xlsx，含 MB001 ~ MB254 列）。</li>
<li><b>选模板</b>：MES 的「物料档案导入模板」。</li>
<li><b>开始转换</b>：跑完可下载结果，文件同时保存在程序目录的 <code>out\\</code> 里（按日期时间命名，不会重名）。</li></ol>
<h4>预检结果怎么看</h4>
<p>转换后下方会列出发现的问题（编码重复、字典里没有的值、单位不在 MES 单位档案里等）。点「最近转换」里的<b>查看问题</b>可随时翻回任意一次的问题明细，重启程序也不会丢。</p>
<h4>托盘图标</h4>
<p>程序常驻右下角托盘：<b>右键</b>可「打开界面 / 打开输出文件夹 / 退出」，<b>双击</b>打开界面。关掉浏览器标签页不会退出程序，要退出请用托盘右键菜单。</p>
<h4>配置中心（管理员，右上角「⚙ 配置」）</h4>
<p>「字段映射」决定每一列怎么填。可以「另存为配置档」保存多套配置（不同工厂 / 模板），也可把字典表导出成 CSV 交给懂业务的人维护后再导回。全部是表单填写，不需要写代码。</p>
<h4>常见问题</h4>
<ul><li><b>点了一下没反应？</b>看右下角托盘图标——界面已在后台运行。</li><li><b>转换失败？</b>下方「转换日志」有详细原因，也可把 <code>out\\</code> 里的日志发给制作者。</li></ul>`,
  },
  zht:{
    home_title:"請選擇要使用的模組", home_desc:"兩個模組各自獨立，設定與輸出互不影響",
    mod_mat_name:"物料檔案", mod_mat_desc:"把鼎新 ERP 匯出的物料檔（INVMB）轉換成 MES 的「物料檔案匯入範本」，自動做欄位對應、單位歸一與預檢。",
    mod_mat_t1:"欄位對應", mod_mat_t2:"單位歸一", mod_mat_t3:"匯入前預檢", mod_mat_go:"進入 →",
    mod_ord_name:"訂單 / 工單", mod_ord_desc:"把鼎新 ERP 匯出的訂單檔與製令（工單）檔，轉換成 MES 的「銷售訂單主表 / 銷售訂單明細 / 工單」三張表，自動回填銷售訂單行號與打標判定。",
    mod_ord_t1:"主表 + 明細 + 工單", mod_ord_t2:"行號回填", mod_ord_t3:"打標判定", mod_ord_go:"進入 →",
    back_btn:"← 返回",
    cfg_title:"設定中心", cfg_subtitle:"欄位對應與轉換規則的維護頁面",
    cfg_desc:"兩個模組的欄位對應與規則都在這裡維護。選檔、轉換這些日常操作在各自的模組頁面完成，這裡只改設定。",
    cfg_entry_name:"設定中心",
    cfg_entry_desc:"欄位對應、單位表、字典表、表頭別名都在這裡維護；操作頁面只負責選檔與轉換",
    cfg_entry_go:"進入 →", cfg_tab_mat:"物料檔案", cfg_tab_ord:"訂單 / 工單", cfg_nav_btn:"⚙ 設定",
    cfg_load_fail:"設定讀取失敗，已禁止儲存設定，以免覆蓋伺服器端現有設定。請檢查服務後重新整理頁面。", cfg_leave_confirm:"設定有尚未儲存的修改，確定離開嗎？",
    err_timeout:"轉換逾時（超過 10 分鐘）。檔案可能過大，可拆分後再試，或查看 out\\ 目錄下是否已產生部分結果。",
    err_aborted:"請求已中斷。", err_offline:"連不上本機服務，請確認程式仍在執行（看右下角系統匣圖示），然後重新整理頁面再試。",
    ord_app_title:"MES 訂單 / 工單轉換工具", ord_app_subtitle:"鼎新 ERP 訂單·製令檔 → MES 匯入表，一鍵轉換",
    ord_step_source:"訂單檔案（可選）", ord_step_source_desc:"鼎新 ERP 匯出的訂單檔（.xlsx），用於產生銷售訂單主表與明細",
    ord_step_work:"工單檔案（可選）", ord_step_work_desc:"鼎新 ERP 匯出的製令（工單）檔（.xlsx），用於產生工單表",
    ord_choose_file:"選擇檔案", ord_drop_hint:"也可以直接把檔案拖到這裡",
    ord_start_btn:"開始轉換", ord_converting:"正在轉換，請稍候…",
    ord_no_file:"請先選擇訂單檔案或工單檔案（至少一個）",
    ord_result_title:"轉換完成！", ord_result_desc:"檔案已產生，點擊下載即可匯入 MES。",
    ord_download:"下載結果檔案", ord_open_folder:"開啟輸出資料夾",
    ord_check_title:"預檢結果", ord_history_title:"最近轉換", ord_log_title:"轉換日誌",
    ord_copy_log:"複製日誌", ord_view_log:"檢視日誌",
    ord_hist_none:"（暫無記錄）", ord_hist_bad:"有問題", ord_hist_ok:"正常", ord_hist_dl:"下載", ord_hist_view:"檢視問題",
    ord_all_ok:"未發現問題 ✓", ord_check_summary:"%d 處需關注",
    ord_advanced_title:"進階設定（管理員）",
    ord_advanced_desc:"訂單模組的欄位對應與判定規則。所有設定都以表單填寫，無需寫程式。",
    ord_cfg_reload:"載入目前設定", ord_cfg_save:"儲存設定",
    ord_cfg_saved:"設定已儲存", ord_cfg_fail:"儲存失敗：",
    ord_block_map:"欄位對應",
    ord_block_map_hint:"每列 = 一個輸出欄：來源欄（可留空表示不匯出）+ 來源檔為空時填入的預設值。表格列序就是輸出欄順序。",
    ord_th_target:"輸出欄", ord_th_source:"來源欄", ord_th_default:"預設值", ord_th_op:"操作", ord_add_row:"+ 新增一欄",
    ord_block_tag:"打標判定", ord_block_tag_hint:"產品編碼以任一前綴開頭 → 「是」，否則「否」",
    ord_tag_column:"輸出欄", ord_tag_source:"判斷依據欄", ord_tag_prefixes:"前綴（逗號分隔）", ord_tag_yes:"命中填", ord_tag_no:"未命中填",
    ord_block_sw:"執行選項",
    ord_sw_header:"輸出表頭帶必填星號（* 欄名）", ord_sw_autoline:"工單的銷售訂單行號從訂單明細自動匹配",
    ord_sw_filtersum:"過濾小計 / 合計列", ord_sw_validate:"轉換前做資料檢核",
    ord_block_alias:"表頭別名表", ord_block_alias_hint:"來源檔表頭五花八門時靠它辨識。匹配時已做簡繁歸一，寫簡寫繁都認。",
    ord_cat_order_master:"銷售訂單主表", ord_cat_order_detail:"銷售訂單明細", ord_cat_work_order:"工單",
    ord_progress:"已處理 %d / %d 行", ord_tag_disabled:"（未啟用打標）",
    app_title:"MES 物料檔案轉換工具", app_subtitle:"鼎新 ERP 物料檔 → MES 匯入範本，一鍵轉換",
    lang_label:"語言",
    step_source:"第 1 步 · 選擇來源檔案", step_source_desc:"選擇從鼎新 ERP 匯出的物料檔（.xlsx）",
    step_template:"第 2 步 · 選擇範本", step_template_desc:"選擇 MES 的「物料檔案匯入範本」（.xlsx）",
    drop_hint:"也可以直接把檔案拖到這裡",
    choose_file:"選擇檔案", no_file:"尚未選擇檔案", file_chosen:"已選擇：", clear_file:"清除選擇",
    start_btn:"開始轉換", converting:"正在轉換，請稍候…",
    status_ready:"請選擇來源檔案和範本後開始", status_pick:"請先選擇兩個檔案",
    result_title:"轉換完成！", result_desc:"檔案已生成，點擊下載即可匯入 MES。",
    download_btn:"下載結果檔案", open_folder:"開啟輸出資料夾", opening:"正在開啟…",
    log_title:"轉換日誌", view_log:"檢視日誌", hide_log:"收起日誌", copy_log:"複製日誌",
    check_title:"預檢結果", check_ok:"未發現問題 ✓", check_summary:"%d 處需關注",
    issue_row:"行號", issue_col:"欄", issue_msg:"說明",
    check_lang_hint:"提示內容依轉換時的語言產生；如需其他語言，請重新轉換。",
    history_title:"最近轉換", refresh:"重新整理", hist_time:"時間", hist_file:"檔案", hist_rows:"筆數", hist_status:"預檢",
    hist_none:"（暫無記錄）", hist_dl:"下載", hist_bad:"有問題", hist_ok:"正常",
    hist_view:"檢視", hist_view_bad:"檢視問題", hist_report_hint:"以下是歷史轉換（%s）的問題明細：",
    advanced_title:"進階設定（管理員）", advanced_desc:"以下為欄位對應設定，僅管理員維護。一般使用者無需修改。所有設定都以表單填寫，無需寫程式。",
    lbl_outsheet:"輸出工作表名稱",
    load_config:"載入目前設定", save_config:"儲存設定", add_field:"新增欄位", del_row:"刪除",
    export_config:"匯出設定", import_config:"匯入設定", imported_ok:"設定已匯入，請點「儲存設定」使其生效", imported_err:"匯入失敗：",
    block_backup:"設定備份", block_backup_hint:"每次儲存都會自動留一份備份，改壞了可以還原",
    restore:"還原此備份", refresh_backup:"重新整理", no_backup:"（暫無備份）",
    restored_ok:"已還原備份", restored_err:"還原失敗：",
    th_target:"目標欄", th_source:"來源欄(MBxxx)", th_op:"填充方式", th_param:"參數設定", th_desc:"說明", th_tpl:"範本欄：",
    block_fields:"欄位對應", block_fields_hint:"每一列 = 一個目標欄怎麼填。帶 * 的是範本必填欄",
    block_unit:"單位翻譯表", block_unit_hint:"左邊填原符號，右邊填中文單位名，例如 kg → 千克",
    unit_from:"原符號 / 原單位", unit_to:"翻譯為", add_unit:"+ 添加單位",
    block_dicts:"字典表（原值 → 翻譯）", block_dicts_hint:"把來源檔裡的代碼翻譯成看得懂的中文，例如 110 → 成品",
    add_dict:"+ 新增字典", del_dict:"刪除字典", add_entry:"+ 添加條目", dict_name:"字典名稱", choose_dict:"（選擇字典）",
    block_wl:"允許值清單", block_wl_hint:"某欄的值不在清單裡時，預檢會告警（不改寫資料）。例如「基本單位」只允許 MES 單位檔案裡的單位名",
    add_wl:"+ 添加一欄的允許值", wl_col:"目標欄", wl_values:"允許的值（逗號分隔）",
    adv_help:"「填充方式」決定這一欄怎麼得到值：直接複製=把來源欄原樣搬過去；字典翻譯=按字典把代碼換成中文；固定值=每次都寫同一個內容；條件填充=滿足某條件才填；關鍵詞判斷=名稱裡含某些詞就填「是」否則「否」；關鍵詞映射=名稱含某詞就填對應中文；單位翻譯=按單位表翻譯；多條件判斷=按多條規則依序判斷。",
    saved_ok:"設定已儲存", saved_err:"儲存失敗：", loaded_ok:"設定已載入", loaded_err:"載入失敗：",
    confirm_save:F=>"發現 "+F+" 處設定問題：\n\n%S\n\n仍要儲存嗎？",
    rows_label:"資料筆數", type_label:"物料類型分布", source_label:"物料來源分布", elapsed_label:"耗時",
    footer:"本工具離線運行，資料不會上傳任何伺服器。",
    err_no_src:"請先選擇來源檔案", err_no_tpl:"請先選擇範本檔案",
    status_done:"轉換成功", status_error:"轉換出錯",
    v_no_target:i=>"第 "+i+" 列：沒有填目標欄",
    v_no_dict:i=>"第 "+i+" 列：選了「字典翻譯」但沒選擇字典",
    v_no_value:i=>"第 "+i+" 列：選了「固定值」但沒填內容",
    v_no_kw:i=>"第 "+i+" 列：選了「關鍵詞判斷」但沒填關鍵詞",
    v_no_kwmap:i=>"第 "+i+" 列：選了「關鍵詞映射」但沒有任何對應關係",
    v_no_case:i=>"第 "+i+" 列：選了「多條件判斷」但沒有任何條件",
    v_no_cond:i=>"第 "+i+" 列：選了「條件填充」但沒填比較值",
    v_dup_target:(i,s)=>"第 "+i+" 列：目標欄「"+s+"」重複了",
    kwmap_title:"關鍵詞對應表（名稱含某詞 → 寫入對應值）", kwmap_else:"都不命中時填寫：", kw_col_kw:"關鍵詞", kw_col_val:"寫入值", case_col_val:"值",
    case_title:"條件列表（由上到下，命中第一條就停）", case_default:"預設（都不命中）填寫：", add_cond:"+ 添加條件", add_kw:"+ 添加關鍵詞",
    p_trim:"去首尾空格", p_raw:"保留原樣", p_default:"空時預設填：", p_value:"固定寫入：",
    p_if:"當來源值等於", p_then:"則填", p_else:"否則填", p_keywords:"關鍵詞(逗號分隔)", p_source:"來源欄", p_match:"匹配",
    hdr_loaded:"表頭已讀取：%d 欄", hdr_fail:"讀取來源檔表頭失敗（不影響轉換）：",
    prog_title:"正在轉換", prog_preparing:"正在讀取檔案…", prog_fmt:"已處理 %d / %d 列 · %d%%", prog_elapsed:"已用 %s", prog_done:"轉換完成，共 %d 列",
    block_profiles:"設定檔（多套設定）", block_profiles_hint:"把目前設定存成一份命名檔案，不同工廠 / 範本一鍵載入",
    profiles_none:"（暫無設定檔）", profiles_load:"載入", profiles_del:"刪除", profiles_saveas:"另存為設定檔",
    profiles_prompt:"給這份設定取個名字（可用中英文、數字、-、_）", profiles_saved:"設定檔已儲存：", profiles_err:"設定檔操作失敗：",
    profiles_loaded:"設定檔已載入到表單，請點「儲存設定」使其生效", profiles_del_confirm:"確定刪除設定檔「%s」嗎？",
    block_values:"值域表（匯出給業務維護）", block_values_hint:"把字典表 / 單位表匯出成 CSV，交給懂業務的人修改，再匯回來，不必碰 JSON",
    values_export:"匯出值域表 CSV", values_import:"匯入值域表 CSV", values_confirm:"匯入會依 CSV 內容整體取代字典表與單位表，要繼續嗎？",
    values_loaded:"值域表已匯入到表單，請點「儲存設定」使其生效", values_err:"值域表匯入失敗：",
    help_btn:"? 使用說明", help_title:"使用說明", about_btn:"關於", about_title:"關於本工具", close_btn:"關閉",
    about_version:"版本", about_build:"建置時間", about_runtime:"執行環境", about_addr:"存取網址",
    about_exe:"程式位置", about_config:"設定檔", about_log:"記錄檔", about_out:"輸出資料夾", about_backup:"備份資料夾",
    help_body:`<h4>三步完成轉換</h4>
<ol><li><b>選來源檔</b>：從鼎新 ERP 匯出的物料檔（.xlsx，含 MB001 ~ MB254 欄）。</li>
<li><b>選範本</b>：MES 的「物料檔案匯入範本」。</li>
<li><b>開始轉換</b>：完成後可下載結果，檔案同時存在程式目錄的 <code>out\\</code> 裡（依日期時間命名，不會同名）。</li></ol>
<h4>預檢結果怎麼看</h4>
<p>轉換後下方會列出發現的問題（編碼重複、字典裡沒有的值、單位不在 MES 單位檔案裡等）。點「最近轉換」裡的<b>查看問題</b>可隨時翻回任一次的問題明細，重啟程式也不會遺失。</p>
<h4>系統匣圖示</h4>
<p>程式常駐右下角系統匣：<b>按右鍵</b>可「開啟介面 / 開啟輸出資料夾 / 結束」，<b>按兩下</b>開啟介面。關閉瀏覽器分頁不會結束程式，要結束請用系統匣右鍵選單。</p>
<h4>設定中心（管理員，右上角「⚙ 設定」）</h4>
<p>「欄位對應」決定每一欄如何填。可「另存為設定檔」保存多套設定（不同工廠 / 範本），也可把字典表匯出成 CSV 交給懂業務的人維護後再匯回。全部以表單填寫，不需要寫程式。</p>
<h4>常見問題</h4>
<ul><li><b>點了一下沒反應？</b>看右下角系統匣圖示——介面已在背景執行。</li><li><b>轉換失敗？</b>下方「轉換記錄」有詳細原因，也可把 <code>out\\</code> 裡的記錄檔傳給製作者。</li></ul>`,
  },
  vi:{
    home_title:"Chọn mô-đun bạn muốn dùng", home_desc:"Hai mô-đun độc lập, cấu hình và kết quả không ảnh hưởng lẫn nhau",
    mod_mat_name:"Hồ sơ vật tư", mod_mat_desc:"Chuyển tệp vật tư (INVMB) xuất từ ERP Đinh Tân thành \"Mẫu nhập hồ sơ vật tư\" của MES, tự động ánh xạ cột, chuẩn hóa đơn vị và kiểm tra trước.",
    mod_mat_t1:"Ánh xạ cột", mod_mat_t2:"Chuẩn hóa đơn vị", mod_mat_t3:"Kiểm tra trước khi nhập", mod_mat_go:"Vào →",
    mod_ord_name:"Đơn hàng / Lệnh sản xuất", mod_ord_desc:"Chuyển tệp đơn hàng và tệp lệnh sản xuất (chế lệnh) xuất từ ERP Đinh Tân thành 3 bảng của MES: \"Bảng đơn hàng chính / Chi tiết đơn hàng / Lệnh sản xuất\", tự động điền số dòng đơn hàng và đánh dấu.",
    mod_ord_t1:"Bảng chính + chi tiết + lệnh SX", mod_ord_t2:"Điền số dòng", mod_ord_t3:"Đánh dấu", mod_ord_go:"Vào →",
    back_btn:"← Quay lại",
    cfg_title:"Trung tâm cấu hình", cfg_subtitle:"Nơi quản lý ánh xạ trường và quy tắc chuyển đổi",
    cfg_desc:"Ánh xạ trường và quy tắc của cả hai mô-đun được quản lý ở đây. Việc chọn tệp và chuyển đổi thực hiện ở trang của từng mô-đun.",
    cfg_entry_name:"Trung tâm cấu hình",
    cfg_entry_desc:"Ánh xạ trường, bảng đơn vị, bảng từ điển, bí danh tiêu đề đều ở đây; trang thao tác chỉ chọn tệp và chuyển đổi",
    cfg_entry_go:"Vào →", cfg_tab_mat:"Hồ sơ vật tư", cfg_tab_ord:"Đơn hàng / Lệnh SX", cfg_nav_btn:"⚙ Cấu hình",
    cfg_load_fail:"Không đọc được cấu hình; đã chặn lưu để tránh ghi đè cấu hình hiện có trên máy chủ. Hãy kiểm tra dịch vụ rồi tải lại trang.", cfg_leave_confirm:"Cấu hình có thay đổi chưa lưu, bạn có chắc muốn rời đi?",
    err_timeout:"Chuyển đổi quá thời gian (hơn 10 phút). Tệp có thể quá lớn — hãy tách nhỏ rồi thử lại, hoặc xem thư mục out\\ xem đã có kết quả một phần chưa.",
    err_aborted:"Yêu cầu đã bị hủy.", err_offline:"Không kết nối được dịch vụ cục bộ. Hãy kiểm tra chương trình còn chạy không (xem biểu tượng khay dưới phải), rồi tải lại trang.",
    ord_app_title:"Công cụ chuyển đổi đơn hàng / lệnh sản xuất",
    ord_app_subtitle:"Tệp đơn hàng·chế lệnh ERP Đinh Tân → bảng nhập MES, chuyển đổi một chạm",
    ord_step_source:"Tệp đơn hàng (không bắt buộc)", ord_step_source_desc:"Tệp đơn hàng (.xlsx) xuất từ ERP Đinh Tân, dùng để tạo bảng đơn hàng chính và chi tiết",
    ord_step_work:"Tệp lệnh sản xuất (không bắt buộc)", ord_step_work_desc:"Tệp chế lệnh (.xlsx) xuất từ ERP Đinh Tân, dùng để tạo bảng lệnh sản xuất",
    ord_choose_file:"Chọn tệp", ord_drop_hint:"Cũng có thể kéo tệp vào đây",
    ord_start_btn:"Bắt đầu chuyển đổi", ord_converting:"Đang chuyển đổi, vui lòng chờ…",
    ord_no_file:"Hãy chọn tệp đơn hàng hoặc tệp lệnh sản xuất (ít nhất một tệp)",
    ord_result_title:"Chuyển đổi hoàn tất!", ord_result_desc:"Tệp đã được tạo, bấm tải xuống để nhập vào MES.",
    ord_download:"Tải tệp kết quả", ord_open_folder:"Mở thư mục kết quả",
    ord_check_title:"Kết quả kiểm tra", ord_history_title:"Chuyển đổi gần đây", ord_log_title:"Nhật ký chuyển đổi",
    ord_copy_log:"Sao chép nhật ký", ord_view_log:"Xem nhật ký",
    ord_hist_none:"(chưa có bản ghi)", ord_hist_bad:"Có vấn đề", ord_hist_ok:"Bình thường", ord_hist_dl:"Tải xuống", ord_hist_view:"Xem vấn đề",
    ord_all_ok:"Không phát hiện vấn đề ✓", ord_check_summary:"%d mục cần lưu ý",
    ord_advanced_title:"Cài đặt nâng cao (quản trị viên)",
    ord_advanced_desc:"Ánh xạ cột và quy tắc đánh dấu của mô-đun đơn hàng. Mọi cài đặt đều nhập bằng biểu mẫu, không cần viết mã.",
    ord_cfg_reload:"Nạp cấu hình hiện tại", ord_cfg_save:"Lưu cấu hình",
    ord_cfg_saved:"Đã lưu cấu hình", ord_cfg_fail:"Lưu thất bại: ",
    ord_block_map:"Ánh xạ cột",
    ord_block_map_hint:"Mỗi dòng = một cột kết quả: cột nguồn (để trống nghĩa là không xuất) + giá trị mặc định khi ô nguồn trống. Thứ tự dòng chính là thứ tự cột kết quả.",
    ord_th_target:"Cột kết quả", ord_th_source:"Cột nguồn", ord_th_default:"Mặc định", ord_th_op:"Thao tác", ord_add_row:"+ Thêm một cột",
    ord_block_tag:"Quy tắc đánh dấu", ord_block_tag_hint:"Mã sản phẩm bắt đầu bằng một trong các tiền tố → \"Có\", ngược lại \"Không\"",
    ord_tag_column:"Cột kết quả", ord_tag_source:"Cột căn cứ", ord_tag_prefixes:"Tiền tố (phân cách bằng dấu phẩy)", ord_tag_yes:"Nếu khớp", ord_tag_no:"Nếu không khớp",
    ord_block_sw:"Tùy chọn chạy",
    ord_sw_header:"Tiêu đề kết quả có dấu sao bắt buộc (* tên cột)", ord_sw_autoline:"Số dòng đơn hàng của lệnh SX tự khớp từ chi tiết đơn hàng",
    ord_sw_filtersum:"Lọc dòng cộng dồn / tổng cộng", ord_sw_validate:"Kiểm tra dữ liệu trước khi chuyển đổi",
    ord_block_alias:"Bảng tên gọi khác của tiêu đề",
    ord_block_alias_hint:"Dùng để nhận diện khi tiêu đề tệp nguồn đa dạng. Khi khớp đã chuẩn hóa phồn/giản, viết phồn hay giản đều nhận.",
    ord_cat_order_master:"Bảng đơn hàng chính", ord_cat_order_detail:"Chi tiết đơn hàng", ord_cat_work_order:"Lệnh sản xuất",
    ord_progress:"Đã xử lý %d / %d dòng", ord_tag_disabled:"(chưa bật đánh dấu)",
    app_title:"Công cụ chuyển đổi vật liệu MES", app_subtitle:"Tài liệu vật liệu Đinh Tân ERP → Mẫu nhập MES, chuyển đổi một chạm",
    lang_label:"Ngôn ngữ",
    step_source:"Bước 1 · Chọn tệp nguồn", step_source_desc:"Chọn tài liệu vật liệu xuất từ Đinh Tân ERP (.xlsx)",
    step_template:"Bước 2 · Chọn mẫu", step_template_desc:"Chọn «Mẫu nhập hồ sơ vật liệu» của MES (.xlsx)",
    drop_hint:"Cũng có thể kéo tệp vào đây",
    choose_file:"Chọn tệp", no_file:"Chưa chọn tệp", file_chosen:"Đã chọn: ", clear_file:"Bỏ chọn",
    start_btn:"Bắt đầu chuyển đổi", converting:"Đang chuyển đổi, vui lòng chờ…",
    status_ready:"Vui lòng chọn tệp nguồn và mẫu để bắt đầu", status_pick:"Vui lòng chọn cả hai tệp trước",
    result_title:"Chuyển đổi hoàn tất!", result_desc:"Tệp đã được tạo, nhấp tải xuống để nhập vào MES.",
    download_btn:"Tải xuống tệp kết quả", open_folder:"Mở thư mục đầu ra", opening:"Đang mở…",
    log_title:"Nhật ký chuyển đổi", view_log:"Xem nhật ký", hide_log:"Ẩn nhật ký", copy_log:"Sao chép nhật ký",
    check_title:"Kết quả kiểm tra trước", check_ok:"Không phát hiện vấn đề ✓", check_summary:"%d mục cần lưu ý",
    issue_row:"Dòng", issue_col:"Cột", issue_msg:"Mô tả",
    check_lang_hint:"Nội dung cảnh báo theo ngôn ngữ lúc chuyển đổi; muốn đổi ngôn ngữ hãy chuyển đổi lại.",
    history_title:"Chuyển đổi gần đây", refresh:"Làm mới", hist_time:"Thời gian", hist_file:"Tệp", hist_rows:"Số dòng", hist_status:"Kiểm tra",
    hist_none:"(chưa có bản ghi)", hist_dl:"Tải xuống", hist_bad:"Có vấn đề", hist_ok:"Bình thường",
    hist_view:"Xem", hist_view_bad:"Xem vấn đề", hist_report_hint:"Chi tiết vấn đề của lần chuyển đổi trước (%s):",
    advanced_title:"Cài đặt nâng cao (quản trị)", advanced_desc:"Cấu hình ánh xạ trường bên dưới, chỉ quản trị viên bảo trì. Người dùng thường không cần sửa. Mọi cài đặt đều là biểu mẫu, không cần viết mã.",
    lbl_outsheet:"Tên bảng tính đầu ra",
    load_config:"Tải cấu hình hiện tại", save_config:"Lưu cấu hình", add_field:"Thêm trường", del_row:"Xóa",
    export_config:"Xuất cấu hình", import_config:"Nhập cấu hình", imported_ok:"Đã nhập cấu hình, hãy nhấn «Lưu cấu hình» để áp dụng", imported_err:"Nhập thất bại: ",
    block_backup:"Sao lưu cấu hình", block_backup_hint:"Mỗi lần lưu đều tự động sao lưu, hỏng có thể khôi phục",
    restore:"Khôi phục bản này", refresh_backup:"Làm mới danh sách", no_backup:"(chưa có sao lưu)",
    restored_ok:"Đã khôi phục sao lưu", restored_err:"Khôi phục thất bại: ",
    th_target:"Cột đích", th_source:"Cột nguồn(MBxxx)", th_op:"Cách điền", th_param:"Thiết lập tham số", th_desc:"Mô tả", th_tpl:"Cột mẫu:",
    block_fields:"Ánh xạ trường", block_fields_hint:"Mỗi dòng = một cột đích được điền thế nào. Dấu * là cột bắt buộc của mẫu",
    block_unit:"Bảng dịch đơn vị", block_unit_hint:"Trái điền ký hiệu gốc, phải điền tên đơn vị tiếng Trung, ví dụ kg → 千克",
    unit_from:"Ký hiệu / đơn vị gốc", unit_to:"Dịch thành", add_unit:"+ Thêm đơn vị",
    block_dicts:"Bảng từ điển (giá trị gốc → dịch)", block_dicts_hint:"Dịch mã trong tệp nguồn sang tiếng Trung dễ hiểu, ví dụ 110 → 成品",
    add_dict:"+ Thêm từ điển", del_dict:"Xóa từ điển", add_entry:"+ Thêm mục", dict_name:"Tên từ điển", choose_dict:"(chọn từ điển)",
    block_wl:"Danh sách giá trị cho phép", block_wl_hint:"Giá trị ngoài danh sách sẽ bị cảnh báo khi kiểm tra trước (không sửa dữ liệu). Ví dụ «Đơn vị cơ bản» chỉ cho phép đơn vị có trong hồ sơ đơn vị MES",
    add_wl:"+ Thêm danh sách cho một cột", wl_col:"Cột đích", wl_values:"Giá trị cho phép (phẩy)",
    adv_help:"«Cách điền» quyết định cột này lấy giá trị ra sao: Sao chép trực tiếp=chuyển nguyên cột nguồn; Dịch từ điển=đổi mã theo từ điển; Giá trị cố định=luôn ghi cùng một nội dung; Điền theo điều kiện=chỉ điền khi thỏa mãn; Từ khóa Đúng/Sai=tên chứa từ nhất định thì «是» còn không thì «否»; Ánh xạ từ khóa=tên chứa từ nhất định thì điền giá trị tương ứng; Dịch đơn vị=theo bảng đơn vị; Nhiều điều kiện=xét lần lượt các quy tắc.",
    saved_ok:"Đã lưu cấu hình", saved_err:"Lưu thất bại: ", loaded_ok:"Đã tải cấu hình", loaded_err:"Tải thất bại: ",
    confirm_save:F=>"Phát hiện "+F+" vấn đề cấu hình:\n\n%S\n\nVẫn lưu?",
    rows_label:"Số dòng dữ liệu", type_label:"Phân bố loại vật liệu", source_label:"Phân bố nguồn vật liệu", elapsed_label:"Thời gian",
    footer:"Công cụ chạy ngoại tuyến, dữ liệu không được tải lên bất kỳ máy chủ nào.",
    err_no_src:"Vui lòng chọn tệp nguồn trước", err_no_tpl:"Vui lòng chọn tệp mẫu trước",
    status_done:"Chuyển đổi thành công", status_error:"Lỗi chuyển đổi",
    v_no_target:i=>"Dòng "+i+": chưa điền cột đích",
    v_no_dict:i=>"Dòng "+i+": đã chọn «Dịch từ điển» nhưng chưa chọn từ điển",
    v_no_value:i=>"Dòng "+i+": đã chọn «Giá trị cố định» nhưng chưa điền nội dung",
    v_no_kw:i=>"Dòng "+i+": đã chọn «Từ khóa Đúng/Sai» nhưng chưa điền từ khóa",
    v_no_kwmap:i=>"Dòng "+i+": đã chọn «Ánh xạ từ khóa» nhưng chưa có ánh xạ nào",
    v_no_case:i=>"Dòng "+i+": đã chọn «Nhiều điều kiện» nhưng chưa có điều kiện nào",
    v_no_cond:i=>"Dòng "+i+": đã chọn «Điền theo điều kiện» nhưng chưa điền giá trị so sánh",
    v_dup_target:(i,s)=>"Dòng "+i+": cột đích «"+s+"» bị trùng",
    kwmap_title:"Bảng ánh xạ từ khóa (tên chứa từ → điền giá trị)", kwmap_else:"Không khớp thì điền:", kw_col_kw:"Từ khóa", kw_col_val:"Giá trị ghi", case_col_val:"Giá trị",
    case_title:"Danh sách điều kiện (từ trên xuống, khớp cái đầu thì dừng)", case_default:"Mặc định (không khớp) điền:", add_cond:"+ Thêm điều kiện", add_kw:"+ Thêm từ khóa",
    p_trim:"Bỏ khoảng trắng", p_raw:"Giữ nguyên", p_default:"Trống thì điền:", p_value:"Ghi cố định:",
    p_if:"Khi giá trị nguồn bằng", p_then:"thì điền", p_else:"ngược lại điền", p_keywords:"Từ khóa(phẩy)", p_source:"Cột nguồn", p_match:"Khớp",
    hdr_loaded:"Đã đọc tiêu đề: %d cột", hdr_fail:"Đọc tiêu đề tệp nguồn thất bại (không ảnh hưởng chuyển đổi): ",
    prog_title:"Đang chuyển đổi", prog_preparing:"Đang đọc tệp…", prog_fmt:"Đã xử lý %d / %d dòng · %d%%", prog_elapsed:"Đã dùng %s", prog_done:"Hoàn tất, tổng %d dòng",
    block_profiles:"Hồ sơ cấu hình (nhiều bộ)", block_profiles_hint:"Lưu cấu hình hiện tại thành hồ sơ có tên, chuyển nhanh giữa các nhà máy / biểu mẫu",
    profiles_none:"(chưa có hồ sơ)", profiles_load:"Tải", profiles_del:"Xóa", profiles_saveas:"Lưu thành hồ sơ",
    profiles_prompt:"Đặt tên cho hồ sơ (chữ, số, - , _)", profiles_saved:"Đã lưu hồ sơ: ", profiles_err:"Thao tác hồ sơ thất bại: ",
    profiles_loaded:"Đã tải hồ sơ vào biểu mẫu, hãy bấm「Lưu cấu hình」để áp dụng", profiles_del_confirm:"Xóa hồ sơ「%s」?",
    block_values:"Bảng giá trị (xuất cho nghiệp vụ)", block_values_hint:"Xuất bảng từ điển / đơn vị ra CSV để người hiểu nghiệp vụ sửa, rồi nhập lại",
    values_export:"Xuất CSV giá trị", values_import:"Nhập CSV giá trị", values_confirm:"Nhập sẽ thay thế toàn bộ bảng từ điển và bảng đơn vị theo CSV. Tiếp tục?",
    values_loaded:"Đã nhập bảng giá trị vào biểu mẫu, hãy bấm「Lưu cấu hình」để áp dụng", values_err:"Nhập bảng giá trị thất bại: ",
    help_btn:"? Hướng dẫn", help_title:"Hướng dẫn sử dụng", about_btn:"Giới thiệu", about_title:"Về công cụ này", close_btn:"Đóng",
    about_version:"Phiên bản", about_build:"Thời điểm build", about_runtime:"Môi trường", about_addr:"Địa chỉ truy cập",
    about_exe:"Vị trí chương trình", about_config:"Tệp cấu hình", about_log:"Tệp log", about_out:"Thư mục kết quả", about_backup:"Thư mục sao lưu",
    help_body:`<h4>Ba bước để chuyển đổi</h4>
<ol><li><b>Chọn tệp nguồn</b>: tệp vật tư xuất từ ERP Đỉnh Tân (.xlsx, gồm MB001 ~ MB254).</li>
<li><b>Chọn biểu mẫu</b>: mẫu nhập hồ sơ vật tư của MES.</li>
<li><b>Bắt đầu chuyển đổi</b>: xong có thể tải kết quả; tệp cũng được lưu trong thư mục <code>out\\</code> cạnh chương trình (đặt tên theo ngày giờ, không trùng).</li></ol>
<h4>Xem kết quả kiểm tra</h4>
<p>Sau khi chuyển đổi, các vấn đề sẽ được liệt kê bên dưới (mã trùng, giá trị chưa có trong từ điển, đơn vị không có trong danh mục đơn vị MES...). Bấm <b>Xem vấn đề</b> trong「Chuyển đổi gần đây」để xem lại chi tiết bất cứ lúc nào, kể cả sau khi khởi động lại.</p>
<h4>Biểu tượng khay hệ thống</h4>
<p>Chương trình nằm ở khay góc phải dưới: <b>nhấp chuột phải</b> để「Mở giao diện / Mở thư mục kết quả / Thoát」, <b>nhấp đúp</b> để mở giao diện. Đóng tab trình duyệt không thoát chương trình; muốn thoát hãy dùng menu chuột phải ở khay.</p>
<h4>Cài đặt nâng cao (quản trị)</h4>
<p>「Ánh xạ trường」quyết định cách điền từng cột. Có thể「Lưu thành hồ sơ」nhiều bộ cấu hình (nhà máy / biểu mẫu khác nhau), hoặc xuất bảng từ điển ra CSV cho nghiệp vụ sửa rồi nhập lại. Tất cả điền bằng biểu mẫu, không cần viết mã.</p>
<h4>Câu hỏi thường gặp</h4>
<ul><li><b>Bấm mà không thấy phản hồi?</b> Xem biểu tượng ở khay góc phải dưới — giao diện đang chạy nền.</li><li><b>Chuyển đổi thất bại?</b> 「Nhật ký chuyển đổi」bên dưới có nguyên nhân chi tiết; cũng có thể gửi tệp log trong <code>out\\</code> cho người tạo.</li></ul>`,
  },
  en:{
    home_title:"Choose a module to start", home_desc:"The two modules are independent — their settings and outputs never interfere",
    mod_mat_name:"Material Archive", mod_mat_desc:"Convert the Dingxin ERP material file (INVMB) into the MES material-archive import template, with automatic field mapping, unit normalisation and pre-checks.",
    mod_mat_t1:"Field mapping", mod_mat_t2:"Unit normalisation", mod_mat_t3:"Pre-import checks", mod_mat_go:"Open →",
    mod_ord_name:"Orders / Work Orders", mod_ord_desc:"Convert Dingxin ERP order files and manufacturing orders into three MES tables — sales order master, sales order detail and work orders — back-filling order line numbers and mark flags automatically.",
    mod_ord_t1:"Master + detail + work orders", mod_ord_t2:"Line back-fill", mod_ord_t3:"Mark decision", mod_ord_go:"Open →",
    back_btn:"← Back",
    cfg_title:"Configuration", cfg_subtitle:"Where field mappings and conversion rules are maintained",
    cfg_desc:"Field mappings and rules for both modules live here. Picking files and converting stay on each module's own page.",
    cfg_entry_name:"Configuration",
    cfg_entry_desc:"Field mappings, units, dictionaries and header aliases all live here; module pages only pick files and convert",
    cfg_entry_go:"Open →", cfg_tab_mat:"Material archive", cfg_tab_ord:"Order / Work order", cfg_nav_btn:"⚙ Configure",
    cfg_load_fail:"Failed to load configuration. Saving is disabled to avoid overwriting the existing server config. Check the service and reload.", cfg_leave_confirm:"You have unsaved configuration changes. Leave anyway?",
    err_timeout:"Conversion timed out (over 10 minutes). The file may be too large — try splitting it, or check the out\\ folder for a partial result.",
    err_aborted:"Request was interrupted.", err_offline:"Cannot reach the local service. Make sure the program is still running (check the tray icon), then reload the page.",
    ord_app_title:"MES Order / Work-Order Converter",
    ord_app_subtitle:"Dingxin ERP order & manufacturing files → MES import tables, one click",
    ord_step_source:"Order file (optional)", ord_step_source_desc:"Order file (.xlsx) exported from Dingxin ERP — builds the sales order master and detail",
    ord_step_work:"Work-order file (optional)", ord_step_work_desc:"Manufacturing order file (.xlsx) exported from Dingxin ERP — builds the work-order table",
    ord_choose_file:"Choose file", ord_drop_hint:"You can also drag the file here",
    ord_start_btn:"Start conversion", ord_converting:"Converting, please wait…",
    ord_no_file:"Please choose an order file and/or a work-order file (at least one)",
    ord_result_title:"Conversion complete!", ord_result_desc:"The file is ready — download it and import into MES.",
    ord_download:"Download result", ord_open_folder:"Open output folder",
    ord_check_title:"Pre-check results", ord_history_title:"Recent conversions", ord_log_title:"Conversion log",
    ord_copy_log:"Copy log", ord_view_log:"View log",
    ord_hist_none:"(no records yet)", ord_hist_bad:"Has issues", ord_hist_ok:"OK", ord_hist_dl:"Download", ord_hist_view:"View issues",
    ord_all_ok:"No issues found ✓", ord_check_summary:"%d item(s) to review",
    ord_advanced_title:"Advanced settings (administrator)",
    ord_advanced_desc:"Field mapping and decision rules for the order module. Everything is edited as forms — no code required.",
    ord_cfg_reload:"Load current config", ord_cfg_save:"Save config",
    ord_cfg_saved:"Configuration saved", ord_cfg_fail:"Save failed: ",
    ord_block_map:"Field mapping",
    ord_block_map_hint:"Each row = one output column: source column (leave blank to skip) + default value used when the source cell is empty. Row order defines the output column order.",
    ord_th_target:"Output column", ord_th_source:"Source column", ord_th_default:"Default", ord_th_op:"Actions", ord_add_row:"+ Add a column",
    ord_block_tag:"Mark decision", ord_block_tag_hint:"Product code starts with any prefix → yes, otherwise no",
    ord_tag_column:"Output column", ord_tag_source:"Basis column", ord_tag_prefixes:"Prefixes (comma separated)", ord_tag_yes:"If matched", ord_tag_no:"If not matched",
    ord_block_sw:"Run options",
    ord_sw_header:"Output headers carry the required asterisk (* column)", ord_sw_autoline:"Work-order sales line no. is matched from the order detail",
    ord_sw_filtersum:"Filter subtotal / total rows", ord_sw_validate:"Validate data before converting",
    ord_block_alias:"Header alias table",
    ord_block_alias_hint:"Used to recognise varied source headers. Matching is traditional/simplified normalised, so either form works.",
    ord_cat_order_master:"Sales order master", ord_cat_order_detail:"Sales order detail", ord_cat_work_order:"Work orders",
    ord_progress:"Processed %d / %d rows", ord_tag_disabled:"(marking disabled)",
    app_title:"MES Material Archive Converter", app_subtitle:"Dingxin ERP material file → MES import template, one-click conversion",
    lang_label:"Language",
    step_source:"Step 1 · Select source file", step_source_desc:"Select the material file exported from Dingxin ERP (.xlsx)",
    step_template:"Step 2 · Select template", step_template_desc:"Select the MES «Material Archive Import Template» (.xlsx)",
    drop_hint:"You can also drag the file here",
    choose_file:"Choose file", no_file:"No file selected", file_chosen:"Selected: ", clear_file:"Clear selection",
    start_btn:"Start conversion", converting:"Converting, please wait…",
    status_ready:"Select the source file and template to begin", status_pick:"Please select both files first",
    result_title:"Conversion complete!", result_desc:"The file has been generated. Click download to import into MES.",
    download_btn:"Download result file", open_folder:"Open output folder", opening:"Opening…",
    log_title:"Conversion log", view_log:"View log", hide_log:"Hide log", copy_log:"Copy log",
    check_title:"Pre-check result", check_ok:"No issues found ✓", check_summary:"%d item(s) to review",
    issue_row:"Row", issue_col:"Column", issue_msg:"Description",
    check_lang_hint:"Messages use the language active at conversion time; re-run to change.",
    history_title:"Recent conversions", refresh:"Refresh", hist_time:"Time", hist_file:"File", hist_rows:"Rows", hist_status:"Pre-check",
    hist_none:"(no records yet)", hist_dl:"Download", hist_bad:"Has issues", hist_ok:"OK",
    hist_view:"View", hist_view_bad:"View issues", hist_report_hint:"Issue details from a previous conversion (%s):",
    advanced_title:"Advanced settings (admin)", advanced_desc:"Field mapping configuration below, maintained by admin only. Regular users do not need to change it. All settings are forms, no coding required.",
    lbl_outsheet:"Output sheet name",
    load_config:"Load current config", save_config:"Save config", add_field:"Add field", del_row:"Delete",
    export_config:"Export config", import_config:"Import config", imported_ok:"Config imported. Click «Save config» to apply.", imported_err:"Import failed: ",
    block_backup:"Config backups", block_backup_hint:"A backup is created automatically on every save, so you can roll back",
    restore:"Restore this backup", refresh_backup:"Refresh list", no_backup:"(no backups yet)",
    restored_ok:"Backup restored", restored_err:"Restore failed: ",
    th_target:"Target column", th_source:"Source(MBxxx)", th_op:"Fill method", th_param:"Parameters", th_desc:"Note", th_tpl:"Template column:",
    block_fields:"Field mapping", block_fields_hint:"Each row = how one target column is filled. * marks a required template column",
    block_unit:"Unit translation table", block_unit_hint:"Left = original symbol, right = Chinese unit name, e.g. kg → 千克",
    unit_from:"Original symbol / unit", unit_to:"Translate to", add_unit:"+ Add unit",
    block_dicts:"Dictionary table (value → translation)", block_dicts_hint:"Translate codes in the source file into readable text, e.g. 110 → 成品",
    add_dict:"+ Add dictionary", del_dict:"Delete dictionary", add_entry:"+ Add entry", dict_name:"Dictionary name", choose_dict:"(select dictionary)",
    block_wl:"Allowed value lists", block_wl_hint:"A value outside the list raises a pre-check warning (data is not changed). E.g. «Basic unit» only allows units that exist in the MES unit archive",
    add_wl:"+ Add a column's allowed values", wl_col:"Target column", wl_values:"Allowed values (comma separated)",
    adv_help:"«Fill method» decides how the column gets its value: Copy=move source column as-is; Dict=translate code via dictionary; Fixed=always write the same value; Condition=fill only when met; Keyword Bool=name contains words → «是» else «否»; Keyword Map=name contains word → write mapped value; Unit=translate via unit table; Cases=judge by multiple rules in order.",
    saved_ok:"Config saved", saved_err:"Save failed: ", loaded_ok:"Config loaded", loaded_err:"Load failed: ",
    confirm_save:F=>"Found "+F+" config problem(s):\n\n%S\n\nSave anyway?",
    rows_label:"Data rows", type_label:"Material type distribution", source_label:"Material source distribution", elapsed_label:"Elapsed",
    footer:"This tool runs offline; no data is uploaded to any server.",
    err_no_src:"Please select the source file first", err_no_tpl:"Please select the template file first",
    status_done:"Conversion succeeded", status_error:"Conversion failed",
    v_no_target:i=>"Row "+i+": target column is empty",
    v_no_dict:i=>"Row "+i+": «Dict» selected but no dictionary chosen",
    v_no_value:i=>"Row "+i+": «Fixed» selected but no value filled",
    v_no_kw:i=>"Row "+i+": «Keyword Bool» selected but no keywords filled",
    v_no_kwmap:i=>"Row "+i+": «Keyword Map» selected but no mapping defined",
    v_no_case:i=>"Row "+i+": «Cases» selected but no conditions defined",
    v_no_cond:i=>"Row "+i+": «Condition» selected but no compare value filled",
    v_dup_target:(i,s)=>"Row "+i+": target column «"+s+"» is duplicated",
    kwmap_title:"Keyword map (name contains word → write value)", kwmap_else:"When none matches, fill:", kw_col_kw:"Keyword", kw_col_val:"Write value", case_col_val:"Value",
    case_title:"Condition list (top to bottom, stops at first match)", case_default:"Default (no match) fill:", add_cond:"+ Add condition", add_kw:"+ Add keyword",
    p_trim:"Trim spaces", p_raw:"Keep as-is", p_default:"If empty fill:", p_value:"Always write:",
    p_if:"When source equals", p_then:"fill", p_else:"else fill", p_keywords:"Keywords(comma)", p_source:"Source", p_match:"Match",
    hdr_loaded:"Headers loaded: %d column(s)", hdr_fail:"Failed to read source headers (conversion unaffected): ",
    prog_title:"Converting", prog_preparing:"Reading files…", prog_fmt:"%d / %d rows · %d%%", prog_elapsed:"elapsed %s", prog_done:"Done, %d rows",
    block_profiles:"Profiles (multiple configs)", block_profiles_hint:"Save the current config as a named profile; switch between factories / templates in one click",
    profiles_none:"(no profiles yet)", profiles_load:"Load", profiles_del:"Delete", profiles_saveas:"Save as profile",
    profiles_prompt:"Name this profile (letters, digits, - , _)", profiles_saved:"Profile saved: ", profiles_err:"Profile operation failed: ",
    profiles_loaded:"Profile loaded into the form — click「Save config」to apply", profiles_del_confirm:"Delete profile「%s」?",
    block_values:"Value tables (export for business)", block_values_hint:"Export dictionaries / units to CSV for business users to edit, then import back — no JSON needed",
    values_export:"Export CSV", values_import:"Import CSV", values_confirm:"Importing replaces the whole dictionary and unit tables with the CSV content. Continue?",
    values_loaded:"Value tables loaded into the form — click「Save config」to apply", values_err:"Failed to import value tables: ",
    help_btn:"? Help", help_title:"How to use", about_btn:"About", about_title:"About this tool", close_btn:"Close",
    about_version:"Version", about_build:"Build time", about_runtime:"Runtime", about_addr:"Address",
    about_exe:"Executable", about_config:"Config file", about_log:"Log file", about_out:"Output folder", about_backup:"Backup folder",
    help_body:`<h4>Three steps</h4>
<ol><li><b>Pick the source</b>: the material file exported from Data Systems ERP (.xlsx, columns MB001 ~ MB254).</li>
<li><b>Pick the template</b>: the MES "material import template".</li>
<li><b>Start conversion</b>: download the result, which is also saved under <code>out\\</code> next to the program (timestamped, never overwritten).</li></ol>
<h4>Reading the pre-check</h4>
<p>Issues found during conversion are listed below (duplicate codes, values missing from a dictionary, units not in the MES unit master...). Click <b>View issues</b> in "Recent conversions" to reopen any past run — it survives a restart.</p>
<h4>Tray icon</h4>
<p>The program stays in the system tray: <b>right-click</b> for "Open interface / Open output folder / Quit", <b>double-click</b> to open the interface. Closing the browser tab does not quit the program; use the tray menu to quit.</p>
<h4>Configuration (admin — "⚙ Configure" in the top bar)</h4>
<p>"Field mapping" decides how each column is filled. Use "Save as profile" to keep multiple configs (different factories / templates), or export dictionaries to CSV for business users and import them back. Everything is form-based; no coding required.</p>
<h4>FAQ</h4>
<ul><li><b>Nothing happened when I clicked?</b> Look at the tray icon — the interface is running in the background.</li><li><b>Conversion failed?</b> The "Conversion log" below shows the reason; you can also send the log in <code>out\\</code> to the author.</li></ul>`,
  }
};
const OPS = ["copy","dict","num","fixed","rownum","cond","kwbool","kwmap","unit","case"];
const OPS_LABEL = {
  copy:{zh:"直接复制",zht:"直接複製",vi:"Sao chép",en:"Copy"},
  dict:{zh:"字典翻译",zht:"字典翻譯",vi:"Dịch từ điển",en:"Dict"},
  num:{zh:"转为数字",zht:"轉為數字",vi:"Số",en:"Number"},
  fixed:{zh:"固定值",zht:"固定值",vi:"Cố định",en:"Fixed"},
  rownum:{zh:"行号",zht:"行號",vi:"Số thứ tự",en:"Row No."},
  cond:{zh:"条件填充",zht:"條件填充",vi:"Điều kiện",en:"Condition"},
  kwbool:{zh:"关键词判断(是/否)",zht:"關鍵詞判斷(是/否)",vi:"Từ khóa Đúng/Sai",en:"Keyword Bool"},
  kwmap:{zh:"关键词映射",zht:"關鍵詞映射",vi:"Ánh xạ từ khóa",en:"Keyword Map"},
  unit:{zh:"单位翻译",zht:"單位翻譯",vi:"Dịch đơn vị",en:"Unit"},
  case:{zh:"多条件判断",zht:"多條件判斷",vi:"Nhiều điều kiện",en:"Cases"}
};
const MATCHES = [["eq","等于"],["ne","不等于"],["contains","包含"],["ncontains","不包含"]];
const MATCH_LABEL = {
  eq:{zh:"等于",zht:"等於",vi:"bằng",en:"equals"},
  ne:{zh:"不等于",zht:"不等於",vi:"không bằng",en:"not equals"},
  contains:{zh:"包含",zht:"包含",vi:"chứa",en:"contains"},
  ncontains:{zh:"不包含",zht:"不包含",vi:"không chứa",en:"not contains"}
};
// 目标列的业务说明（列名本身是中文，故说明保持中文，供维护人对照）
const TARGET_DESC = {
  "物料编码":"MES 内物料唯一编号",
  "物料名称":"物料名称，保留源文件原始空格",
  "物料规格":"规格描述，保留原始内容",
  "显示顺序":"按数据行号自动递增",
  "物料分类":"分组用；源为空时填「臨時分類」",
  "物料类型":"把 110/120… 按字典翻成成品/原材料",
  "基本单位":"按单位表翻译（如 KG→千克）",
  "物料来源":"把 M/P/S 翻成自制/外购/委外",
  "序列号规则编码":"固定 SN001",
  "默认发料仓库":"固定「虚拟仓」",
  "默认供应商":"取源 MB032 主供應商",
  "批次管理":"源批号管理为 N → 否，否则 是",
  "序列管理":"固定 是",
  "保质期天数":"取源有效天数（数字）",
  "时效物料":"固定 否",
  "危险品标识":"固定 否",
  "关键物料":"固定 否",
  "物料状态":"固定 正常",
  "ABC分类":"取源 ABC等級（数字）",
  "虚拟件标识":"固定 否",
  "虚设件标识":"固定 否",
  "可采购标识":"固定 否",
  "可生产标识":"成品(110) 或 名称含工序词 → 是，否则 否",
  "可销售标识":"固定 是",
  "可委外标识":"固定 否",
  "工艺路线名称":"成品→包装；名称含工序词→对应中越双语工艺名"
};

function detectLang(){
  const saved=localStorage.getItem("mes_lang"); if(saved && STR[saved]) return saved;
  const n=(navigator.language||"zh").toLowerCase();
  if(/^zh-(tw|hk|mo|hant)/.test(n)) return "zht";
  const b=n.slice(0,2); return STR[b]?b:"zh";
}
let currentLang = detectLang();
if(!STR[currentLang]) currentLang = "zh";
let cfg = null;            // 服务端返回的原始配置（小写键）
let advFields = [];        // 字段映射工作副本
let advUnitMap = {};       // 单位表工作副本
let advDicts = {};         // 字典表工作副本
let advWl = {};            // 允许值清单工作副本
let advLoaded = false;
let srcHeaders = [];       // 源文件表头（供下拉）

function t(k){ return (STR[currentLang] && STR[currentLang][k]) || STR.zh[k] || k; }
// 取翻译并按需填充占位符：
//   - 译文是函数（如 confirm_save:F=>"..."）→ 直接调用
//   - 译文是带 %d/%s 的字符串（如 check_summary）→ 按参数顺序替换
// 注意：只替换小写的 %d / %s，保留 %S 给调用方做二次替换。
function fmt(k,args){
  const v=t(k);
  if(typeof v==="function") return v.apply(null,args);
  let i=0;
  return String(v==null?"":v).replace(/%[sd]/g, ()=>(args && i<args.length) ? args[i++] : "");
}
function esc(s){ return String(s==null?"":s).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;"); }
function clone(o){ return JSON.parse(JSON.stringify(o==null?{}:o)); }
function opLabel(op){ const m=OPS_LABEL[op]; return m?m[currentLang]:op; }
function matchLabel(m){ const x=MATCH_LABEL[m]; return x?x[currentLang]:m; }
function cleanHdr(s){ return String(s==null?"":s).replace(/^\*\s*/,"").trim(); }

const TEXT_MAP = {
  app_title:"app_title", app_subtitle:"app_subtitle", lang_label:"lang_label",
  cfgNavBtn:"cfg_nav_btn", cfg_title:"cfg_title", cfg_desc:"cfg_desc",
  cfgTabMat:"cfg_tab_mat", cfgTabOrd:"cfg_tab_ord", cfgFailBanner:"cfg_load_fail",
  cfg_entry_name:"cfg_entry_name", cfg_entry_desc:"cfg_entry_desc", cfg_entry_go:"cfg_entry_go",
  step_source:"step_source", step_source_desc:"step_source_desc",
  step_template:"step_template", step_template_desc:"step_template_desc",
  srcDropHint:"drop_hint", tplDropHint:"drop_hint",
  srcLabel:"choose_file", tplLabel:"choose_file", startBtn:"start_btn",
  result_title:"result_title", result_desc:"result_desc", downloadLink:"download_btn",
  openFolderBtn:"open_folder",
  log_title:"log_title", copyLogBtn:"copy_log", toggleLogBtn:"view_log",
  check_title:"check_title", issue_row:"issue_row", issue_col:"issue_col", issue_msg:"issue_msg",
  history_title:"history_title", refreshHistBtn:"refresh",
  hist_time:"hist_time", hist_file:"hist_file", hist_rows:"hist_rows", hist_status:"hist_status",
  advanced_desc:"advanced_desc", lbl_outsheet:"lbl_outsheet",
  loadConfigBtn:"load_config", saveConfigBtn:"save_config", addFieldBtn:"add_field",
  exportConfigBtn:"export_config", importConfigBtn:"import_config",
  block_backup:"block_backup", block_backup_hint:"block_backup_hint",
  restoreBtn:"restore", refreshBackupBtn:"refresh_backup",
  th_target:"th_target", th_source:"th_source", th_op:"th_op", th_param:"th_param", th_desc:"th_desc",
  block_fields:"block_fields", block_fields_hint:"block_fields_hint",
  block_unit:"block_unit", block_unit_hint:"block_unit_hint",
  unit_from:"unit_from", unit_to:"unit_to", addUnitBtn:"add_unit",
  block_dicts:"block_dicts", block_dicts_hint:"block_dicts_hint", addDictBtn:"add_dict",
  block_wl:"block_wl", block_wl_hint:"block_wl_hint", addWlBtn:"add_wl",
  adv_help:"adv_help", footer:"footer",
  helpBtn:"help_btn", aboutBtn:"about_btn", help_title:"help_title", about_title:"about_title",
  helpClose:"close_btn", aboutClose:"close_btn",
  block_profiles:"block_profiles", block_profiles_hint:"block_profiles_hint",
  profileLoadBtn:"profiles_load", profileSaveBtn:"profiles_saveas", profileDelBtn:"profiles_del",
  block_values:"block_values", block_values_hint:"block_values_hint",
  valuesExportBtn:"values_export", valuesImportBtn:"values_import",
  // ---- 首页模块选择 ----
  backHomeBtn:"back_btn",
  home_title:"home_title", home_desc:"home_desc",
  mod_mat_name:"mod_mat_name", mod_mat_desc:"mod_mat_desc",
  mod_mat_t1:"mod_mat_t1", mod_mat_t2:"mod_mat_t2", mod_mat_t3:"mod_mat_t3", mod_mat_go:"mod_mat_go",
  mod_ord_name:"mod_ord_name", mod_ord_desc:"mod_ord_desc",
  mod_ord_t1:"mod_ord_t1", mod_ord_t2:"mod_ord_t2", mod_ord_t3:"mod_ord_t3", mod_ord_go:"mod_ord_go",
  // ---- 订单 / 工单模块 ----
  ord_step_source:"ord_step_source", ord_step_source_desc:"ord_step_source_desc",
  ord_step_work:"ord_step_work", ord_step_work_desc:"ord_step_work_desc",
  ordSrcLabel:"ord_choose_file", ordWorkLabel:"ord_choose_file",
  ordSrcDropHint:"ord_drop_hint", ordWorkDropHint:"ord_drop_hint",
  ordStartBtn:"ord_start_btn",
  ord_result_title:"ord_result_title", ord_result_desc:"ord_result_desc",
  ordDownloadLink:"ord_download", ordOpenFolderBtn:"ord_open_folder",
  ord_check_title:"ord_check_title", ord_issue_row:"issue_row", ord_issue_col:"issue_col", ord_issue_msg:"issue_msg",
  ord_history_title:"ord_history_title", ordRefreshHistBtn:"refresh",
  ord_hist_time:"hist_time", ord_hist_file:"hist_file", ord_hist_rows:"hist_rows", ord_hist_status:"hist_status",
  ord_log_title:"ord_log_title", ordCopyLogBtn:"ord_copy_log", ordToggleLogBtn:"ord_view_log",
  ord_advanced_desc:"ord_advanced_desc",
  ordCfgReloadBtn:"ord_cfg_reload", ordCfgSaveBtn:"ord_cfg_save",
  ord_block_map:"ord_block_map", ord_block_map_hint:"ord_block_map_hint",
  ord_block_tag:"ord_block_tag", ord_block_tag_hint:"ord_block_tag_hint",
  ord_block_sw:"ord_block_sw",
  ord_block_alias:"ord_block_alias", ord_block_alias_hint:"ord_block_alias_hint",
  // ---- 首次运行引导 ----
  wcTitle:"wc_title", wcDesc:"wc_desc", wcLangLabel:"wc_lang",
  wcTip1:"wc_tip1", wcTip2:"wc_tip2", wcTip3:"wc_tip3", wcOk:"wc_ok"
};

function applyLang(){
  document.documentElement.lang = currentLang==="zht"?"zh-Hant":currentLang;
  document.title = t("app_title");
  Object.keys(TEXT_MAP).forEach(id=>{ const el=document.getElementById(id); if(el) el.innerHTML=t(TEXT_MAP[id]); });
  document.getElementById("langSel").value = currentLang;
  const sn=document.getElementById("srcName"), tn=document.getElementById("tplName");
  if(sn.dataset.chosen!=="1") sn.textContent=t("no_file");
  if(tn.dataset.chosen!=="1") tn.textContent=t("no_file");
  const tb=document.getElementById("toggleLogBtn");
  if(tb) tb.textContent = document.getElementById("logBox").classList.contains("show") ? t("hide_log") : t("view_log");
  const ob=document.getElementById("ordToggleLogBtn");
  if(ob) ob.textContent = document.getElementById("ordLogBox").classList.contains("show") ? t("hide_log") : t("ord_view_log");
  applyModuleChrome();
}

document.getElementById("langSel").addEventListener("change",e=>{
  currentLang=e.target.value; localStorage.setItem("mes_lang",currentLang); applyLang();
  if(advLoaded && cfg){   // 先把编辑器内容并入再重绘，重绘后还原 cfg，未保存的改动不会被转换误用
    const bak=clone(cfg), dm=window.__dirty&&window.__dirty.mat;
    syncAdvToCfg(); loadAdvFromCfg(); cfg=bak;
    if(dm) window.__dirty.mat=true;
  }
  loadHistory();
  // 结果区已显示时，按新语言重渲染（后端生成的提示会附一行说明）
  if(lastResult && document.getElementById("resultSection").classList.contains("show")){
    renderStats(lastResult.data);
    renderChecks(lastResult.data);
  }
});

