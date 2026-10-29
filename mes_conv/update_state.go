package main

// ============================================================================
// 更新落位之后的「启动自检 + 自动回滚」
//
// 以前的漏洞：新版本 exe 一落位，下一次启动在 serveAsync 之前就把旧 exe 删了。
// 于是「新版能复制进来、但起不来」这种最坏情况没有任何退路 —— 现场只剩一个
// 打不开的程序，只能找人远程拷文件。
//
// 现在改成有状态的握手：
//   1. 落位成功 → update_state.json 里登记 pending{from,to,mode,backup}，tries=0；
//   2. 真正要启动服务时 → beginUpdateBoot() 把 tries 置 1；
//   3. HTTP 服务起得来 → markBootOK() 确认，之后才由 cleanupOldExe 归档旧 exe；
//   4. 下次启动发现 pending 且未确认（tries>=1）→ 判定「上次没起来」，
//      自动把旧 exe 换回去并带 MES_UPDATE_ROLLBACK=1 重启，界面照常可用。
//
// 为什么 tries 不放在 prepareUpdateBoot 里自增：还有一种「没真启动」的路径 ——
// 旧实例还在跑，新 exe 被双击后走单实例分支直接打开界面就退出了。这种不能算
// 一次失败的尝试，否则会误回滚一个根本没跑过的版本。
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// updateGateMu 进程内互斥：自动更新与「立即更新」按钮同时触发时只能有一个在落位。
// 以前没有它，两个流程都执行 os.Remove(backup) + Rename(cur, backup)，
// 后到的那个会把前一个刚留下的回滚副本删掉，再把自己这份改名成 backup。
var updateGateMu sync.Mutex

// updatePending 一次已落位、但尚未确认「能启动」的更新
type updatePending struct {
	From   string `json:"from"`   // 更新前的版本
	To     string `json:"to"`     // 新版本
	Mode   string `json:"mode"`   // inplace（同名就地替换）/ newfile（新文件名并存）
	Target string `json:"target"` // 新版本所在的文件名（相对 exe 目录）
	Backup string `json:"backup"` // 回滚用的旧版本文件名
	Tries  int    `json:"tries"`  // 已经尝试启动过几次
	At     int64  `json:"at"`
}

// updateState 更新状态（落盘在 exe 目录，跨版本保留）
type updateState struct {
	Pending    *updatePending `json:"pending,omitempty"`
	BootOK     string         `json:"boot_ok,omitempty"`     // 最近一次「启动自检通过」的版本
	BootAt     int64          `json:"boot_at,omitempty"`     // 确认时间
	Rollback   string         `json:"rollback,omitempty"`    // 最近一次自动回滚的说明（给「关于」看）
	RollbackAt int64          `json:"rollback_at,omitempty"` // 回滚时间
}

func updateStatePath() string { return filepath.Join(exeDir(), "update_state.json") }

func loadUpdateState() updateState {
	var s updateState
	if b, err := os.ReadFile(updateStatePath()); err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveUpdateState(s updateState) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	// 与配置文件同一套原子写：断电不会留下半截 JSON（那会被下次启动判成「没有状态」，
	// 结果就是既不会回滚也不会清理）
	if err := writeConfigFile(updateStatePath(), b); err != nil {
		appLog("更新状态写入失败（忽略）：%v", err)
	}
}

// recordUpdatePending 落位成功后登记「待确认启动」
func recordUpdatePending(mode, target, backup, to string) {
	s := loadUpdateState()
	s.Pending = &updatePending{
		From:   appVersion,
		To:     to,
		Mode:   mode,
		Target: target,
		Backup: backup,
		Tries:  0,
		At:     time.Now().Unix(),
	}
	saveUpdateState(s)
	appLog("已登记待确认更新：v%s → v%s（重启后若起不来会自动回退）", appVersion, to)
}

// prepareUpdateBoot 在启动早期判断「上一次更新是否没能起来」，需要时直接回滚并重启。
// 必须在 ensureConfig 之前调用：万一新版本的配置迁移本身有问题，先退回能用的版本。
func prepareUpdateBoot() {
	st := loadUpdateState()
	// 上一次回滚要让人看得见：回滚发生在上一个（坏掉的）进程里，
	// 那个进程随即退出，它的界面提示根本没人看得到，所以从状态文件补一条。
	notifyRollback(st)

	p := st.Pending
	if p == nil {
		return
	}
	// 跑的已经不是「待确认的那个版本」（用户手工换了 exe / 回退过 / 升到了更新的版本）
	if appVersion != p.To {
		st.Pending = nil
		saveUpdateState(st)
		return
	}
	// 已经确认过：pending 只是没清掉，顺手收尾
	if st.BootOK == p.To {
		st.Pending = nil
		saveUpdateState(st)
		return
	}
	if p.Tries < 1 {
		// 还没真正尝试过启动（例如被单实例分支挡回去过），不判失败
		return
	}
	rollbackUpdate(&st, p)
}

// notifyRollback 把「最近一次自动回滚」挂进「关于」的配置说明栏。
// 只显示最近 7 天内的，太旧的提示反而让人以为刚出过问题。
func notifyRollback(st updateState) {
	if st.Rollback == "" || st.RollbackAt == 0 {
		return
	}
	if time.Since(time.Unix(st.RollbackAt, 0)) > 7*24*time.Hour {
		return
	}
	addConfigNote("%s（%s）", st.Rollback, time.Unix(st.RollbackAt, 0).Format("01-02 15:04"))
}

// beginUpdateBoot 确认「本次是真的要启动服务了」，记一次尝试。
// 放在单实例检查之后、serveAsync 之前：被单实例挡回去的那次不算尝试。
func beginUpdateBoot() {
	st := loadUpdateState()
	if st.Pending == nil {
		return
	}
	if st.Pending.Tries < 1 {
		st.Pending.Tries = 1
		saveUpdateState(st)
		appLog("更新后首次启动：v%s（服务起来才会确认，起不来下次启动自动回退到 v%s）",
			st.Pending.To, st.Pending.From)
	}
}

// rollbackUpdate 把旧 exe 换回来并重启。
// 换不回来（文件被占用等）就只在日志与「关于」里留说明，不阻塞启动。
func rollbackUpdate(st *updateState, p *updatePending) {
	dir := exeDir()
	fail := func(why string) {
		st.Rollback = fmt.Sprintf("⚠ v%s 启动未通过自检，且自动回退失败（%s）；请用 previous/ 或 backup/ 里的旧版本手工替换", p.To, why)
		st.RollbackAt = time.Now().Unix()
		saveUpdateState(*st)
		// 这里不重启，本进程会继续启动，所以直接挂进「关于」的配置说明栏（addConfigNote 同时写日志）
		addConfigNote("%s", st.Rollback)
	}

	if p.Mode == "newfile" {
		// 并存式替换：旧 exe 一直没动过，直接把它重新拉起来即可
		old := filepath.Join(dir, p.Backup)
		if !fileExists(old) {
			fail("找不到旧版本文件 " + p.Backup)
			return
		}
		st.Pending = nil
		st.Rollback = fmt.Sprintf("v%s 启动未通过自检，已自动回退到 v%s", p.To, p.From)
		st.RollbackAt = time.Now().Unix()
		saveUpdateState(*st)
		appLog("⚠ 新版本 v%s 启动未通过自检，正在回退到 v%s…", p.To, p.From)
		// 坏版本文件此时正被本进程占用，删不掉；交给下次启动归档
		_ = os.WriteFile(pendingUpdateFile(), []byte(p.Target), 0644)
		relaunchFn(old, "MES_UPDATE_ROLLBACK=1")
		return
	}

	// 就地替换：当前运行的就是坏版本（target），旧版本是 target.old
	target := filepath.Join(dir, p.Target)
	backup := filepath.Join(dir, p.Backup)
	bad := target + ".bad"
	_ = os.Remove(bad)
	if err := os.Rename(target, bad); err != nil {
		fail("无法移走当前程序文件：" + err.Error())
		return
	}
	if err := os.Rename(backup, target); err != nil {
		_ = os.Rename(bad, target) // 还原，别把程序弄丢
		fail("旧版本无法恢复：" + err.Error())
		return
	}
	st.Pending = nil
	st.Rollback = fmt.Sprintf("v%s 启动未通过自检，已自动回退到 v%s", p.To, p.From)
	st.RollbackAt = time.Now().Unix()
	saveUpdateState(*st)
	appLog("⚠ 新版本 v%s 启动未通过自检，正在回退到 v%s…", p.To, p.From)
	_ = os.WriteFile(pendingUpdateFile(), []byte(filepath.Base(bad)), 0644)
	relaunchFn(target, "MES_UPDATE_ROLLBACK=1")
}

// markBootOK 服务起来之后确认本次更新。只有走到这里，旧 exe 才会被归档清理。
func markBootOK() {
	st := loadUpdateState()
	hadPending := st.Pending != nil
	prev := st.BootOK
	st.BootOK = appVersion
	st.BootAt = time.Now().Unix()
	st.Pending = nil
	saveUpdateState(st)
	if hadPending {
		appLog("启动自检通过：v%s 已确认可用，旧版本将归档到 previous/", appVersion)
	} else if prev != appVersion {
		appLog("启动自检通过：v%s", appVersion)
	}
}

// archiveOldExe 把旧 exe 移到 previous/ 而不是直接删除。
// 走到这里说明新版本已经能正常启动，但留一份旧文件，现场出问题时还能手工换回去。
// 归档名统一用 .exe 结尾（源文件可能是 app.exe.old / app.exe.bad），
// 这样直接改回主文件名就能跑，不用再想「.old 能不能双击」。
func archiveOldExe(old string) error {
	dir := filepath.Join(exeDir(), "previous")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	base := filepath.Base(old)
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".old") || strings.HasSuffix(lower, ".bad") {
		base = base[:len(base)-4]
	}
	ext := filepath.Ext(base)
	stem := base[:len(base)-len(ext)]
	dst := filepath.Join(dir, fmt.Sprintf("%s_%s%s", stem, time.Now().Format("20060102_150405"), ext))
	if err := moveFile(old, dst); err != nil {
		return err
	}
	appLog("旧版本已归档：previous/%s（保留最近 2 份，可手工回退）", filepath.Base(dst))
	pruneDir(dir, 2)
	return nil
}

// pruneDir 按修改时间保留最近 keep 个文件
func pruneDir(dir string, keep int) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type entry struct {
		name string
		mod  time.Time
	}
	list := make([]entry, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, entry{e.Name(), info.ModTime()})
	}
	if len(list) <= keep {
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.After(list[j].mod) })
	for _, f := range list[keep:] {
		_ = os.Remove(filepath.Join(dir, f.name))
	}
}

// acquireUpdateFileLock 目录级更新锁：共享盘上多台机器同跑时，
// 两台同时更新会互相覆盖 .part / pending_update.txt，这里用互斥文件串起来。
// 残留锁（进程被杀）超过 15 分钟视为过期，可被后来者接管。
func acquireUpdateFileLock() (func(), error) {
	p := filepath.Join(exeDir(), "update.lock")
	create := func() (*os.File, error) {
		return os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	}
	f, err := create()
	if err != nil {
		if fi, e := os.Stat(p); e == nil && time.Since(fi.ModTime()) > 15*time.Minute {
			_ = os.Remove(p)
			f, err = create()
		}
		if err != nil {
			return nil, fmt.Errorf("另一个更新正在进行中，请稍后再试（锁文件 %s）", p)
		}
	}
	fmt.Fprintf(f, "%s pid=%d at=%s\n", clientID(), os.Getpid(), time.Now().Format(time.RFC3339))
	_ = f.Close()
	return func() { _ = os.Remove(p) }, nil
}
