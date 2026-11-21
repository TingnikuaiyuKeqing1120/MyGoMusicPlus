import { readFileSync } from "node:fs";

// `mygo dev` and `mygo build` evaluate this with Bun or Node.js. The development
// app has its own link scheme, leaving mygo-music:// to the installed app.
export default ({ command }: { command: string }) => {
  // Local Plus packages have their own data folder and protocol registration.
  const profile = command === "build" ? process.env.MYGO_PLUS_DESKTOP : undefined;
  const plusDev = profile === "dev";
  const standalone = profile === "standalone";
  const plusDesktop = profile === "1" || plusDev || standalone;
  const name = plusDesktop ? "MyGo Music Plus" : "MyGo Music";
  const slug = plusDesktop ? "mygo-music-plus" : "mygo-music";
  return {
    name: plusDev ? name + " Dev" : name,
    identifier: plusDev ? "com.mygomusic.plus.dev" : plusDesktop ? "com.mygomusic.plus" : "dev.egoist.music",
    slug: plusDev ? slug + "-dev" : slug,
    icon: plusDesktop ? "resources/plus-rain-cat.png" : undefined,
    version: "0.5.1",
    out: "build",
    urlSchemes: [plusDev || command === "dev" ? slug + "-dev" : slug],
    // Installed updates replace a directory of packaged resources. Standalone
    // users can put the EXE anywhere and replace that one file to upgrade.
    updates: plusDev || standalone ? undefined : plusDesktop ? {
      github: "TingnikuaiyuKeqing1120/MyGoMusicPlus",
      publicKey: readFileSync(new URL("./scripts/release/plus-update.pub", import.meta.url), "utf8").trim(),
      deltas: 0,
    } : {
      publicKey: "WzJsOGNIuf6mcEqo5ff8jub+NoQQOEk4JXreLPYjgyQ=",
      github: "egoist/mygo-music",
    },
    macos: plusDesktop ? {
      minimumSystemVersion: "13.0",
      signingIdentity: process.env.MYGO_PLUS_MAC_SIGN_IDENTITY,
      notarize: process.env.MYGO_PLUS_NOTARY_PROFILE ? { keychainProfile: process.env.MYGO_PLUS_NOTARY_PROFILE } : undefined,
    } : {
      icon: "icon/AppIcon.icon",
      minimumSystemVersion: "13.0",
      signingIdentity: "Developer ID Application: xinyu zeng (GJE9R5VE87)",
      notarize: { keychainProfile: "notary" },
    },
    linux: {
      comment: "A music player for your files and more",
      categories: ["AudioVideo", "Audio", "Player"],
      webview: false,
    },
  };
};
