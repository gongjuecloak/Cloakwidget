# -*- coding: utf-8 -*-
"""夹具路径解析：回归脚本依赖的真实源档不入库，这里统一处理查找与报错。

本仓库是公开的，_fmttest/ 下的源档与产物含真实业务数据（客户简称、品号、金额等），
所以只入库脚本，夹具由使用者自行准备。准备方式二选一：

  1. 放到工作区下的 _fixtures/ 目录（推荐，脚本自动发现）；
  2. 用环境变量指定目录：
       MES_SRC_DIR   物料侧：模板档 + 物料源档
       MES_ORD_DIR   订单侧：订单档 + 工单档（不设置则复用 MES_SRC_DIR）

文件名不必与原来完全一致，find() 会按候选名依次尝试。
"""
import os
import sys

# 工作区根目录 = _fmttest/ 的上一级（脚本放哪都能正确定位，换机器无需改代码）
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

_CANDIDATE_DIRS = (
    os.path.join(ROOT, "_fixtures"),
    os.path.join(ROOT, "_fmttest", "fixtures"),
)


def _pick_dir(env_name):
    v = os.environ.get(env_name)
    if v:
        if os.path.isdir(v):
            return v
        sys.stderr.write("⚠ 环境变量 %s 指向的目录不存在：%s\n" % (env_name, v))
    for d in _CANDIDATE_DIRS:
        if os.path.isdir(d):
            return d
    return _CANDIDATE_DIRS[0]


SRC = _pick_dir("MES_SRC_DIR")
ORD = _pick_dir("MES_ORD_DIR") if os.environ.get("MES_ORD_DIR") else SRC

# 常用夹具的候选文件名（第一个是原始导出名）
TPL_NAMES = ("物料档案导入模版 (3).xlsx", "物料档案导入模版.xlsx", "物料档案导入模板.xlsx")
MAT_FULL_NAMES = ("20261006 匯入 577筆.xlsx", "20261006 匯入 577笔.xlsx")
ORD_NAMES = ("訂單資料.xlsx", "订单资料.xlsx")
WORK_NAMES = ("工單資料.xlsx", "工单资料.xlsx")


def find(d, names):
    """在目录 d 下按候选名找文件；找不到时打印清晰提示并退出。"""
    if isinstance(names, str):
        names = (names,)
    for n in names:
        p = os.path.join(d, n)
        if os.path.exists(p):
            return p
    sys.stderr.write(
        "✗ 找不到夹具：%s\n"
        "  查找目录：%s\n"
        "  请把夹具放进 %s，或用环境变量 MES_SRC_DIR / MES_ORD_DIR 指向实际目录。\n"
        % (" / ".join(names), d, _CANDIDATE_DIRS[0])
    )
    raise SystemExit(2)


def read(path):
    with open(path, "rb") as f:
        return f.read()
