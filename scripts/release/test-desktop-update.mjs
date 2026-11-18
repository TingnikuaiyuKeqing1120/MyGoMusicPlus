import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { setTimeout } from "node:timers/promises";

const [directory, target] = process.argv.slice(2);
const root = mkdtempSync(join(tmpdir(), "mygo-plus-probe-"));
const dir = resolve(directory);
const manifest = join(dir, `update-${target}.json`);
const { url } = JSON.parse(readFileSync(manifest));
const key = readFileSync(new URL("./plus-update.pub", import.meta.url), "utf8").trim();
const player = { win32: "MyGo Music Plus.exe", linux: "mygo-music-plus", darwin: "MyGo Music Plus.app/Contents/MacOS/MyGo Music Plus" }[process.platform];
const exe = join(root, "install", player);
const hash = (path) => createHash("sha256").update(readFileSync(path)).digest("hex");
const expected = hash(join(dir, player));
try {
  mkdirSync(dirname(exe), { recursive: true });
  const flags = `-X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageUpdateFeed=https://example.invalid/ -X github.com/egoist/mygo.packageUpdateKey=${key}`;
  execFileSync("go", ["build", "-ldflags", flags, "-o", exe, "./scripts/release/probe"], { stdio: "inherit" });
  execFileSync(exe, [manifest, join(dir, basename(new URL(url).pathname))], {
    env: { ...process.env, PLUS_PROBE_ROOT: root, XDG_DATA_HOME: join(root, "data") },
    stdio: "inherit",
  });
  // macOS swaps the bundle after the old process exits.
  const deadline = Date.now() + 15000;
  let actual;
  do {
    try { actual = hash(exe); } catch (err) { if (err.code !== "ENOENT") throw err; }
    if (actual !== expected) await setTimeout(100);
  } while (actual !== expected && Date.now() < deadline);
  assert.equal(actual, expected, "updated executable must match the packaged player");
} finally {
  rmSync(root, { recursive: true, force: true, maxRetries: 4 });
}
