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
- `Bridge`：web media-bridge → 原生（元数据/播放态/进度/分享/外链）。
- `WebLink`：原生 → web 的反向控制缝。

## 构建步骤
1. 工具链：JDK 17、Android SDK（compileSdk 35 / build-tools 35.0.0）、Gradle 8.7。
   本机已装于 `E:\Program\tools`（`scripts/setup-android-toolchain.sh` 可重装）；
   `android/local.properties` 指向 SDK（已 gitignore）。
2. 在仓库根生成 web 产物与壳：
   ```bash
   go tool mygo build -platform web
   node scripts/web-shell.mjs        # 注入 fs-bridge / media-bridge，产出 build/web-shell
   node scripts/android-assets.mjs   # 拷入 android/app/src/main/assets/web
   ```
3. 命令行构建（或 Android Studio 打开 `android/` Run `app`）：
   ```bash
   export JAVA_HOME="E:\Program\tools\jdk17"
   export ANDROID_HOME="E:\Program\tools\android-sdk"
   E:/Program/tools/gradle-8.7/bin/gradle.bat -p android assembleDebug --no-daemon
   ```

## 移动端已就绪
- GD 音乐台内置一键安装（设置 → 插件 → 安装；或 + 菜单）：优先拉取仓库
  `TingnikuaiyuKeqing1120/gd-music-plugin` 的最新 release（记录 Origin，可检查更新），
  断网时回退 App 内置副本。
- GitHub 私有仓库同步（设置 → 同步）：令牌 + 仓库名，独立开关同步设置/歌单/插件，
  增量 merge + last-write-wins，定期 pull + 改动 debounce push。
- 桌面-only UI 已收敛：浏览器/壳内隐藏托盘、原生文件夹/zip 选择、 reveal-in-folder、
  桌面更新器；设置覆盖层有关闭按钮。

## 已知待验证 / 待办（真机）
- 后台/锁屏续播、音频焦点、耳机键（P3）；Android 14+ 后台启动前台服务的限制。
- OPFS 在目标 WebView 版本的可用性与配额（缓存上限 CacheMB）。
- 通知图标暂用系统 `ic_media_play`，后续换品牌图标。