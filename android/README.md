# MyGo Music++ · Android 壳（源码骨架，待真机验证）

Kotlin WebView 壳承载 `build/web-shell` 的 mygo-web 产物（wasm），复用全部现有 UI 与 Go 核心。
本目录为**源码骨架**：本机无 Android SDK/Gradle/真机，尚未编译与真机验证；请在 Android Studio 中构建。

## 组成
- `MainActivity`：WebView 加载 `file:///android_asset/web/index.html`；JS 桥 `AndroidBridge`；
  SAF 文件选择（插件 zip / `<input type=file>`）与下载导出（`ACTION_CREATE_DOCUMENT`）；
  `mygo-music://` App Links 转给 web（`window.__mygoLink` + `mygo-link` 事件）。
- `PlaybackService`：`mediaPlayback` 前台服务 + 媒体通知 + `MediaSessionCompat` + 音频焦点；
  通知/锁屏/耳机键经 `WebLink` 调 web 的 mediaSession handlers；焦点丢失自动暂停。
- `Bridge`：web media-bridge → 原生（元数据/播放态/进度/分享/外链）。
- `WebLink`：原生 → web 的反向控制缝。

## 构建步骤
1. 装 **JDK 17** 与 **Android SDK**（compileSdk 35），Android Studio 打开 `android/`（自带 Gradle wrapper 生成）。
2. 在仓库根生成 web 产物与壳：
   ```bash
   go tool mygo build -platform web
   node scripts/web-shell.mjs        # 注入 fs-bridge / media-bridge，产出 build/web-shell
   node scripts/android-assets.mjs   # 拷入 android/app/src/main/assets/web
   ```
3. Android Studio 连接真机/模拟器 Run `app`。

## 已知待验证 / 待办（真机）
- 后台/锁屏续播、音频焦点、耳机键（P3）。
- OPFS 在目标 WebView 版本的可用性与配额（缓存上限 CacheMB）。
- `mygo-music://` 安装链接在 web 侧的接线（P5）。
- 桌面-only UI 收敛（Notch/托盘/自启/本地文件夹）（P5）。
- 通知图标暂用系统 `ic_media_play`，后续换品牌图标。