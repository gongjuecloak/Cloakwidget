//go:build windows

package main

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// 真的建一个 Win32 弹出菜单，再用 GetMenuStringW 把每一项的文本读回来，
// 断言托盘右键菜单的结构与文案 —— 不需要真的把菜单弹到桌面上。
//
// 读回手段：
//   GetMenuItemCount(hMenu)
//   GetMenuStringW(hMenu, i, buf, len, MF_BYPOSITION)
//   GetMenuState(hMenu, i, MF_BYPOSITION) & MF_GRAYED

const (
	mfByPosition = 0x0400
)

func menuItemText(hMenu uintptr, i int) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetMenuStringW.Call(hMenu, uintptr(i),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), mfByPosition)
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func menuItemGrayed(hMenu uintptr, i int) bool {
	st, _, _ := procGetMenuState.Call(hMenu, uintptr(i), mfByPosition)
	// 0xFFFFFFFF 表示取不到；MF_GRAYED/MF_DISABLED 同为 0x0001
	return st != 0xFFFFFFFF && st&mfGrayed != 0
}

func dumpMenu(t *testing.T, hMenu uintptr, lang string) []string {
	cnt, _, _ := procGetMenuItemCount.Call(hMenu)
	n := int(int32(cnt))
	t.Logf("=== 语言 %s：菜单共 %d 项", lang, n)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		txt := menuItemText(hMenu, i)
		gray := menuItemGrayed(hMenu, i)
		out = append(out, txt)
		mark := "  "
		if gray {
			mark = "灰"
		}
		t.Logf("    [%d] %s %q", i, mark, txt)
	}
	return out
}

func TestTrayMenuStructure(t *testing.T) {
	for _, lang := range []string{"zh", "zht", "vi", "en"} {
		hMenu := buildTrayMenu(lang)
		if hMenu == 0 {
			t.Fatalf("%s：CreatePopupMenu 失败", lang)
		}
		items := dumpMenu(t, hMenu, lang)
		_, _, _ = procDestroyMenu.Call(hMenu)

		if len(items) != 9 {
			t.Fatalf("%s：菜单应有 9 项（1 摘要 + 3 分隔线 + 5 命令），实际 %d", lang, len(items))
		}

		T := trayTexts(lang)
		// 0 摘要（灰）/ 1 分隔 / 2 打开界面 / 3 打开输出文件夹 / 4 分隔
		// 5 重启程序 / 6 检查更新 / 7 分隔 / 8 退出
		if items[0] == "" {
			t.Errorf("%s：第 0 项应有「最近转换」摘要", lang)
		}
		if !strings.Contains(items[0], strings.TrimSuffix(strings.Split(T.Recent, "%s")[0], "：")) {
			t.Errorf("%s：摘要 %q 应含 %q 的前缀", lang, items[0], T.Recent)
		}
		if items[1] != "" || items[4] != "" || items[7] != "" {
			t.Errorf("%s：第 1 / 4 / 7 项应为分隔线，实际 %q %q %q", lang, items[1], items[4], items[7])
		}
		want := map[int]string{
			2: T.Open, 3: T.Folder, 5: T.Restart, 6: T.Update, 8: T.Quit,
		}
		for i, w := range want {
			if items[i] != w {
				t.Errorf("%s：第 %d 项应为 %q，实际 %q", lang, i, w, items[i])
			}
		}
	}
}

func TestTraySummaryItemGrayed(t *testing.T) {
	hMenu := buildTrayMenu("zh")
	if hMenu == 0 {
		t.Fatal("CreatePopupMenu 失败")
	}
	defer func() { _, _, _ = procDestroyMenu.Call(hMenu) }()

	if !menuItemGrayed(hMenu, 0) {
		t.Error("第 0 项（最近转换摘要）应当是灰显不可点的")
	}
	for _, i := range []int{2, 3, 5, 6, 8} {
		if menuItemGrayed(hMenu, i) {
			t.Errorf("第 %d 项是命令项，不该灰显", i)
		}
	}
}

func TestTrayTextsAllLanguages(t *testing.T) {
	langs := []string{"zh", "zht", "vi", "en"}
	for _, lang := range langs {
		T := trayTexts(lang)
		vals := map[string]string{
			"Tip": T.Tip, "Open": T.Open, "Folder": T.Folder, "Recent": T.Recent,
			"NoRecent": T.NoRecent, "Restart": T.Restart, "Update": T.Update, "Quit": T.Quit,
			"Rows": T.Rows, "Problems": T.Problems, "Warnings": T.Warnings,
		}
		for k, v := range vals {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s：托盘文案 %s 为空", lang, k)
			}
		}
		if !strings.Contains(T.Recent, "%s") {
			t.Errorf("%s：Recent 模板应带 %%s（%q）", lang, T.Recent)
		}
	}
	// 四语必须互不相同，避免复制粘贴漏改
	seen := map[string]string{}
	for _, lang := range langs {
		T := trayTexts(lang)
		if prev, dup := seen[T.Quit]; dup {
			t.Errorf("托盘「退出」文案 %s 与 %s 完全相同（%q），疑似漏翻", lang, prev, T.Quit)
		}
		seen[T.Quit] = lang
	}
}

func TestRecentSummaryFormat(t *testing.T) {
	// 没有历史记录时给「暂无记录」，绝不能是空串或裸 %s
	got := recentSummary("zh")
	if strings.TrimSpace(got) == "" || strings.Contains(got, "%s") {
		t.Errorf("无记录时的摘要异常：%q", got)
	}
	if !strings.Contains(got, "最近转换") {
		t.Errorf("摘要应带「最近转换」前缀，实际 %q", got)
	}
	for _, lang := range []string{"zht", "vi", "en"} {
		if s := recentSummary(lang); strings.TrimSpace(s) == "" {
			t.Errorf("%s：摘要为空", lang)
		}
	}
}
