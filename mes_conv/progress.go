package main

// 转换进度：供 Web 界面在转换过程中轮询显示（大文件不再只转圈干等）。
//
// 本地单用户场景，同一时刻只会有一个转换在跑，因此用一个全局状态即可。
// 转换主体（convert.go）在行循环里调用 progressSet 上报，server 层在
// /api/progress 里读取快照；前端一边发转换请求、一边每 300ms 拉一次进度。

import (
	"sync"
	"time"
)

var convProg = struct {
	mu     sync.Mutex
	Active bool
	Done   int
	Total  int
	File   string
	Start  time.Time
}{}

// progressStart 标记一次转换开始（file 仅用于展示）。
func progressStart(file string) {
	convProg.mu.Lock()
	convProg.Active = true
	convProg.Done = 0
	convProg.Total = 0
	convProg.File = file
	convProg.Start = time.Now()
	convProg.mu.Unlock()
}

// progressSet 上报已处理行数 / 总行数。
func progressSet(done, total int) {
	convProg.mu.Lock()
	convProg.Done = done
	convProg.Total = total
	convProg.mu.Unlock()
}

// progressEnd 标记转换结束（成功或失败都要调，否则界面会一直显示进度中）。
func progressEnd() {
	convProg.mu.Lock()
	convProg.Active = false
	convProg.mu.Unlock()
}

func progressSnapshot() map[string]interface{} {
	convProg.mu.Lock()
	defer convProg.mu.Unlock()
	var elapsed int64
	if !convProg.Start.IsZero() {
		elapsed = time.Since(convProg.Start).Milliseconds()
	}
	pct := 0
	if convProg.Total > 0 {
		pct = convProg.Done * 100 / convProg.Total
		if pct > 100 {
			pct = 100
		}
	}
	return map[string]interface{}{
		"active":     convProg.Active,
		"done":       convProg.Done,
		"total":      convProg.Total,
		"percent":    pct,
		"elapsed_ms": elapsed,
		"file":       convProg.File,
	}
}
