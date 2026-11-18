// `mygo dev` and `mygo build` evaluate this with Bun or Node.js. The development
// app has its own link scheme, leaving mygo-music:// to the installed app.
export default ({ command }: { command: string }) => {
  // Local Plus packages have their own data folder and protocol registration.
  const profile = command === "build" ? process.env.MYGO_PLUS_DESKTOP : undefined;
  const plusDev = profile === "dev";
  const plusDesktop = profile === "1" || plusDev;
  const name = plusDesktop ? "MyGo Music Plus" : "MyGo Music";
  const slug = plusDesktop ? "mygo-music-plus" : "mygo-music";
  return {
    name: plusDev ? name + " Dev" : name,
    identifier: plusDev ? "com.mygomusic.plus.dev" : plusDesktop ? "com.mygomusic.plus" : "dev.egoist.music",
    slug: plusDev ? slug + "-dev" : slug,
    icon: plusDesktop ? "resources/plus-rain-cat.png" : undefined,
    version: "0.2.3",
    out: "build",
    urlSchemes: [plusDev || command === "dev" ? slug + "-dev" : slug],
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
