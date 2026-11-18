# MyGo Music Plus

MyGo Music Plus plays local music and plugin sources. The Windows player has a native Go UI built with MyGo. Android runs the same player as WebAssembly in a Kotlin WebView shell. The Web player can also be built for a browser. [MyGo framework](https://github.com/egoist/mygo)

Download the Windows installer or standalone EXE, a macOS or Linux package, or the Android APK from the Plus release page. [Download MyGo Music Plus](https://github.com/TingnikuaiyuKeqing1120/MyGoMusicPlus/releases/latest)

### Desktop installation and updates

On Windows x64, the installer adds the player to the Start menu, registers its link scheme, and supports signed updates under **Settings → Updates**. Install v0.3.0 manually once when upgrading from v0.2.3 or earlier.

For a single file, download **MyGoMusicPlus-standalone-windows-x64.exe** and open it directly. To upgrade, quit the player and replace the EXE. The installer and standalone builds both use `%APPDATA%\MyGo Music Plus` for playlists, settings and plugins. Switch between them by quitting the running player first. Copying the EXE to another PC leaves this profile on the original PC. Plus Dev has a separate test profile.

macOS packages are available for Intel and Apple Silicon. They are ad-hoc signed and have not been notarized by Apple. On macOS and Linux, writable per-user installations can update in the player. Linux x64 and ARM64 system packages use the package manager. Android checks for releases under Settings → Updates and opens the system installer.

To import your library, quit **MyGo Music**, then open **Settings → Sync → Import from Original** in Plus. Confirm **Import and Restart**. Plus exits normally and imports before opening the new session. It copies the local library and folder paths, playback session, preferences, window state, downloaded speech models, plugins and their stored data (including playlists, sign-ins and permission grants). Original preferences, the original library and matching plugins take precedence. Plus-only settings and plugins stay in place. The Dev build imports into its own test profile.

The result dialog gives the backup path. Profiles are under `%APPDATA%` on Windows, `~/Library/Application Support/` on macOS, and `$XDG_CONFIG_HOME/` (usually `~/.config/`) on Linux. To undo an import, quit Plus, move its profile aside and copy the backup's `before` directory back under the original Plus profile name. For Windows installations predating the settings button, quit both players and run the [standalone migration tool](resources/windows).

## Features

- Local files (MP3, FLAC, Ogg Vorbis, WAV, and ALAC, FLAC or AAC in .m4a and .aac) with tags, covers and `.lrc` lyrics
- Playlists, liked songs, search, albums and artists
- Now Playing with lyric stage styles, cover wall, queue, shuffle and repeat
- Desktop tray and media keys, Android media notification and lock-screen controls
- Audio cache, fades on pause and seek, loudness normalization
- Session restore
- On-device podcast transcription on supported native desktops
- English and Simplified Chinese
- Plugins can add accounts, catalogs, podcasts, stations, comments and pages of their own
- Install GD Music from Settings → Plugins. Plus fetches the plugin from GitHub and offers its bundled copy if the fetch fails.
- Select multiple tracks to add, remove or download where the source supports it. Drag rows to reorder an editable playlist or drop them on a sidebar playlist.
- Queue downloads at a chosen quality. Pause and resume within the session. A server that supports HTTP Range can continue from the saved bytes. The Downloaded page keeps completed tracks for local playback and deletion. Desktop users can also reveal the saved file.
- Sync preferences, the built-in local source's playlists and likes, and installed plugins through a GitHub repository. Each category has its own switch. Use GitHub device sign-in on desktop or Android, or enter a token in a browser. Plus creates new sync repositories as private. If you supply an existing repository, check its visibility yourself. Plugin sync includes plugin settings, which can contain credentials.

## Build

```bash
go tool mygo dev     # live reload with the MyGo Music Dev identity
go tool mygo build   # package with the upstream identity by default
go test ./internal/...
```

Build the local Windows x64 package of MyGo Music Plus in PowerShell.

```powershell
$env:MYGO_PLUS_DESKTOP = '1'
go tool mygo build -platform windows/amd64 -o build/windows-plus
```

The Plus package can run alongside MyGo Music. It uses its own settings folder
and `mygo-music-plus://` links. Use `-skip-installer` for the unpackaged app alone.

For a double-clickable Windows test build, run the following in PowerShell:

```powershell
$env:MYGO_PLUS_DESKTOP = 'dev'
go tool mygo build -debug -skip-installer -platform windows/amd64 -o build/windows-plus-dev
```

Open `build/windows-plus-dev/windows-amd64/MyGo Music Plus Dev.exe`. The app
keeps its profile in `%APPDATA%\MyGo Music Plus Dev` and registers
`mygo-music-plus-dev://` at startup. Close it before rebuilding at the same path.

For a standalone Web build, run `node scripts/build-web.mjs` and then `node scripts/web-shell.mjs`. Serve `build/web-shell/` over HTTPS or localhost. With OPFS available, the player keeps files between sessions. Without it, files stay in memory for the session. The Android build packages this same directory. [Android build instructions](android/README.md#构建步骤)

## Podcast captions

On supported native desktops, the player transcribes episodes with sherpa-onnx and Qwen3-ASR, downloading the required files on request. Web and Android hide the transcription controls. Native libraries come from the upstream MyGo Music prerelease. [Library release](https://github.com/egoist/mygo-music/releases/tag/sherpa-onnx-2.0.1)

## Plugins

A plugin contains a `plugin.json` and a JavaScript or TypeScript entry point. In Settings → Plugins, use + to install from GitHub and the ⋯ menu to remove a plugin. Desktop also supports installation from a local folder or ZIP and can open the plugins folder. Plugins that launch a helper need a binary for the target desktop platform. [Example plugin](examples/demo-plugin)

```json
{
  "id": "example",
  "name": "Example",
  "version": "1.0.0",
  "minAppVersion": "0.1.8",
  "main": "index.js",
  "playback": "stream",
  "permissions": { "network": ["api.example.com"] },
  "settings": [{ "key": "apiKey", "label": "API Key", "help": "From your account page", "link": "https://example.com/keys" }],
  "navigation": [
    { "builtin": "home" },
    { "title": "Charts", "page": "charts", "icon": "chart" },
    { "title": "Stations", "builtin": "stations" }
  ]
}
```

```js
export async function search({ query, kind, offset }) {
  const res = await fetch(`https://api.example.com/search?q=${encodeURIComponent(query)}&type=${kind}&offset=${offset}`, {
    headers: { authorization: `Bearer ${host.setting("apiKey")}` },
  });
  return { tracks: (await res.json()).items.map(toTrack), more: false };
}

export function stream({ id, quality }) {
  return { url: `https://api.example.com/audio/${id}.mp3`, type: "mp3" };
}
```

Export a function for each thing the plugin does. Each takes one object of parameters and returns a value or a promise. What it doesn't export, the app leaves out of its pages.

| Export | Parameters | Returns |
| --- | --- | --- |
| `signIn`, `signOut`, `account` | | `{id, name, imageUrl, page}`; `account` returns `null` when signed out. `page` is a `browse` page of the listener's own, which the account menu opens as My Page |
| `setSetting` | `{key, value}` | |
| `home` | | `Browse` |
| `browse` | `{page}` | `Browse` |
| `search` | `{query, kind, offset}`, where `kind` is `tracks`, `albums`, `artists`, `playlists` or `shows` | `{tracks or albums or ..., more}` |
| `suggest` | `{query}` | `[string]` |
| `recent` | | `[Track]` |
| `played` | `{id}` | |
| `liked` | `{offset}` | `{tracks, more}` |
| `isLiked` | `{ids}` | `{id: bool}` |
| `like`, `likeEpisode` | `{id, on}` | `like` likes a song, and `likeEpisode` an episode: without it, episodes show no heart |
| `album` | `{id}` | `{album, tracks}` |
| `artist` | `{id}` | `{artist, tracks, albums}` |
| `saveAlbum`, `followArtist`, `savePlaylist`, `subscribeShow` | `{id, on}` | |
| `savedAlbums` | `{offset}` | `{albums, more}` |
| `playlists` | | `[Playlist]` |
| `playlist` | `{id}` | `Playlist` with `tracks` |
| `createPlaylist` | `{name}` | `Playlist` |
| `renamePlaylist` | `{id, name}` | |
| `deletePlaylist` | `{id}` | |
| `addToPlaylist`, `removeFromPlaylist` | `{id, trackIds}` | |
| `movePlaylistTrack` | `{id, from, to}` | |
| `shows` | | `[Show]` |
| `show` | `{id, offset}` | `{show, episodes, more}` |
| `stations` | | `[Station]` |
| `stationNext` | `{id}` | `[Track]` |
| `comments` | `{trackId, offset}` | `{comments, total, more}` |
| `likeComment` | `{trackId, commentId, on}` | |
| `postComment` | `{trackId, text}` | `Comment` |
| `lyrics` | `{id}` | `{lrc, translation}`, or `{lines}` with timed `words` |
| `stream` | `{id, quality}` | `{url, type, headers, size, gain, peak}`, where `type` is `mp3`, `flac`, `ogg`, `wav`, `m4a` (ALAC, FLAC or AAC-LC, whole or in fragments as a DASH stream serves it) or `aac` (AAC-LC in ADTS) |
| `remotePlay` | `{ids, start, position}` | |
| `remotePause`, `remoteResume` | | |
| `remoteSeek` | `{position}` | |
| `remoteVolume` | `{percent}` | |
| `remoteState` | | `{playing, trackId, position, duration, volume, device}` or `null` |
| `remoteDevices` | | `[{id, name, active}]` |
| `remoteUse` | `{deviceId}` | |

The shapes are in [`internal/music/music.go`](internal/music/music.go) by their JSON names. Times are in milliseconds. A `Browse` is `{title, subtitle, imageUrl, description, sections}`: with an `imageUrl`, such as a listener's avatar, it's headed round, as an artist's page is, and `description` goes under the title a line to each line. A section can hold `tracks`, `albums`, `artists`, `playlists`, `shows`, `stations`, `links` (`{title, subtitle, imageUrl, page}`), a `morePage`, and `empty`, what it says while it holds nothing. A source's first section of `tracks` leads its Home as a card of the day's songs, which opens a page of all of them, headed by the section's `pageTitle` if it has one, else its `title`, and its `subtitle` after the count of songs. A show's `publisherPage` is a `browse` page of who publishes it, which their name on the show's page opens.

An `imageUrl` can say where its size goes, with `{size}`, for an image server that makes pictures the size asked: `https://img.example.com/cover.jpg?param={size}y{size}` or `https://img.example.com/{size}/cover.jpg`. The app fills in how many pixels across it shows the picture at, 40 in a row and 600 in Now Playing, or 600 where nothing asks for a size. `%7Bsize%7D`, as JavaScript's `URL` writes it in a query, counts too. An app older than 0.1.20 fetches `{size}` as it is, so a plugin that gives it wants a `minAppVersion` of 0.1.20. Without it, give pictures at about 800 pixels if the service can, rather than as large as they were made: the app fetches and decodes the whole of each, a few megabytes for one thousands of pixels across, to show it in a row.

Of the `playlists`, those with `mine` are the listener's own, listed under My Playlists, and the rest are ones they saved. One that is `mine` and also `fixed`, such as the songs they liked, is kept by the source itself: the app doesn't offer to rename it, delete it, add songs to it, take them out or move them.

`navigation` is the plugin's sidebar, in its order. An entry opens a `browse` page of its own by `page`, or one of the app's by `builtin`: `home`, `liked`, `savedAlbums`, `stations` or `shows`, which can be given a `title`. Without `navigation`, the sidebar has an entry for each of the app's pages the plugin can do. Up to four stations show as a switch over one card, and more as a grid.

`minAppVersion` is the oldest version of MyGo Music the plugin works with. An older one won't install it, and says to update first. Versions before 0.1.8 don't read it.

### Setting controls

Settings can declare an optional `type` to use a native control. Existing settings with only `key`, `label`, `help` and `link` keep their text fields and save on Enter or when focus leaves the field.

```json
{
  "settings": [
    { "key": "password", "label": "Password", "type": "password" },
    { "key": "history", "label": "Save listening history", "type": "boolean" },
    {
      "key": "authMode", "label": "Sign-in method", "type": "select",
      "options": [
        { "value": "", "label": "Automatic (token)" },
        { "value": "token", "label": "Token" },
        { "value": "legacy", "label": "Legacy password" }
      ]
    }
  ]
}
```

- Missing `type`, `text`, unknown types and invalid select options use the existing text editor.
- `password` masks the input and its accessibility value. It requires **Save changes**; Enter or blur does not save partial credentials. Storage remains plaintext JSON.
- `boolean` uses a switch and saves the strings `"true"` or `"false"`. An unset value appears off without writing a default. Other existing string values retain a text editor.
- `select` displays option labels but saves only their string values. Blank values are allowed. Unlisted saved values stay visible and unchanged until a declared choice is selected. Empty labels, duplicate labels or duplicate values fall back to text.
- Switch/select changes save automatically; pending writes disable the control. Failed saves offer retry or restoration of the previous value. UI saves for each source run in initiation order.

`host.setting(key)` and `setSetting({key, value})` still use strings; opening settings does not migrate or write values. Older hosts ignore `type` and `options` and render all fields as text, so plugin help must still explain text entry.

### Translating a plugin

Put the translations in `locales/<language>.json` next to `plugin.json`: `locales/zh-CN.json` for Simplified Chinese, the language the app speaks besides English. Each one maps the English text to the translation:

```json
{ "Example": "示例", "API Key": "API 密钥", "From your account page": "在你的账号页面获取", "Charts": "排行榜" }
```

The app translates the plugin's `name`, its settings' `label` and `help`, select option labels and its `navigation` titles with them. Option values are never translated. For what the plugin says itself, such as a page's titles or an error's message, `host.t(text)` answers the translation, or `text` when there's none, and `host.language()` the listener's language, `en` or `zh-CN`. Both follow the listener as they change it, and the page they're on loads again.

With `"playback": "stream"`, the app plays what `stream` answers. With `"playback": "remote"`, the plugin makes the sound itself, or has another program do so, and the app calls the `remote` functions and polls `remoteState`.

Throw an error with a `code` to say what's wrong: `signIn` prompts the listener to sign in, `setup` sends them to the plugin's settings, `unavailable` means this can't be played, and `unsupported` means the plugin doesn't do this. Any other error shows its message.

### Publishing a plugin

Put it in a public GitHub repository, with `plugin.json` at the top. The app takes the latest release: a `.zip` asset named for the platform, such as `plugin-darwin-arm64.zip`, or one named for none, or else the source. Without a release it takes the latest commit of the default branch. A new tag or commit is a new version, which shows as an update in the settings. An update asking for more permissions asks the listener again.

Add the topic `mygo-music-plugin` to the repository to have it listed on the [plugins page](https://mygo-music.egoist.dev/plugins).

An install link opens the player and asks the listener to confirm the plugin. For Windows Plus, use `mygo-music-plus://install-plugin?repo=owner/repo`. Use the same path and query with `mygo-music://` for Android or the upstream desktop app, `mygo-music-plus-dev://` for the standalone Windows Plus Dev build, and `mygo-music-dev://` for the upstream development app.

### What a plugin can reach

A plugin has the JavaScript standard library, timers, `fetch` and its relatives, `URL`, `console` and `host`:

| | |
| --- | --- |
| `host.platform`, `host.arch` | such as `darwin`, `arm64` |
| `host.dir`, `host.dataDir` | the plugin's folder, and one for its data |
| `host.storage.get(key)`, `.set(key, value)`, `.remove(key)` | JSON values that outlive the app |
| `host.setting(key)` | what the listener filled in |
| `host.t(text)`, `host.language()` | the text in the listener's language, from the plugin's `locales`, and that language: `en` or `zh-CN` |
| `host.removeDataDir(name)` | removes a folder inside `host.dataDir` |
| `host.crypto.md5(text)` | its hash, in hex |
| `host.crypto.aesEcbEncrypt(key, text)` | AES-ECB with PKCS#7 padding, in hex, for services whose requests are made that way |
| `host.openURL(url)` | opens a web page in the listener's browser |
| `host.signInPrompt({qr, text})` | what the sign-in window shows while `signIn` goes on: a QR code to scan, and what to do |
| `host.waitForRedirect({port, path, timeout, page})` | the end of a browser sign-in: listens on `127.0.0.1` and answers the query it's redirected back with. Needs the network permission |
| `host.webSignIn({url, cookiesOf, until, userAgent})` | a sign-in on the service's own page, for one that has no other way in: the app shows `url` in a window of its own, apart from every other page and forgotten after, and once the cookies named in `until` are all there, closes it and answers `{cookie}`, the Cookie header a request to `cookiesOf` would send. `cookiesOf` has to be a host the network permission reaches. `userAgent` is what the page is told it's shown in |
| `host.spawn(command, args)` | starts a program, by a path from the plugin's folder or absolute, or a name on `PATH`. Needs the process permission to name it. Returns `{pid, write(text), closeStdin(), readLine(), wait(), kill(), errorOutput()}` |

Everything else takes a permission in `plugin.json`:

- `"network": ["api.example.com", "*.example.com"]` lists the hosts `fetch` can reach, or `["*"]` for any. Without it, `fetch` reaches nothing.
- `"requestHeaders"` are headers the app sends with what it fetches itself for the plugin, its pictures (`image`) and its songs' audio (`audio`), for a service that keeps those behind its session as well as its API:

  ```json
  "requestHeaders": [
    { "hosts": ["{setting:server}"], "kinds": ["image", "audio"], "headers": { "cookie": "music-token={storage:token}" } },
    { "hosts": ["img.example.com"], "kinds": ["image"], "headers": { "referer": "https://example.com/" } }
  ]
  ```

  `hosts` are hosts or `*.domain`, which the network permission has to reach, or `{setting:key}`, the host of the server a setting names, such as `https://nas.local:5666`, while the network permission reaches it. `*` isn't one. Without `kinds`, they go with both. In `headers`, `{setting:key}` is what the listener filled in and `{storage:key}` what the plugin keeps with `host.storage`, as each is when the request goes out: a token the plugin stores anew goes with the next one, and none is kept in a URL. A header whose value isn't there yet, as before signing in, is left out. The headers a `stream` answers come first. Neither goes on to another host a request is redirected to.
- `"process": ["bin/helper-*", "ffmpeg"]` lists the programs `host.spawn` can start, for what JavaScript can't do: a path from the plugin's folder or absolute, or a name looked up on `PATH`. A path only matches a path and a name only a name, and `*` matches within one folder, as in a glob. `["*"]`, or `true`, asks for any program, which the listener is warned about. The listener has to allow it in Settings first, as a program can do anything they can. Programs end when the plugin does, and once allowed, `fetch` can reach this machine too, to talk to them. Ship one binary per platform and pick it with `host.platform` and `host.arch`.

## Credits

- [folia-major](https://github.com/chthollyphile/folia-major): the lyric stage animations

## License

AGPL-3.0. See [LICENSE](LICENSE).
