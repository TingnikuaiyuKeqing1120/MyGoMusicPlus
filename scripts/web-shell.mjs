// Assemble a persistent web shell from the mygo web build: copies build/web
// into build/web-shell, adds web/fs-bridge.js, and rewrites index.html so the
// bridge installs globalThis.fs before wasm_exec.js and the app boots only
// after the filesystem has hydrated from OPFS.
import { readFile, writeFile, mkdir, cp, readdir, rm } from "node:fs/promises";
import { join } from "node:path";

const root = new URL("..", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
const src = join(root, "build", "web");
const out = join(root, "build", "web-shell");

await rm(out, { recursive: true, force: true });
await mkdir(out, { recursive: true });
for (const f of await readdir(src)) {
  await cp(join(src, f), join(out, f), { recursive: true });
}
await cp(join(root, "web", "fs-bridge.js"), join(out, "fs-bridge.js"));
await cp(join(root, "web", "media-bridge.js"), join(out, "media-bridge.js"));
await cp(join(root, "web", "music-folders.js"), join(out, "music-folders.js"));
await cp(join(root, "web", "touch-scroll.js"), join(out, "touch-scroll.js"));
await cp(join(root, "web", "native-playback.js"), join(out, "native-playback.js"));

const html = await readFile(join(src, "index.html"), "utf8");
const wasm = (html.match(/fetch\("([^"]+\.wasm)"\)/) || [])[1];
if (!wasm) throw new Error("no wasm filename in build/web/index.html");
const wasmScript = '<script src="wasm_exec.js"></script>';
const boot = /const go = new Go\(\)\s*\n\s*WebAssembly\.instantiateStreaming\(fetch\("[^"]+\.wasm"\), go\.importObject\)\s*\n\s*\.then\(\(r\) => go\.run\(r\.instance\)\)/;
if (!html.includes(wasmScript) || !boot.test(html)) {
  throw new Error("index.html boot block did not match; update web-shell.mjs");
}

const patched = html
  .replace("</head>", '<meta name="color-scheme" content="light dark">\n</head>')
  .replace(wasmScript,
    '<script src="music-folders.js"></script>\n    <script src="fs-bridge.js"></script>\n    <script src="media-bridge.js"></script>\n    <script src="native-playback.js"></script>\n    <script src="touch-scroll.js"></script>\n    <script src="wasm_exec.js"></script>')
  .replace(boot,
    `(async () => {
        await globalThis.__mygoFsReady;
        const shell = globalThis.AndroidBridge;
        if (shell?.systemDark) {
          document.documentElement.setAttribute("data-mygo-theme", shell.systemDark() ? "dark" : "light");
        }
        const go = new Go();
        const resp = await fetch("${wasm}");
        const bytes = await resp.arrayBuffer();
        const r = await WebAssembly.instantiate(bytes, go.importObject);
        await go.run(r.instance);
      })()`);

await writeFile(join(out, "index.html"), patched);
console.log("web-shell assembled at build/web-shell (wasm " + wasm + ")");
