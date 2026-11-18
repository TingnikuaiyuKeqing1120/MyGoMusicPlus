# MyGo Music++ · Android 壳

Kotlin WebView 壳承载 `build/web-shell` 的 mygo-web 产物（wasm），复用全部现有 UI 与 Go 核心。
已用本机工具链编译出可上手测试的 debug APK；真机播放/后台行为仍待实测。

## 产物
- `android/app/build/outputs/apk/debug/app-debug.apk`（构建输出）
- 仓库根 `MyGoMusicPlus-debug.apk`（便于取用的副本）
- 包名 `com.mygomusic.plus`，minSdk 26（Android 8.0+），targetSdk 35，debug 签名。

## 组成
- `MainActivity`：WebView 经 `WebViewAssetLoader` 从虚拟 https 域
  （`appassets.androidplatform.net/assets/web/`）加载壳（安全上下文，fetch/OPFS 可用）；
  JS 桥 `AndroidBridge`；系统返回键转给 web 的 `__mygoBack`（`back_web.go` 按上下文
  关对话框/抽屉/设置页/播放页/回退，无处可退才退出）；
  SAF 文件选择与下载导出（`ACTION_CREATE_DOCUMENT`）；`mygo-music://` App Links 转给 web
  （`window.__mygoLink` + `mygo-link` 事件，web 侧 `links_web.go` 接收）。
- `PlaybackService`：`mediaPlayback` 前台服务 + 媒体通知 + `MediaSessionCompat` + 音频焦点；
  通知/锁屏/耳机键经 `WebLink` 调 web 的 mediaSession handlers；焦点丢失自动暂停。
- `Bridge`：web media-bridge → 原生（元数据/播放态/进度/分享/外链）；GitHub OAuth
  设备流的两步请求（`githubDeviceStart`/`githubDevicePoll`，GitHub 登录端点不对浏览器
  fetch 开放 CORS，由壳代发，结果经 `__mygoDeviceResult` 回传）；`installApk` 下载更新
  并经 FileProvider 交给系统安装器（进度经 `__mygoInstallState` 回传）。
- `WebLink`：原生 → web 的反向控制缝（媒体控制 / 设备流应答 / 安装进度）。

## 构建步骤
1. 工具链：Node.js 22.18+、JDK 17、Android SDK（compileSdk 35 / build-tools 35.0.0）、Gradle 8.7。
   本机已装于 `E:\Program\tools`（`scripts/setup-android-toolchain.sh` 可重装）；
   `android/local.properties` 指向 SDK（已 gitignore）。
2. 在仓库根生成 web 产物与壳：
   ```bash
   node scripts/build-web.mjs        # go tool mygo build -platform web，并注入 config 的版本号
   node scripts/web-shell.mjs        # 注入 fs-bridge / media-bridge，产出 build/web-shell
   node scripts/android-assets.mjs   # 拷入 android/app/src/main/assets/web
   ```
   （mygo 的 web 编译不带 package flags，版本号经 `MYGO_LDFLAGS` 注入，
   否则 App 内版本为空、任何 release 都会被当作更新。）
3. 命令行构建（或 Android Studio 打开 `android/` Run `app`）：
   ```bash
   export JAVA_HOME="E:\Program\tools\jdk17"
   export ANDROID_HOME="E:\Program\tools\android-sdk"
   E:/Program/tools/gradle-8.7/bin/gradle.bat -p android assembleDebug --no-daemon
   ```

## 移动端已就绪
- GD 音乐台内置一键安装（设置 → 插件 → 安装；或 + 菜单）：优先拉取仓库
  `TingnikuaiyuKeqing1120/gd-music-plugin` 的最新 release（记录 Origin，可检查更新），
  断网时回退 App 内置副本。Android 由原生壳下载 GitHub 发布附件和源码 ZIP。
- GitHub 私有仓库同步（设置 → 同步）：令牌 + 仓库名，独立开关同步设置/歌单/插件，
  增量 merge + last-write-wins，定期 pull + 改动 debounce push。
  令牌可经 OAuth 设备流获取（设置 → 同步 → 通过 GitHub 登录，显示设备码，
  授权后自动填入；OAuth App client ID 见 `internal/app/oauth.go`）。
- App 自动更新（设置 → 更新）：每天检查 `TingnikuaiyuKeqing1120/MyGoMusicPlus`
  的 releases，发现新版可下载 APK 交系统安装器（需允许安装未知应用）；
  纯浏览器中打开 release 页面。
- 安装后，启用的音源可从手机首页顶部切换。设置页用返回箭头逐级返回。
- 本地音乐可从首页或设置 → 插件 → 本地文件添加。Android 使用系统目录选择器并保留
  只读授权，扫描和播放时按需读取。支持 File System Access 的浏览器可以选择目录，
  重开页面后若浏览器收回授权，可再次选择同一目录。
- 外观选择「系统」时跟随 Android 深色模式，切换系统主题时无需重启播放器。

## 发版流程
1. 同步改两处版本：`mygo.config.ts` 的 `version`、`android/app/build.gradle.kts` 的
   `versionName` 与 `versionCode`（code 只升不降，系统安装器拒绝更低的 code）。
2. 按上述步骤构建，复制 APK 到仓库根。
3. 提交推送后 `gh release create v<版本> MyGoMusicPlus-debug.apk#MyGoMusicPlus-<版本>.apk`；
   旧版 App 的每日检查会发现新版，下载经系统安装器升级。

## 已知待验证 / 待办（真机）
- 后台/锁屏续播、音频焦点、耳机键（P3）；Android 14+ 后台启动前台服务的限制。
- OPFS 在目标 WebView 版本的可用性与配额（缓存上限 CacheMB）。
- 通知图标暂用系统 `ic_media_play`，后续换品牌图标。
- OAuth 设备流与 APK 自动更新的真机交互（安装未知应用授权提示）。
