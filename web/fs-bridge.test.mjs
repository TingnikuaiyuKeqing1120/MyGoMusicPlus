import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";

const source = readFileSync(new URL("./fs-bridge.js", import.meta.url), "utf8");

function file() {
  let data = new Uint8Array();
  return {
    kind: "file",
    async getFile() { return { async arrayBuffer() { return data.buffer; } }; },
    async createWritable() {
      let pending;
      return { async write(bytes) { pending = bytes.slice(); }, async close() { data = pending; } };
    },
  };
}

function directory() {
  const entries = new Map();
  function entry(name, create, kind, make) {
    if (!entries.has(name) && create) entries.set(name, make());
    const item = entries.get(name);
    if (!item) throw new DOMException(name, "NotFoundError");
    if (item.kind !== kind) throw new DOMException(name, "TypeMismatchError");
    return item;
  }
  return {
    kind: "directory",
    entries() { return entries.entries(); },
    async getDirectoryHandle(name, { create } = {}) { return entry(name, create, "directory", directory); },
    async getFileHandle(name, { create } = {}) { return entry(name, create, "file", file); },
    async removeEntry(name) {
      if (!entries.delete(name)) throw new DOMException(name, "NotFoundError");
    },
  };
}

async function boot(root) {
  const page = {
    navigator: { storage: { async getDirectory() { return root; } } },
    addEventListener() {},
    setTimeout() { return 1; }, // A page may close as soon as fsync finishes.
    clearTimeout() {},
  };
  runInNewContext(source, page);
  await page.__mygoFsReady;
  return async (method, ...args) => new Promise((resolve, reject) => {
    page.fs[method](...args, (error, value) => error ? reject(error) : resolve(value));
  });
}

async function write(call, path, contents) {
  const data = new TextEncoder().encode(contents);
  const fd = await call("open", path, 65, 0o644);
  await call("write", fd, data, 0, data.length, null);
  await call("close", fd);
}

async function contents(call, path) {
  const fd = await call("open", path, 0, 0);
  const data = new Uint8Array(32);
  const count = await call("read", fd, data, 0, data.length, null);
  await call("close", fd);
  return new TextDecoder().decode(data.slice(0, count));
}

async function syncDir(call, path) {
  const fd = await call("open", path, 0, 0);
  await call("fsync", fd);
  await call("close", fd);
}

// A completed install can be reopened immediately with all staged files in their new directory.
test("an installed plugin survives a fresh boot after moving its staging tree", async () => {
  const root = directory();
  let call = await boot(root);
  await call("mkdir", "/plugins", 0o755);
  await call("mkdir", "/plugins/.stage", 0o755);
  await call("mkdir", "/plugins/.stage/locales", 0o755);
  await write(call, "/plugins/.stage/plugin.json", "manifest");
  await write(call, "/plugins/.stage/locales/zh-CN.json", "translation");
  await call("rename", "/plugins/.stage", "/plugins/gdstudio");
  await syncDir(call, "/plugins/gdstudio");

  call = await boot(root);
  assert.equal(await contents(call, "/plugins/gdstudio/plugin.json"), "manifest");
  assert.equal(await contents(call, "/plugins/gdstudio/locales/zh-CN.json"), "translation");
  await assert.rejects(call("stat", "/plugins/.stage"), { code: "ENOENT" });

  await call("mkdir", "/plugins/.stage", 0o755);
  await write(call, "/plugins/.stage/plugin.json", "new manifest");
  await write(call, "/plugins/.stage/locales", "changed shape");
  await call("rename", "/plugins/gdstudio", "/plugins/.old");
  await call("rename", "/plugins/.stage", "/plugins/gdstudio");
  await syncDir(call, "/plugins/gdstudio");

  call = await boot(root);
  assert.equal(await contents(call, "/plugins/gdstudio/plugin.json"), "new manifest");
  assert.equal(await contents(call, "/plugins/gdstudio/locales"), "changed shape");
});
