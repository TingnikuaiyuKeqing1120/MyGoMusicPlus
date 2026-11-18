// `mygo dev` and `mygo build` evaluate this with Bun or Node.js. The development
// app has its own link scheme, leaving mygo-music:// to the installed app.
export default ({ command }: { command: string }) => {
  // Local Plus packages have their own data folder and protocol registration.
  const plusDesktop = command === "build" && process.env.MYGO_PLUS_DESKTOP === "1";
  return {
    name: plusDesktop ? "MyGo Music Plus" : "MyGo Music",
    identifier: plusDesktop ? "com.mygomusic.plus" : "dev.egoist.music",
    slug: plusDesktop ? "mygo-music-plus" : "mygo-music",
    version: "0.2.2",
    out: "build",
    urlSchemes: [plusDesktop ? "mygo-music-plus" : command === "dev" ? "mygo-music-dev" : "mygo-music"],
    updates: plusDesktop ? undefined : {
      publicKey: "WzJsOGNIuf6mcEqo5ff8jub+NoQQOEk4JXreLPYjgyQ=",
      github: "egoist/mygo-music",
    },
    macos: plusDesktop ? undefined : {
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
