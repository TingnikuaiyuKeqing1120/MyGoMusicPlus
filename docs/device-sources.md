# 设备端独立音源方案

核对日期为 2026-11-23。本文回应独立于 GD Studio 获取音源、各设备自行运行且无需自有服务器的目标，当前完成源码与公开文档可行性核对。具体渠道的实时搜索、账号登录和播放仍需原型验证。

## 可行的运行方式

每台设备内运行平台适配器，直接请求上游平台的搜索、详情和播放地址接口，播放器连接返回的 CDN 音频。协议编码、签名、数据转换、URL 刷新和缓存均在设备内完成。应用开发者主要承担适配代码维护和客户端更新，基础音乐服务仍由各上游提供。

播放器现有插件提供 `search`、`stream`、`lyrics`、歌单和喜欢等入口，宿主已有 MD5、AES-ECB、定时器、网络权限及媒体请求头能力。桌面解码器支持 AAC/MP4 和分片 M4A，Android 使用 Media3。这些代码可复用，新的源需要实现自己的平台协议。代码入口为 `internal/plugin/host.go`、`internal/plugin/headers.go`、`internal/music/music.go`、`internal/player/decoded.go` 和 `internal/player/formats_test.go`。

## 渠道比较

| 渠道 | 设备如何取得音频 | 可以提供的内容 | 主要成本与限制 |
| --- | --- | --- | --- |
| B 站公开视频音轨 | 搜索或解析 BV 链接，取得视频分 P 的 CID，再解析 DASH 中的音频地址 | MV、现场、翻唱、游戏与动画音乐等视频音轨 | 标题、UP 主与真实歌名艺人需要整理，音质按返回音轨选择，登录状态、地区和平台风控会影响结果 |
| 网易云平台直连 | 本地实现请求编码，调用搜索、详情、歌词和播放地址接口 | 中文元数据、歌单、歌词，以及当前账号可播放的歌曲 | 匿名、登录、会员与地区对应不同可用范围，请求协议与会话需要持续维护 |
| YouTube / SoundCloud / Bandcamp | 在本地使用或移植站点提取器，解析播放资源 | 各平台公开音乐与音轨 | 地区网络可达性、页面变更、音频格式、账号要求各异，Android 与桌面复用同一实现需要适配 |
| Audius | 查询公共 REST 接口和发现节点，解析曲目音频 | 平台上的创作者音乐、搜索、歌单与发现 | 曲库与商业流媒体各有侧重，客户端依赖协议网络中的可用节点，API 仍有限额 |

B 站路径可从 yt-dlp 的现有实现确认，代码直接读取 `dash.audio`、Dolby 与 FLAC 音轨，保留各音轨 URL、编码和码率，并处理登录与缺失格式。它也实现搜索和 WBI 请求签名，可作为设备端适配的参考。国内音乐内容的实际搜索质量需要通过样本评估。[yt-dlp Bilibili 提取器](https://github.com/yt-dlp/yt-dlp/blob/master/yt_dlp/extractor/bilibili.py)

网易云适配项目把搜索与歌曲地址封装成独立模块，分别指向平台的 `cloudsearch/pc` 和 `song/enhance/player/url/v1`。由此可以推断，移植请求处理部分到设备端具备技术可行性。模块涉及的 Node.js 依赖、请求编码和会话能力需要逐项迁移到当前宿主。[搜索模块](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/main/module/cloudsearch.js) · [歌曲地址模块](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/main/module/song_url_v1.js)

NewPipeExtractor 是可以独立使用的 Java 提取库，明确支持 YouTube、SoundCloud、Bandcamp、PeerTube 和 media.ccc.de。Android 可以评估直接集成，桌面需要相应运行时或协议移植。项目采用 GPL-3.0，直接集成时需要确定对应分发方式。它的修复记录显示站点变化会持续带来维护工作。[NewPipeExtractor](https://github.com/TeamNewPipe/NewPipeExtractor) · [修复记录](https://github.com/TeamNewPipe/NewPipeExtractor/releases)

Audius 官方文档说明多数只读接口可匿名调用，API Key 可提高额度。发现节点负责索引曲目、用户与歌单，并提供 REST 查询。可以设计多节点选择以减少对单个入口的依赖，客户端本身无需部署发现节点。[Audius API](https://docs.audius.co/api/) · [发现节点架构](https://docs.audius.co/learn/architecture/discovery-node/)

## 建议的首个原型

优先验证 B 站音频直连，先完成 BV 链接播放和普通音频流，再接入搜索。视频 ID 与分 P ID 能作为明确的资源身份，现有分片 M4A 播放器可用于端到端验证。网易云作为下一条独立音源，补齐音乐元数据、歌词和个人账号范围内的播放。

一次完整原型应验证以下行为。

1. 在 Windows 和 Android 分别从同一公开视频链接取得音频，播到结束，暂停、继续与拖动进度均正常。
2. 保存 `平台 + 内容 ID + 分 P ID`，播放前按需解析临时 URL。地址过期后重新解析，下载和播放缓存继续按稳定身份对应。
3. 搜索结果保存来源页面、标题、上传者、时长与可用音轨信息。视频标题和上传者可以先按原信息展示，完善音乐元数据时保留原始来源。
4. 多平台结果分别保留资源 ID，候选版本可同时展示。后续匹配可参考歌名、艺人、时长和版本，现场、翻唱与录音室版本需要明确展示。
5. 查询与解析在每台设备本地执行，凭证按用户选择管理，喜欢、歌单继续接入已有同步。临时播放地址和网络缓存按设备保存。

## 当前工程还需要补的能力

- **Android 的通用设备内 HTTP。** 本轮 GD 原生通道限定两个 GD 域名和三个 GET 路径。独立平台适配需要按插件网络权限提供请求方法、请求体、必要请求头和响应状态，并逐跳检查重定向目标。桌面已有 Go HTTP 执行路径。
- **平台所需的请求编码。** 当前宿主提供 MD5 和 AES-ECB。B 站 WBI 和网易云当前协议需要分别核对，新增能力以具体适配器的必要条件为准。
- **账号会话入口。** 对匿名访问无法取得的内容，可让用户使用自己的平台账号授权，并明确显示当前可用音质、试听或不可播放状态。
- **地址刷新与错误分类。** 区分临时 URL 失效、账号状态、地区限制、平台拒绝和网络失败，控制重试次数，保留实际音质与格式。
- **跨端运行范围。** Windows、macOS、Linux 可使用现有原生请求栈，Android 可补设备内请求桥。普通浏览器受各平台 CORS 和可设置请求头限制，需要逐个平台验证兼容性。

## 判断

设备端独立取源具有现成项目和现有播放器能力作为基础，B 站与网易云值得优先做小型原型。可用曲库和音质由各设备的网络、账号与上游返回结果共同决定。主要长期投入是适配更新、会话处理和音乐版本识别，自有服务器运维可从方案中省去。
