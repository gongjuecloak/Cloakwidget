package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func outputIsExternal(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	outDir, err := filepath.Abs(filepath.Join(exeDir(), "out"))
	return err != nil || !strings.EqualFold(filepath.Clean(filepath.Dir(abs)), filepath.Clean(outDir))
}

func baseOrEmpty(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Base(path)
}

func main() {
	cli := flag.Bool("cli", false, "无界面模式：直接按 mapping.json 转换")
	src := flag.String("src", "", "源文件（-cli 模式必填）")
	tpl := flag.String("tpl", "", "模板文件（-cli 模式必填）")
	out := flag.String("out", "", "输出文件（默认：out/ 下按时间命名）")
	port := flag.Int("port", 8731, "Web UI 端口")
	lang := flag.String("lang", "zh", "提示语言：zh 简体 / zht 繁体 / vi 越南语 / en 英文")
	nobrowser := flag.Bool("nobrowser", false, "启动时不自动打开浏览器（适合开机自启/托盘常驻）")
	orderSrc := flag.String("order", "", "订单源文件（给了就走订单/工单模块）")
	workSrc := flag.String("work", "", "工单源文件（给了就走订单/工单模块）")
	nosingle := flag.Bool("nosingle", false,
		"调试用：跳过单实例检查（可在别的端口再起一个，便于自动化验证）")
	flag.Parse()
	noBrowser = *nobrowser

	// 订单 / 工单模块的命令行模式
	if *orderSrc != "" || *workSrc != "" {
		oc := ensureOrdersConfig()
		outPath := *out
		if outPath == "" {
			dir := filepath.Join(exeDir(), "out")
			_ = os.MkdirAll(dir, 0755)
			outPath = filepath.Join(dir, "订单工单_"+time.Now().Format("20060102_150405")+".xlsx")
		}
		logf := func(s string) { appLog("%s", s) }
		started := time.Now()
		rep, err := ConvertOrders(oc, *orderSrc, *workSrc, outPath, *lang, logf)
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			os.Exit(1)
		}
		appLog("订单主表 %d / 明细 %d / 工单 %d", rep.OrderMaster, rep.OrderDetail, rep.WorkOrders)
		if rep.IssueTotal > 0 {
			appLog("预检：%d 处需要关注", rep.IssueTotal)
		}
		for _, w := range rep.Warnings {
			appLog("%s", "  * "+w)
		}
		nowStr := time.Now().Format("2006-01-02 15:04:05")
		base := filepath.Base(outPath)
		sources := []string{}
		for _, source := range []string{baseOrEmpty(*orderSrc), baseOrEmpty(*workSrc)} {
			if source != "" {
				sources = append(sources, source)
			}
		}
		source := strings.Join(sources, " + ")
		reportID := saveReport(&ConvertReport{
			ID: strings.TrimSuffix(base, filepath.Ext(base)) + "_" + time.Now().Format("20060102_150405.000000000") + ".json", Time: nowStr, Module: "orders",
			Source: source, Template: ordersMsgs(*lang)["label_work"], Output: base,
			Rows: rep.OrderMaster + rep.OrderDetail + rep.WorkOrders, ElapsedMS: time.Since(started).Milliseconds(),
			IssueTotal: rep.IssueTotal, Errors: rep.Errors, Warnings: rep.Warnings, Issues: rep.Issues,
		})
		if err := appendOrdersHistory(HistoryItem{
			Time: nowStr, File: base, Rows: rep.OrderMaster + rep.OrderDetail + rep.WorkOrders,
			Source: source, Template: ordersMsgs(*lang)["label_work"], Issues: rep.IssueTotal,
			Warnings: len(rep.Warnings), Report: reportID, Module: "orders", External: outputIsExternal(outPath),
		}); err != nil {
			appLog("订单 CLI 转换历史保存失败：%v", err)
		}
		return
	}

	if *cli {
		if *src == "" || *tpl == "" {
			fmt.Fprintln(os.Stderr, "错误：-cli 模式需要同时指定 -src 与 -tpl")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "用法：")
			fmt.Fprintln(os.Stderr, `  "物料档案转换工具.exe" -cli -src "源档.xlsx" -tpl "物料档案导入模板.xlsx"`)
			fmt.Fprintln(os.Stderr, `  "物料档案转换工具.exe" -cli -src "源档.xlsx" -tpl "模板.xlsx" -out "结果.xlsx" -lang zh`)
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "不传任何参数直接双击运行时，程序会打开图形界面，无需命令行参数。")
			os.Exit(2)
		}
		cfg := ensureConfig()
		outPath := *out
		if outPath == "" {
			dir := filepath.Join(exeDir(), "out")
			_ = os.MkdirAll(dir, 0755)
			outPath = filepath.Join(dir, "MES物料档案_"+time.Now().Format("20060102_150405")+".xlsx")
		}
		L := msgs(*lang)
		started := time.Now()
		rep, err := Convert(cfg, *src, *tpl, outPath, *lang)
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			os.Exit(1)
		}
		appLog("%s", fmt.Sprintf(L["wrote"], outPath))
		appLog("%s", fmt.Sprintf(L["rows"], rep.Rows))
		if rep.IssueTotal > 0 {
			appLog("%s", fmt.Sprintf(L["precheck"], rep.IssueTotal))
		}
		for _, e := range rep.Errors {
			appLog("%s", "  ! "+e)
		}
		for _, x := range rep.Warnings {
			appLog("%s", "  * "+x)
		}
		appLog("%s", fmt.Sprintf(L["type"], formatDist(rep.Types)))
		appLog("%s", fmt.Sprintf(L["source"], formatDist(rep.Sources)))
		appLog("%s", L["done"])
		nowStr := time.Now().Format("2006-01-02 15:04:05")
		base := filepath.Base(outPath)
		reportID := saveReport(&ConvertReport{
			ID: strings.TrimSuffix(base, filepath.Ext(base)) + "_" + time.Now().Format("20060102_150405.000000000") + ".json", Time: nowStr,
			Source: filepath.Base(*src), Template: filepath.Base(*tpl), Output: base, Rows: rep.Rows,
			ElapsedMS: time.Since(started).Milliseconds(), IssueTotal: rep.IssueTotal,
			Errors: rep.Errors, Warnings: rep.Warnings, Issues: rep.Issues, Types: rep.Types, Sources: rep.Sources,
		})
		if err := appendHistory(HistoryItem{
			Time: nowStr, File: base, Rows: rep.Rows, Source: filepath.Base(*src), Template: filepath.Base(*tpl),
			Issues: rep.IssueTotal, Warnings: len(rep.Warnings), Report: reportID, External: outputIsExternal(outPath),
		}); err != nil {
			appLog("物料 CLI 转换历史保存失败：%v", err)
		}
		return
	}

	// 更新兜底：上一次更新落位后若没能通过启动自检，这里直接回退到旧版本并重启。
	// 必须放在 ensureConfig 之前——万一新版本的配置迁移本身有问题，
	// 先退回一个确定能用的版本，别把配置和程序一起卡死。
	prepareUpdateBoot()

	// 首次运行确保配置文件存在（两个模块各一份）
	ensureConfig()
	ensureOrdersConfig()

	// 记录本次运行的版本，并把「上一个版本」记下来：版本变了就在界面上主动
	// 弹一次「这个版本改了什么」（更新日志编译在 exe 里，内网机器也看得到）。
	updatedFrom = trackVersion()

	// 注意：旧 exe 不在这里清理。它可能是新版本起不来时唯一的回退副本，
	// 要等服务真正起来（markBootOK）之后才归档到 previous/。

	// 单实例：已有实例在运行就直接打开它的界面并退出，避免两个托盘图标 / 两个服务。
	// -nosingle 时跳过（调试/验证用：可以在另一个端口独立跑一份，不去打扰用户正在用的那个）。
	// 就地更新后的重启进程带 MES_UPDATE_RESTART：旧进程即将退出，不做重复检测，直接接管。
	isRestart := os.Getenv("MES_UPDATE_RESTART") == "1"
	if !isRestart && !*nosingle && !acquireSingleInstance() {
		if p := findRunningInstance(*port); p > 0 {
			appLog("检测到本程序已在运行（端口 %d），直接打开该界面。", p)
			if !noBrowser {
				openBrowser(fmt.Sprintf("http://127.0.0.1:%d/", p))
			}
		} else {
			appLog("检测到本程序已在运行，但没找到界面端口；请查看右下角托盘图标。")
		}
		return
	}

	// 走到这里说明单实例锁拿到了、确实要起服务，记一次「启动尝试」。
	// 起不来时，下次启动就能据此判定为失败并自动回滚。
	beginUpdateBoot()

	// 启动 HTTP 服务（跑在后台 goroutine，前台留给托盘）
	actualPort, started := serveAsync(*port)
	if !started {
		// 服务没起来就不确认本次更新：下次启动会按「未通过自检」自动回退
		appLog("服务启动失败，本次更新未确认（下次启动将自动回退到旧版本）")
		return
	}

	// 自检通过：确认本次更新，并把旧 exe 归档到 previous/（保留最近 2 份，可手工回退）
	markBootOK()
	cleanupOldExe()

	// 后台静默自动更新：启动后稍等再查 GitHub，不阻塞启动；查不到 / 无更新都不影响使用
	go autoUpdate()

	// 主 goroutine 交给系统托盘消息循环：程序常驻右下角，右键可「打开界面 / 打开输出文件夹 / 退出」
	runTray(actualPort, *lang)
}
