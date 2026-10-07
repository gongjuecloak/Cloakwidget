# MES 转换工具

鼎新 ERP 导出文件 → MES 导入表的本地转换工具。单个 exe，双击即用，离线运行，数据不上传。

工具里有两个**互相独立**的模块，打开程序先选模块：

| 模块 | 输入 | 输出 |
|---|---|---|
| **物料档案** | 鼎新物料档（INVMB，MB001 ~ MB254） | MES「物料档案导入模板」单表 |
| **订单 / 工单** | 鼎新订单档 + 製令（工单）档 | MES「销售订单主表 / 销售订单明细 / 工单」三张表 |

两个模块共用同一套外壳：系统托盘、转换进度、日志、预检报告、四语界面（简中 / 繁中 / 越南语 / 英文）。

---

## 目录结构

```
mes_conv/                         Go 源码（工具本体）
  main.go                         入口：CLI 模式 / 托盘 + Web UI 模式
  version.go                      版本号（全项目唯一一处）
  server.go                       HTTP 接口、启动、单实例、日志
  convert.go                      物料档案转换引擎
  config.go                       物料档案配置结构 + 内置默认配置
  orders.go                       订单/工单引擎（核心）
  orders_flow.go                  订单/工单转换流程
  orders_config.go                订单模块配置结构 + 内置默认配置
  orders_server.go                订单模块 HTTP 接口
  orders_i18n.go                  订单模块四语文案
  webui.html                      界面（内嵌进 exe，同目录放文件可覆盖）
  mapping.json                    物料档案配置（运行期可改）
  mbfields.go                     MB 字段中文名对照
  report.go / progress.go         报告落盘、进度上报
  tray_windows.go / single_windows.go   托盘图标、单实例互斥
  versioninfo.json / appicon.ico  版本资源与图标
  *_test.go / test_*.js           回归测试与 i18n 校验

build.sh                          一键构建：校验 → 测试 → 编译 → 同步分享版
gen_golden.py                     用 Python 原版生成订单基准（改造前跑一次即可）
cmp_orders.py                     订单输出与 Python 基准逐格比对
verify_unit.py                    物料档案输出与参考文件逐格比对
verify_norm.py                    单位归一回归
verify_orders_http.py             订单模块 HTTP 端到端验证
verify_render_v13.py              无头浏览器渲染验证（三视图 + 四语）
MES物料档案转换工具_分享版/        发给别人的成品（exe + 说明）
build.py                          **历史文件**：Go 版之前的 Python 原型，仅作参考
```

---

## 构建

需要 Go 1.21+（本机用 1.27.1）、bash。

```bash
./build.sh
```

脚本会：校验 `version.go` 与 `versioninfo.json` 版本一致 → `go vet` → `go test` → 生成图标/版本资源 → 编译 exe（注入构建时间）→ 同步到分享版目录。

单独跑某一步：

```bash
cd mes_conv
go vet ./...
go test ./...                                   # 回归测试
go run . -cli -lang zh                          # 命令行转换物料档案
go run .                                        # 起 Web UI（托盘常驻）
```

---

## 常用命令行参数

| 参数 | 说明 |
|---|---|
| `-cli` | 无界面模式，按 `mapping.json` 转物料档案 |
| `-order <file>` / `-work <file>` | 走订单/工单模块（给任意一个即可） |
| `-out <path>` | 指定输出文件（默认 `out/` 下按时间戳命名） |
| `-port 8731` | 界面端口 |
| `-lang zh\|zht\|vi\|en` | 提示语言 |
| `-nobrowser` | 启动时不自动开浏览器（开机自启 / 常驻场景） |

界面还支持深链：`http://127.0.0.1:8731/?view=orders&lang=en` 可直接进入订单模块并指定语言。

---

## 输出与日志

- 输出：`out/` 下按 `名字_YYYYMMDD_HHMMSS.xlsx` 命名，同秒冲突自动加序号，永不覆盖
- 日志：`out/*.log`（每次转换）+ `logs/server_日期.log`（服务级）
- 预检报告：`out/reports/*.json`，界面「最近转换 → 查看问题」可随时翻回任意一次，**重启程序也不丢**
- 配置备份：`backup/`，每次保存配置自动留一份，保留最近 20 份

---

## 验证

改动后按顺序跑一遍（都在仓库根目录执行）：

```bash
# 1. 单元/回归测试 + i18n 词条完整性
cd mes_conv && go test ./... && node test_i18n.js && node test_fmt.js && cd ..

# 2. 订单模块：与 Python 原版输出逐格比对（首次需先跑 gen_golden.py 生成基准）
python gen_golden.py
python cmp_orders.py

# 3. 物料档案：与人工参考文件逐格比对
cd mes_conv && ./物料档案转换工具.exe -cli -lang zh -out ../verify.xlsx && cd ..
python verify_unit.py

# 4. 接口端到端 + 界面渲染
python verify_orders_http.py
python verify_render_v13.py
```

---

## 版本管理

- 版本号只在 `mes_conv/version.go` 定义；`versioninfo.json` 与 `CHANGELOG.md` 需同步
- 每次发版：改版本号 → 更新 CHANGELOG → `./build.sh` → `git tag v1.x.0`
- **不要提交** exe、`out/`、`logs/`、`backup/`、`configs/`、运行期 `orders.json`（已在 `.gitignore`）
