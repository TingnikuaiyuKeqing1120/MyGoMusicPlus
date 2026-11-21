# MyGo Music++ · Android 壳

Kotlin WebView 承载 `build/web-shell` 的界面和 Go 插件运行时。Android 的 Media3 ExoPlayer 前台服务负责音频解码、输出和队列播放。
Plus Release 提供可覆盖升级的 Android APK。文末列出模拟器已验证的范围与仍待真机确认的行为。

## 产物
- `android/app/build/outputs/apk/debug/app-debug.apk`（构建输出）
- 仓库根 `MyGoMusicPlus-debug.apk`（便于取用的副本）
- 包名 `com.mygomusic.plus`，minSdk 26（Android 8.0+），targetSdk 35，debug 签名。

## 组成
- `MainActivity`：WebView 经 `WebViewAssetLoader` 从虚拟 https 域
  （`appassets.androidplatform.net/assets/web/`）加载壳。支持 OPFS 的 WebView 可跨会话保存数据，OPFS 不可用时使用会话内存。
  JS 桥 `AndroidBridge`；系统返回键转给 web 的 `__mygoBack`（`back_web.go` 按上下文
  关对话框/抽屉/设置页/播放页/回退，无处可退才退出）；
  WebView 网页下载可通过系统文件选择器导出（`ACTION_CREATE_DOCUMENT`）。播放器内下载的歌曲保存在应用自己的 Downloads 目录，可从「已下载」播放或删除。`mygo-music://` 自定义协议链接转给 web
  （`window.__mygoLink` + `mygo-link` 事件，web 侧 `links_web.go` 接收）。
- `PlaybackService` 使用 Media3 ExoPlayer、MediaSession 和 `mediaPlayback` 前台服务。通知、锁屏和耳机键直接控制原生播放器，音频焦点和耳机断开由 Media3 处理。
- `NativeSources` 向 Go 插件获取播放地址和请求头，原生加载线程读取音频。服务预先解析后续两首歌曲，已准备的队列可在界面繁忙时自动切换。新地址解析仍依赖 Go 插件运行时。
- `NativeCache` 保存原生在线播放缓存，完整缓存可离线重播。设置中的音频缓存上限与清理操作覆盖这部分缓存，已有 Go 缓存仍可读取。
- `NativeAudioFiles` 将 OPFS 中的下载文件或旧缓存按内容哈希校验，分块复制成原生可读文件。原始下载保留，播放副本按 512 MiB 目标清理，正在使用和最近准备的文件保留。系统目录授权文件直接使用 `content://` URI。
- `NativeAudioProcessor` 在音频线程应用音量、响度归一化和频谱计算。服务回传播放位置、缓冲状态、错误和输出欠载计数。
- `PlayerWebRuntime` 在播放服务存活期间保留 Go 插件运行时，Activity 重建后重新接入界面。系统终止整个进程后，通过已有会话记录恢复。
- `Bridge` 和 `web/native-playback.js` 传递队列、控制、文件准备和播放状态，也提供分享和外链入口。GitHub OAuth
  设备流的两步请求（`githubDeviceStart`/`githubDevicePoll`，GitHub 登录端点不对浏览器
  fetch 开放 CORS，由壳代发，结果经 `__mygoDeviceResult` 回传）；`ApkUpdates` 下载并保存
  更新包，经 FileProvider 交给系统安装器。`__mygoInstallState` 回传进度和已下载版本。
- `WebLink` 回传设备流应答和安装进度。

## 构建步骤
1. 工具链使用 Node.js 22.18+、JDK 17、Android SDK 36、Gradle 8.11.1、AGP 8.10.1 和 Kotlin 2.2.10。Media3 版本为 1.11.1，minSdk 26、targetSdk 35。
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
   E:/Program/tools/gradle-8.11.1/bin/gradle.bat -p android assembleDebug --no-daemon
   ```

## 移动端已就绪
- GD 音乐台内置一键安装（设置 → 插件 → 安装；或 + 菜单）：优先拉取仓库
  `TingnikuaiyuKeqing1120/gd-music-plugin` 的最新 release（记录 Origin，可检查更新），
  断网时回退 App 内置副本。Android 由原生壳下载 GitHub 发布附件和源码 ZIP。
- 在「设置 → 同步」中，可分别启用偏好设置、本地文件的歌单与喜欢、插件及其数据。新建同步仓库默认为私有。复用已有仓库时，请先确认仓库可见性。「同步插件和数据」用一个总开关同步所有插件的设置与标准 JSON 存储，包括歌单、收藏、历史及保存的登录信息。缓存目录和下载音频排除在外。两端均升级到 v0.4.0 后使用。
  令牌可经 OAuth 设备流获取（设置 → 同步 → 通过 GitHub 登录，显示设备码）。点「复制授权码」即可粘贴到 GitHub。同步时可看到处理阶段和数量，完成后显示各音源的歌单与喜欢数。设备页优先交给
  支持该链接的 GitHub App，也可以直接用浏览器打开。授权后返回同步页，令牌会自动填入。
  OAuth App client ID 见 `internal/app/oauth.go`。
- App 自动更新（设置 → 更新）：每天检查 `TingnikuaiyuKeqing1120/MyGoMusicPlus`
  的 releases。Android 可选择 GitHub 官方源或 GH-Proxy 国内镜像下载 APK，版本和校验值均从 GitHub 获取。
  下载完成后 APK 保存在应用文件目录，取消安装或重开应用后可在更新页点「安装已下载的版本」。
  升级后会清理已不适用的安装包。纯浏览器中打开 release 页面。
- 安装后，启用的音源可从手机首页顶部切换。设置页用返回箭头逐级返回。
- 本地音乐可从首页或设置 → 插件 → 本地文件添加。Android 使用系统目录选择器并保留
  只读授权，扫描和播放时按需读取。支持 File System Access 的浏览器可以选择目录，
  重开页面后若浏览器收回授权，可再次选择同一目录。
- 外观选择「系统」时跟随 Android 深色模式，切换系统主题时无需重启播放器。

## 发版流程
1. 同步改两处版本：`mygo.config.ts` 的 `version`、`android/app/build.gradle.kts` 的
   `versionName` 与 `versionCode`（code 只升不降，系统安装器拒绝更低的 code）。
2. 按上述步骤构建，复制 APK 到仓库根。
3. 按[发版说明](../RELEASING.md#mygo-music-plus)把 APK 和本次的 Windows 安装包放进同一个 Release。
   旧版 App 的每日检查会发现新版，下载后交给系统安装器升级。

## 验证范围

### 本机带界面模拟器

Windows 开发机保留 Android 15 Google APIs x86_64 虚拟设备 `test35`。桌面的「Android 测试模拟器」快捷方式打开模拟器窗口，其他项目的 APK 也可拖入安装。

- SDK 位于 `E:/Program/tools/android-sdk`，使用 WHPX 硬件加速、主机 GPU 和 3 GB 内存。
- 通用启动脚本位于 `E:/Program/tools/android-testing/Start-Android.ps1`，同目录的 `README.md` 记录使用方法。
- 虚拟设备数据位于 `%USERPROFILE%/.android/avd/test35.avd`，关闭窗口后保留。
- 可通过参数安装和启动应用，命令会明确指定模拟器。

```powershell
& E:/Program/tools/android-testing/Start-Android.ps1 `
  -Apk E:/Program/mygo-music/android/app/build/outputs/apk/debug/app-debug.apk `
  -Package com.mygomusic.plus
```

### 布局与交互回归

`internal/app/batch_test.go` 和 `mobile_test.go` 验证 320、390 dp 手机宽度及桌面宽度下的复选框、批量操作、播放器和队列导航。设置 `MM_FRAMES` 可输出截图供目视检查。新增 UI 测试运行五次。

```powershell
$env:CGO_ENABLED = '0'
go test ./internal/app -run 'TestTrackCheckboxSelection|TestBatchLikeCheckboxes|TestMobilePlayerNavigation|TestTouchScrollRegions' -count=5
node --test web/touch-scroll.test.mjs
```

`web/touch-scroll.js` 将滚动区域内的竖向触摸拖动转为精确滚轮输入。Go 视图通过 `ObserveInput(touchScrollInput)` 标记滚动区域，拖动超过阈值时取消原点击。横向控件拖动和封面墙继续使用原有手势。

模拟器已验证授权链接的应用选择与浏览器回退，以及更新包在取消安装、冷启动后保留，断网时可再次打开系统安装器。启动器使用黑猫图标，播放通知由 Media3 提供。

2026-11-19 的布局修复在 Android 15 模拟器中验证了歌单上滑、封面随列表滚出、左侧复选框、两首歌曲批量喜欢、批量加入歌单和批量下载。已核对测试插件的存储及下载文件。全屏队列、切歌和系统返回也完成了交互验证。模拟器保留「界面测试音源」供本机复查，测试数据独立于日常播放器。

### 原生播放回归

2026-11-21 在 Android 15 模拟器中完成以下检查，使用 MP3 和 WAV 测试音频。

- 在线播放、关闭 Wi-Fi 和移动数据后的缓存重播，以及从「已下载」页面播放保存的文件。
- 系统目录选择器授权后，通过 `content://` 直接播放文件。
- WebView 主线程阻塞 10 秒时，原生播放位置和音频输出帧持续前进，输出欠载次数为 0。阻塞期间自动切换到下一首也通过验证。
- 锁屏播放、系统媒体命令的暂停、继续、上一首和下一首。移除界面任务并确认 Activity 销毁后，音频继续输出，重新打开接回同一个运行时。
- 冷启动恢复下载清单和暂停位置，核对下载音频内容仍为原来的 81,083 字节。WASM 的用户数据路径现在指向既有持久化资料目录，新下载和会话记录能正常保存。
- 音源失败后显示错误，通过界面播放按钮和系统媒体播放命令均可重新请求音源。

Go 原生播放边界测试覆盖队列、进度、重复点击继续、过期请求、错误重试、随机电台退出和缓存音质。JavaScript 测试覆盖请求头传递、分块文件准备、失败清理及目录 URI。运行 `go test ./internal/...`、`go tool mygo vet .` 和 `node --test web/native-playback.test.mjs web/touch-scroll.test.mjs`。本机模拟器记录保存在忽略目录 `build/verify-native/`。

手机蓝牙杂音的实际改善、厂商省电策略、长时间后台播放及其他编码格式仍待真机验证。真实账号授权、系统安装权限确认后的完整升级流程和不同 WebView 的 OPFS 持久性也仍需真机覆盖。`CacheMB` 控制在线播放缓存，OPFS 配额由系统和 WebView 决定。
