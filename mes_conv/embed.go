package main

// ============================================================================
// 内置输出模板
//
// 物料档案与 BOM 转换的输出底稿（表头 + 样式）直接嵌进二进制，转换页不再要求
// 上传模板文件。若请求里带了 template，仍然优先用上传的（方便临时换新版模板）。
//
// 数据红线：templates/*.xlsx 入库前必须只含表头行 —— 已剔除全部业务数据
// （BOM 模板删掉 7.3 万行旧转换数据，物料模板删掉示例行），并用脚本扫过
// sharedStrings 等全部 zip 成员，确认无品号/客户名残留后才嵌入。
// ============================================================================

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed templates/materials.xlsx templates/bom.xlsx
var embeddedTemplates embed.FS

// embeddedTemplate 把内置模板解到 dir 下并返回路径；dir 一般是转换用的临时目录。
func embeddedTemplate(dir, name string) (string, error) {
	data, err := embeddedTemplates.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("内置模板 %s 读取失败: %w", name, err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0644); err != nil {
		return "", err
	}
	return p, nil
}
