package main

// 版本号集中在这里 —— 全项目唯一一处。
//
// 改版本时同步做三件事：
//   1. 改下面的 appVersion
//   2. 改 versioninfo.json 里的 FileVersion / ProductVersion / 注释
//   3. 在 CHANGELOG.md 顶部加一节
// build.sh 会读这里的值做校验，不一致会直接报错停下。

const appVersion = "1.10.2"

// buildTime 由构建时注入：-ldflags "-X main.buildTime=..."
// 未注入时保留「开发构建」，界面「关于」里能一眼看出是本地跑的还是正式包。
var buildTime = "开发构建"
