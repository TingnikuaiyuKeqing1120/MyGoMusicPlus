import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const script = fileURLToPath(new URL("../resources/windows/import-profile.ps1", import.meta.url));

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "mygo-import-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const source = join(root, "MyGo Music");
  const target = join(root, "MyGo Music Plus");
  const backups = join(root, "MyGo Music Plus Migration Backups");
  const write = (dir, file, value) => {
    const path = join(dir, file);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, JSON.stringify(value));
  };
  const read = (dir, file) => JSON.parse(readFileSync(join(dir, file), "utf8"));
  write(source, "settings.json", { language: "zh-CN" });
  write(target, "settings.json", { language: "en", updateMirror: true });
  const run = (apply, running = false) => {
    // The fixture has no player processes. Simulate one for the refusal check.
    const result = spawnSync("powershell.exe", ["-NoProfile", "-Command", [
      "function Get-Process { if ($env:PLAYER_OPEN -eq '1') { [pscustomobject]@{ProcessName='MyGo Music'} } }",
      apply ? "& $env:IMPORT_SCRIPT -Apply" : "& $env:IMPORT_SCRIPT",
      "if (!$?) { exit 1 }",
    ].join("\n")], {
      env: { ...process.env, APPDATA: root, LOCALAPPDATA: join(root, "local"), IMPORT_SCRIPT: script, PLAYER_OPEN: running ? "1" : "0" },
      encoding: "utf8",
    });
    if (result.error) throw result.error;
    return result;
  };
  return { root, source, target, backups, write, read, run };
}

// Migrate a plugin named differently on disk, keep Plus-only data, and preserve the complete pre-import profile.
test("upstream to Plus migration", { skip: process.platform !== "win32" }, (t) => {
  const { root, source, target, backups, write, read, run } = fixture(t);
  const dev = join(root, "MyGo Music Plus Dev");
  write(dev, "settings.json", { dev: true });
  write(source, "plugins/gd-music-plugin/plugin.json", { id: "gdstudio" });
  write(source, "plugin-data/gdstudio/storage.json", { playlists: ["source playlist"] });
  write(source, "local/library.json", { folders: ["music"] });
  write(target, "plugins/gdstudio/plugin.json", { id: "gdstudio" });
  write(target, "plugin-data/gdstudio/granted.json", true);
  write(target, "plugins/extra/plugin.json", { id: "extra" });
  write(target, "sync.json", { repo: "fixture" });
  assert.equal(run(false).status, 0);
  assert.equal(existsSync(backups), false);
  assert.notEqual(run(true, true).status, 0);
  assert.deepEqual(read(target, "settings.json"), { language: "en", updateMirror: true });
  const applied = run(true);
  assert.equal(applied.status, 0, applied.stderr);
  const [id] = readdirSync(backups).filter((name) => /^[0-9a-f]{32}$/.test(name));
  assert.deepEqual(read(join(backups, id, "before"), "settings.json"), { language: "en", updateMirror: true });
  assert.deepEqual(read(target, "settings.json"), { language: "zh-CN", updateMirror: true });
  assert.equal(existsSync(join(target, "plugins/gdstudio")), false);
  assert.equal(existsSync(join(target, "plugin-data/gdstudio/granted.json")), false);
  for (const file of ["plugins/gd-music-plugin/plugin.json", "plugin-data/gdstudio/storage.json", "local/library.json"]) {
    assert.deepEqual(read(target, file), read(source, file));
  }
  assert.deepEqual(read(target, "plugins/extra/plugin.json"), { id: "extra" });
  assert.deepEqual(read(target, "sync.json"), { repo: "fixture" });
  assert.deepEqual(read(dev, "settings.json"), { dev: true });
  assert.equal(run(true).status, 0);
  assert.deepEqual(read(source, "settings.json"), { language: "zh-CN" });
});

// Restore the old profile after a crash between renames, even if the next import then fails validation.
test("interrupted swap and invalid input preserve previous Plus data", { skip: process.platform !== "win32" }, (t) => {
  const { source, target, backups, read, run } = fixture(t);
  const id = "0123456789abcdef0123456789abcdef";
  const job = join(backups, id);
  mkdirSync(join(job, "prepared"), { recursive: true });
  renameSync(target, join(job, "before"));
  writeFileSync(join(backups, "pending.txt"), id);
  writeFileSync(join(source, "settings.json"), "{");
  assert.notEqual(run(true).status, 0);
  assert.deepEqual(read(target, "settings.json"), { language: "en", updateMirror: true });
  assert.equal(existsSync(join(backups, "pending.txt")), false);
});
