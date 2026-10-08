# _fmttest —— 端到端回归测试

这里的脚本针对**已构建的 exe** 跑端到端验证（起本地服务 → 走 HTTP 接口 → 查产物）。
它们不依赖任何 Python 测试框架，直接 `python xxx_test.py` 即可。

> 真实业务数据**不入库**（本仓库是公开的）。脚本只入库，夹具要你自行准备。

---

## 一、准备夹具

把下面这些源档放进**工作区根目录的 `_fixtures/`**（脚本会自动发现），
或者用环境变量指向实际所在目录：

| 环境变量 | 用途 | 默认 |
|---|---|---|
| `MES_SRC_DIR` | 物料侧：模板档 + 物料源档 | `<工作区>/_fixtures` |
| `MES_ORD_DIR` | 订单侧：订单档 + 工单档 | 未设置时复用 `MES_SRC_DIR` |
| `MES_EXE` | 被测 exe 的文件名 | `_newbuild.exe` |
| `APP_PORT` | 服务端口（各脚本已错开，通常不用改） | 见脚本顶部 |

需要的文件（文件名不必完全一致，脚本会按候选名依次尝试）：

| 文件 | 用在哪 | 说明 |
|---|---|---|
| `物料档案导入模版.xlsx` | 全部物料用例 | MES 的「物料档案导入模板」（原始名带 ` (3)` 后缀也能识别） |
| `20261006 匯入 577筆.xlsx` | `batch_test.py` | 577 行物料源档，用于与批量结果做对照 |
| `訂單資料.xlsx` | `batch_test.py` / `dup_test.py` | 订单档 |
| `工單資料.xlsx` | `batch_test.py` / `dup_test.py` | 製令（工单）档 |

`_fmttest/` 下另有一批**派生夹具**（`拆_1.xlsx`、`拆_2.xlsx`、`dup_mat.xlsx`、
`err_mat.xlsx`、`out_*.xlsx` 等），同样被 `.gitignore` 排除。它们由脚本自身生成，
或由源档派生 —— 详见各脚本头部注释。

设置环境变量的例子（Git Bash）：

```bash
export MES_SRC_DIR="D:/导出目录"
export MES_ORD_DIR="D:/导出目录/订单"
```

---

## 二、先构建被测 exe

脚本默认测 `mes_conv/_newbuild.exe`，构建一次即可：

```bash
cd mes_conv
go build -o _newbuild.exe .
```

---

## 三、脚本清单

| 脚本 | 覆盖内容 | 断言数 |
|---|---|---|
| `dup_test.py` | 重复行 / 重复编码 + Excel 错误值（物料按编码、订单按「订单编号+产品编码」组合键） | 25 |
| `tpl_test.py` | 模板列变化检测（基准记录 / 一致 / 加列 / 缺列 / 换序 / 重置） | 22 |
| `batch_test.py` | 批量多文件一次转（合并结果与单文件逐格一致） | 14 |
| `sys_test.py` | 系统设置 / 口令锁 / 局域网 / 开机自启 / 排障包 / 统计 / 检查更新 | 52 |
| `cmp.py` | 三种伪装格式（改名的 .xls、UTF-8 CSV、GBK CSV）与基准逐格比对 | — |

> `sys_test.py` 会短暂改写 `mes_conv/sys_settings.json` 与注册表 `HKCU\...\Run` 的自启项，
> 脚本结束（含异常）时会自动还原。

运行：

```bash
python _fmttest/dup_test.py
python _fmttest/tpl_test.py
python _fmttest/batch_test.py
python _fmttest/sys_test.py
python _fmttest/cmp.py
```

---

## 四、相关：Go 侧单测

`_fmttest` 之外，`mes_conv/` 下还有 Go 单元测试（不需要夹具）：

```bash
cd mes_conv && go test ./...
```

其中 `srcgrid_test.go` 里有一组「真实样本」测试，需要环境变量 `MES_SAMPLE_DIR`
指向样本目录；未设置时自动跳过。
