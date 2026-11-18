// Read-only mounts for music folders chosen by the listener. The Android
// shell keeps a SAF grant. Chromium browsers can keep a directory handle.
(() => {
  "use strict";

  const prefix = "/music-folders/";
  const unavailable = "Folder selection is unavailable in this browser";
  const cannotOpen = "The selected folder couldn't be opened";
  const failure = code => Object.assign(new Error(code), { code });
  const safeName = name => {
    const cleaned = name.replace(/[\\/\x00-\x1f]/g, "_");
    return !cleaned || cleaned === "." || cleaned === ".." ? "_" : cleaned;
  };

  let androidPicked;
  globalThis.__mygoMusicFolderPicked = (token, name, error) => {
    const done = androidPicked;
    androidPicked = undefined;
    done?.(token ? `${prefix}android/${token}/${safeName(name)}` : "", error);
  };

  const androidLists = new Map();
  async function nativeRequest(tree, operation) {
    const response = await fetch(`/native-music/${tree}/${operation}`, { cache: "no-store" });
    if (!response.ok) {
      const code = { 403: "EACCES", 404: "ENOENT" }[response.status] || "EIO";
      throw failure(code);
    }
    return response;
  }

  async function androidChildren(tree, id, refresh = false) {
    const key = `${tree}/${id}`;
    if (refresh || !androidLists.has(key)) {
      const request = (async () => {
        const response = await nativeRequest(tree, `list/${id}`);
        const entries = new Map();
        for (const child of await response.json()) {
          const name = safeName(child.name);
          let unique = name;
          // Some providers allow two documents with the same display name.
          if (entries.has(unique)) {
            const dot = name.lastIndexOf(".");
            const stem = dot > 0 ? name.slice(0, dot) : name;
            const ext = dot > 0 ? name.slice(dot) : "";
            for (let i = 2; entries.has(unique); i++) unique = `${stem} (${i})${ext}`;
          }
          entries.set(unique, child);
        }
        return entries;
      })();
      androidLists.set(key, request);
      request.catch(() => {
        if (androidLists.get(key) === request) androidLists.delete(key);
      });
    }
    return androidLists.get(key);
  }

  async function androidEntry(tree, names) {
    let entry = { dir: true, id: "-", size: 0, mtime: 0 };
    for (const name of names) {
      if (!entry.dir) throw failure("ENOTDIR");
      entry = (await androidChildren(tree, entry.id)).get(name);
      if (!entry) throw failure("ENOENT");
    }
    if (entry.dir) {
      return { ...entry, list: async () => [...(await androidChildren(tree, entry.id, true)).keys()] };
    }
    let block = new Uint8Array();
    let blockStart = -1;
    return { ...entry, async read(at, length) {
      if (at >= entry.size || !length) return new Uint8Array();
      if (at < blockStart || at + length > blockStart + block.length) {
        const count = Math.min(256 * 1024, Math.max(64 * 1024, length), entry.size - at);
        const response = await nativeRequest(tree, `read/${entry.id}/${at}/${count}`);
        block = new Uint8Array(await response.arrayBuffer());
        blockStart = at;
      }
      return block.subarray(at - blockStart, at - blockStart + length);
    } };
  }

  let database;
  function openDatabase() {
    database ??= new Promise((resolve, reject) => {
      const open = indexedDB.open("mygo-music-folders", 1);
      open.onupgradeneeded = () => open.result.createObjectStore("folders", { keyPath: "id" });
      open.onsuccess = () => resolve(open.result);
      open.onerror = () => reject(open.error);
    });
    return database;
  }

  function idbResult(request) {
    return new Promise((resolve, reject) => {
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
  }

  const browserRoots = new Map();
  async function browserEntry(id, names) {
    if (!browserRoots.has(id)) {
      const db = await openDatabase();
      const item = await idbResult(db.transaction("folders").objectStore("folders").get(id));
      if (!item) throw failure("ENOENT");
      browserRoots.set(id, item.handle);
    }
    let handle = browserRoots.get(id);
    for (const name of names) {
      if (handle.kind !== "directory") throw failure("ENOTDIR");
      try {
        handle = await handle.getDirectoryHandle(name);
      } catch (e) {
        if (e.name !== "TypeMismatchError") throw e;
        handle = await handle.getFileHandle(name);
      }
    }
    if (handle.kind === "directory") {
      return { dir: true, size: 0, mtime: 0, async list() {
        const names = [];
        for await (const name of handle.keys()) names.push(name);
        return names;
      } };
    }
    const file = await handle.getFile();
    return { dir: false, size: file.size, mtime: file.lastModified, async read(at, length) {
      return new Uint8Array(await file.slice(at, at + length).arrayBuffer());
    } };
  }

  async function pickBrowserFolder() {
    const handle = await globalThis.showDirectoryPicker({ mode: "read" });
    const db = await openDatabase();
    const items = await idbResult(db.transaction("folders").objectStore("folders").getAll());
    let id;
    for (const item of items) {
      if (await handle.isSameEntry(item.handle)) {
        id = item.id;
        break;
      }
    }
    id ??= crypto.randomUUID();
    await new Promise((resolve, reject) => {
      const tx = db.transaction("folders", "readwrite");
      tx.oncomplete = resolve;
      tx.onabort = () => reject(tx.error);
      tx.objectStore("folders").put({ id, handle });
    });
    browserRoots.set(id, handle);
    return `${prefix}browser/${id}/${safeName(handle.name)}`;
  }

  globalThis.__mygoMusicFolders = {
    contains: path => path.startsWith(prefix),
    async entry(path) {
      const [kind, id, rootName, ...names] = path.slice(prefix.length).split("/");
      if (!id || !rootName) throw failure("ENOENT");
      if (kind === "android") return androidEntry(id, names);
      if (kind === "browser") return browserEntry(id, names);
      throw failure("ENOENT");
    },
  };

  globalThis.__mygoPickMusicFolder = async done => {
    const bridge = globalThis.AndroidBridge;
    try {
      if (bridge?.pickMusicFolder) {
        androidPicked = done;
        bridge.pickMusicFolder();
        return;
      }
      if (!globalThis.showDirectoryPicker) {
        done("", unavailable);
        return;
      }
      done(await pickBrowserFolder(), "");
    } catch (e) {
      androidPicked = undefined;
      done("", e.name === "AbortError" ? "" : cannotOpen);
    }
  };
})();
