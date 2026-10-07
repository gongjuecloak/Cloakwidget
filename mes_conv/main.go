package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	cli := flag.Bool("cli", false, "无界面模式：直接按 mapping.json 转换")
	src := flag.String("src", "D:/User/Cloak_Zeng/Download/20261006 匯入 577筆.xlsx", "源文件")
	tpl := flag.String("tpl", "D:/User/Cloak_Zeng/Download/物料档案导入模版 (3).xlsx", "模板文件")
	out := flag.String("out", "", "输出文件（默认：out/ 下按时间命名）")
	port := flag.Int("port", 8731, "Web UI 端口")
	lang := flag.String("lang", "zh", "提示语言：zh 简体 / zht 繁体 / vi 越南语 / en 英文")
	nobrowser := flag.Bool("nobrowser", false, "启动时不自动打开浏览器（适合开机自启/托盘常驻）")
	orderSrc := flag.String("order", "", "订单源文件（给了就走订单/工单模块）")
	workSrc := flag.String("work", "", "工单源文件（给了就走订单/工单模块）")
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
		return
	}

	if *cli {
		cfg := ensureConfig()
		outPath := *out
		if outPath == "" {
			dir := filepath.Join(exeDir(), "out")
			_ = os.MkdirAll(dir, 0755)
			outPath = filepath.Join(dir, "MES物料档案_"+time.Now().Format("20060102_150405")+".xlsx")
		}
		L := msgs(*lang)
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
		return
	}

	// 首次运行确保配置文件存在（两个模块各一份）
	ensureConfig()
	ensureOrdersConfig()

	// 单实例：已有实例在运行就直接打开它的界面并退出，避免两个托盘图标 / 两个服务。
	if !acquireSingleInstance() {
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

	// 启动 HTTP 服务（跑在后台 goroutine，前台留给托盘）
	actualPort, started := serveAsync(*port)
	if !started {
		return
	}

	// 主 goroutine 交给系统托盘消息循环：程序常驻右下角，右键可「打开界面 / 打开输出文件夹 / 退出」
	runTray(actualPort, *lang)
}