package main

// 更新状态机（启动自检 / 自动回滚 / 目录锁 / 节流）的回归测试。
// 这些逻辑一出错就是「现场程序打不开」，比转换结果错一位严重得多，必须锁住。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type relaunchCall struct {
	target string
	env    []string
}

func setupUpdateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := exeDirOverride
	exeDirOverride = dir
	t.Cleanup(func() { exeDirOverride = old })
	return dir
}

func captureRelaunch(t *testing.T) *[]relaunchCall {
	t.Helper()
	calls := &[]relaunchCall{}
	old := relaunchFn
	relaunchFn = func(target string, extra ...string) {
		*calls = append(*calls, relaunchCall{target: target, env: extra})
	}
	t.Cleanup(func() { relaunchFn = old })
	return calls
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写 %s 失败：%v", path, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", path, err)
	}
	return string(b)
}

// 没有任何 pending 时，启动流程不该产生任何动作
func TestUpdateBootNoPending(t *testing.T) {
	setupUpdateDir(t)
	calls := captureRelaunch(t)

	prepareUpdateBoot()
	beginUpdateBoot()
	if len(*calls) != 0 {
		t.Fatalf("无 pending 时不应重启，实际 %d 次", len(*calls))
	}
}

// 首次尝试启动不能判失败：tries=0 时 prepare 什么都不做，begin 才置 1；
// 服务起来后 markBootOK 确认，之后再启动不会回滚。
func TestUpdateBootFirstAttemptThenConfirm(t *testing.T) {
	dir := setupUpdateDir(t)
	calls := captureRelaunch(t)
	saveUpdateState(updateState{Pending: &updatePending{
		From: "0.0.1", To: appVersion, Mode: "inplace",
		Target: "app.exe", Backup: "app.exe.old",
	}})

	prepareUpdateBoot()
	if len(*calls) != 0 {
		t.Fatalf("首次尝试不应回滚，实际重启 %d 次", len(*calls))
	}
	beginUpdateBoot()
	if st := loadUpdateState(); st.Pending == nil || st.Pending.Tries != 1 {
		t.Fatalf("beginUpdateBoot 应把 tries 置 1，实际 %+v", st.Pending)
	}

	markBootOK()
	st := loadUpdateState()
	if st.Pending != nil {
		t.Fatalf("确认启动后 pending 应清空，实际 %+v", st.Pending)
	}
	if st.BootOK != appVersion {
		t.Fatalf("boot_ok 应为 %s，实际 %s", appVersion, st.BootOK)
	}

	prepareUpdateBoot()
	if len(*calls) != 0 {
		t.Fatalf("已确认的版本不应回滚，实际重启 %d 次", len(*calls))
	}

	// 归档时旧 exe 还在，pending_update.txt 应被消费掉
	mustWrite(t, filepath.Join(dir, "app.exe.old"), "OLD")
	if err := os.WriteFile(pendingUpdateFile(), []byte("app.exe.old"), 0644); err != nil {
		t.Fatal(err)
	}
	cleanupOldExe()
	if fileExists(pendingUpdateFile()) {
		t.Fatal("归档成功后应删掉 pending_update.txt")
	}
	if fileExists(filepath.Join(dir, "app.exe.old")) {
		t.Fatal("旧 exe 应被移走")
	}
}

// 就地替换：上次没通过自检 → 把 .old 换回来并重启
func TestUpdateRollbackInplace(t *testing.T) {
	dir := setupUpdateDir(t)
	calls := captureRelaunch(t)
	target := filepath.Join(dir, "app.exe")
	backup := filepath.Join(dir, "app.exe.old")
	mustWrite(t, target, "NEW-BROKEN")
	mustWrite(t, backup, "OLD-WORKING")
	saveUpdateState(updateState{Pending: &updatePending{
		From: "1.0.0", To: appVersion, Mode: "inplace",
		Target: "app.exe", Backup: "app.exe.old", Tries: 1,
	}})

	prepareUpdateBoot()

	if len(*calls) != 1 {
		t.Fatalf("应重启 1 次，实际 %d 次", len(*calls))
	}
	if filepath.Clean((*calls)[0].target) != filepath.Clean(target) {
		t.Fatalf("应重启 %s，实际 %s", target, (*calls)[0].target)
	}
	if len((*calls)[0].env) == 0 || (*calls)[0].env[0] != "MES_UPDATE_ROLLBACK=1" {
		t.Fatalf("重启应带 MES_UPDATE_ROLLBACK=1，实际 %v", (*calls)[0].env)
	}
	if got := mustRead(t, target); got != "OLD-WORKING" {
		t.Fatalf("旧版本应被恢复，实际内容 %q", got)
	}
	if got := mustRead(t, target+".bad"); got != "NEW-BROKEN" {
		t.Fatalf("坏版本应改名保留，实际 %q", got)
	}
	st := loadUpdateState()
	if st.Pending != nil {
		t.Fatal("回滚后 pending 应清空，避免反复回滚")
	}
	if st.Rollback == "" {
		t.Fatal("应记下回滚说明，供「关于」显示")
	}
	if got := mustRead(t, pendingUpdateFile()); got != "app.exe.bad" {
		t.Fatalf("pending_update.txt 应指向坏版本，实际 %q", got)
	}
}

// 并存式替换：旧 exe 一直没动，回滚只需重新拉起它
func TestUpdateRollbackNewfile(t *testing.T) {
	dir := setupUpdateDir(t)
	calls := captureRelaunch(t)
	oldExe := filepath.Join(dir, "app.exe")
	mustWrite(t, oldExe, "OLD-WORKING")
	mustWrite(t, filepath.Join(dir, "v2.exe"), "NEW-BROKEN")
	saveUpdateState(updateState{Pending: &updatePending{
		From: "1.0.0", To: appVersion, Mode: "newfile",
		Target: "v2.exe", Backup: "app.exe", Tries: 1,
	}})

	prepareUpdateBoot()

	if len(*calls) != 1 {
		t.Fatalf("应重启 1 次，实际 %d 次", len(*calls))
	}
	if filepath.Clean((*calls)[0].target) != filepath.Clean(oldExe) {
		t.Fatalf("应重启旧 exe %s，实际 %s", oldExe, (*calls)[0].target)
	}
	if got := mustRead(t, oldExe); got != "OLD-WORKING" {
		t.Fatalf("旧 exe 不该被改动，实际 %q", got)
	}
	if got := mustRead(t, pendingUpdateFile()); got != "v2.exe" {
		t.Fatalf("坏版本应登记到 pending_update.txt，实际 %q", got)
	}
}

// 运行的版本已经不是「待确认的那个」，说明用户手工换过 exe：清掉状态，别乱回滚
func TestUpdateBootVersionMismatchClears(t *testing.T) {
	setupUpdateDir(t)
	calls := captureRelaunch(t)
	saveUpdateState(updateState{Pending: &updatePending{
		From: "1.0.0", To: "9.9.9", Mode: "inplace",
		Target: "app.exe", Backup: "app.exe.old", Tries: 3,
	}})

	prepareUpdateBoot()

	if len(*calls) != 0 {
		t.Fatalf("版本不匹配时不应回滚，实际 %d 次", len(*calls))
	}
	if st := loadUpdateState(); st.Pending != nil {
		t.Fatal("版本不匹配时应清掉 pending")
	}
}

// 回滚发生在上一个（坏掉的）进程里，那个进程随即退出 —— 提示必须能从状态文件
// 带进重启后的进程，否则用户只会看到「程序自己重启了一次」，不知道为什么。
func TestRollbackNoticeVisibleAfterRestart(t *testing.T) {
	setupUpdateDir(t)
	resetConfigNotes()
	defer resetConfigNotes()

	saveUpdateState(updateState{
		Rollback:   "v1.9.0 启动未通过自检，已自动回退到 v1.8.0",
		RollbackAt: time.Now().Unix(),
	})
	prepareUpdateBoot()
	notes := configNotesSnapshot()
	if len(notes) != 1 || !strings.Contains(notes[0], "已自动回退") {
		t.Fatalf("重启后应在「关于」里看到回滚说明，实际 %v", notes)
	}

	// 太旧的提示不再打扰
	resetConfigNotes()
	saveUpdateState(updateState{
		Rollback:   "很久以前的回滚",
		RollbackAt: time.Now().Add(-8 * 24 * time.Hour).Unix(),
	})
	prepareUpdateBoot()
	if got := configNotesSnapshot(); len(got) != 0 {
		t.Fatalf("超过 7 天的回滚说明不该再显示，实际 %v", got)
	}
}

// 回滚失败（旧文件不见了）不能卡住启动
func TestUpdateRollbackMissingBackupKeepsBooting(t *testing.T) {
	dir := setupUpdateDir(t)
	calls := captureRelaunch(t)
	mustWrite(t, filepath.Join(dir, "app.exe"), "NEW-BROKEN")
	saveUpdateState(updateState{Pending: &updatePending{
		From: "1.0.0", To: appVersion, Mode: "inplace",
		Target: "app.exe", Backup: "app.exe.old", Tries: 1,
	}})

	prepareUpdateBoot()

	if len(*calls) != 0 {
		t.Fatalf("回滚失败不应重启，实际 %d 次", len(*calls))
	}
	st := loadUpdateState()
	if st.Rollback == "" {
		t.Fatal("回滚失败要留下说明")
	}
	if got := mustRead(t, filepath.Join(dir, "app.exe")); got != "NEW-BROKEN" {
		t.Fatal("回滚失败时不能破坏现有 exe")
	}
}

// previous/ 只保留最近 2 份，避免共享盘上无限堆积
func TestArchiveOldExeKeepsTwo(t *testing.T) {
	dir := setupUpdateDir(t)
	prev := filepath.Join(dir, "previous")
	if err := os.MkdirAll(prev, 0755); err != nil {
		t.Fatal(err)
	}
	// 造 3 份旧归档，时间依次变新
	for i := 0; i < 3; i++ {
		p := filepath.Join(prev, "old_"+string(rune('a'+i))+".exe")
		mustWrite(t, p, "x")
		ts := time.Now().Add(time.Duration(i) * time.Hour)
		_ = os.Chtimes(p, ts, ts)
	}
	live := filepath.Join(dir, "app.exe.old")
	mustWrite(t, live, "OLD")

	if err := archiveOldExe(live); err != nil {
		t.Fatalf("归档失败：%v", err)
	}
	ents, err := os.ReadDir(prev)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 {
		t.Fatalf("previous/ 应只保留 2 份，实际 %d 份", len(ents))
	}
	if fileExists(live) {
		t.Fatal("归档后原文件应被移走")
	}
}

// 目录锁：同一时刻只能一个更新流程；残留锁超时后可被接管
func TestUpdateFileLock(t *testing.T) {
	setupUpdateDir(t)

	release, err := acquireUpdateFileLock()
	if err != nil {
		t.Fatalf("首次加锁失败：%v", err)
	}
	if _, err := acquireUpdateFileLock(); err == nil {
		t.Fatal("第二次加锁应当失败")
	}
	release()
	if _, err := acquireUpdateFileLock(); err != nil {
		t.Fatalf("释放后应能重新加锁：%v", err)
	}

	// 模拟进程被杀留下的陈旧锁
	old := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(filepath.Join(exeDir(), "update.lock"), old, old); err != nil {
		t.Fatal(err)
	}
	r2, err := acquireUpdateFileLock()
	if err != nil {
		t.Fatalf("陈旧锁应可被接管：%v", err)
	}
	r2()
}

// 并发写配置不能写出半截 JSON（临时文件名必须唯一）
func TestWriteConfigFileConcurrent(t *testing.T) {
	dir := setupUpdateDir(t)
	path := filepath.Join(dir, "mapping.json")

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			body := `{"schema":1,"fields":[` + `{"target":"C` + string(rune('A'+n%26)) + `"}` + `]}`
			_ = writeConfigFile(path, []byte(body))
		}(i)
	}
	wg.Wait()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读配置失败：%v", err)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("并发写入后配置不是合法 JSON：%v（内容 %q）", err, string(b))
	}
	// 临时文件不应残留
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("残留临时文件：%s", e.Name())
		}
	}
}

// 节流：连不上更新源只做短退避，不算「检查过」
func TestAutoUpdateBackoff(t *testing.T) {
	setupUpdateDir(t)
	if autoUpdateCheckedRecently() {
		t.Fatal("初始状态不应节流")
	}
	if n := autoUpdateMarkFailed(); n != 1 {
		t.Fatalf("首次失败计数应为 1，实际 %d", n)
	}
	if !autoUpdateCheckedRecently() {
		t.Fatal("失败后应进入退避窗口，本次启动不再重试")
	}
	// 模拟退避到期：不该被当成「已检查过」（Last 仍为 0）
	s := loadUpdateCheckState()
	s.Next = time.Now().Add(-time.Second).Unix()
	saveUpdateCheckState(s)
	if autoUpdateCheckedRecently() {
		t.Fatal("退避到期且从未成功检查过，应允许再次检查")
	}

	autoUpdateMarkChecked()
	if autoUpdateCheckedRecently() == false {
		t.Fatal("检查成功后应进入 24h 节流")
	}
	if s := loadUpdateCheckState(); s.Fails != 0 || s.Next != 0 {
		t.Fatalf("成功后应重置退避，实际 %+v", s)
	}
}
