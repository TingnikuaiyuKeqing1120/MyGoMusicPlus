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

const html = await readFile(join(src, "index.html"), "utf8");
const wasm = (html.match(/fetch\("([^"]+\.wasm)"\)/) || [])[1];
if (!wasm) throw new Error("no wasm filename in build/web/index.html");

const patched = html
  .replace('<script src="wasm_exec.js"></script>',
    '<script src="fs-bridge.js"></script>\n    <script src="media-bridge.js"></script>\n    <script src="wasm_exec.js"></script>')
  .replace(/const go = new Go\(\)\s*\n\s*WebAssembly\.instantiateStreaming\(fetch\("[^"]+\.wasm"\), go\.importObject\)\s*\n\s*\.then\(\(r\) => go\.run\(r\.instance\)\)/,
    `(async () => {
        await globalThis.__mygoFsReady;
        const go = new Go();
        const resp = await fetch("${wasm}");
        const bytes = await resp.arrayBuffer();
        const r = await WebAssembly.instantiate(bytes, go.importObject);
        await go.run(r.instance);
      })()`);

if (patched === html) throw new Error("index.html boot block did not match; update web-shell.mjs");
await writeFile(join(out, "index.html"), patched);
console.log("web-shell assembled at build/web-shell (wasm " + wasm + ")");