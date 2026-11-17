// Persistent filesystem for the MyGo web build.
//
// Go's os.* in js/wasm goes through globalThis.fs; wasm_exec.js only installs
// its own stub when none is present, so this file defines one BEFORE
// wasm_exec.js loads. It keeps a read-write in-memory tree (so the synchronous
// writeSync/readSync work on the main thread) and writes it back to OPFS
// asynchronously, hydrating from OPFS before the app boots. MyGo's own wrapper
// still answers for /mygo-drop paths on top of this.
//
// Without OPFS (very old WebViews) the tree stays in memory for the session.
(function () {
  "use strict";

  const O_RDONLY = 0, O_WRONLY = 1, O_RDWR = 2, O_CREAT = 64, O_EXCL = 128,
    O_TRUNC = 512, O_APPEND = 1024, O_DIRECTORY = 65536;
  const WRITES = O_WRONLY | O_RDWR | O_CREAT | O_TRUNC | O_APPEND;

  const files = new Map(); // path -> {data:Uint8Array, mtime:number}
  const dirs = new Set(["/"]);
  const fds = new Map(); // fd -> {path, at, flags}
  let nextFd = 100;
  const dirty = new Set(); // paths to write back
  const removed = new Set(); // paths to delete from OPFS

  const clean = (p) => {
    p = String(p).replace(/\\/g, "/");
    const parts = [];
    for (const seg of p.split("/")) {
      if (seg === "" || seg === ".") continue;
      if (seg === "..") parts.pop();
      else parts.push(seg);
    }
    return "/" + parts.join("/");
  };
  const parent = (p) => clean(p.slice(0, p.lastIndexOf("/")) || "/");
  const base = (p) => p.slice(p.lastIndexOf("/") + 1);
  const err = (code) => { const e = new Error(code); e.code = code; return e; };
  const isDir = (p) => dirs.has(p);
  const isFile = (p) => files.has(p);

  function ensureDirs(p) {
    let cur = "";
    for (const seg of p.split("/").filter(Boolean)) {
      cur += "/" + seg;
      dirs.add(cur);
    }
  }
  function statOf(p) {
    const f = files.get(p);
    const dir = isDir(p);
    const ms = f ? f.mtime : Date.now();
    const size = f ? f.data.length : 0;
    const mode = dir ? 0o40755 : 0o100644;
    const st = {
      dev: 0, ino: 0, mode, nlink: 1, uid: 0, gid: 0, rdev: 0,
      size, blksize: 4096, blocks: Math.ceil(size / 512),
      atimeMs: ms, mtimeMs: ms, ctimeMs: ms, birthtimeMs: ms,
      atime: new Date(ms), mtime: new Date(ms), ctime: new Date(ms), birthtime: new Date(ms),
      isDirectory: () => dir, isFile: () => !dir, isSymbolicLink: () => false,
      isBlockDevice: () => false, isCharacterDevice: () => false,
      isFIFO: () => false, isSocket: () => false,
    };
    return st;
  }
  function touch(p) { dirty.add(p); removed.delete(p); scheduleFlush(); }

  // ---- OPFS write-back ----
  let opfsRoot = null;
  let flushTimer = null;
  async function opfs() {
    if (opfsRoot) return opfsRoot;
    if (!navigator.storage || !navigator.storage.getDirectory) return null;
    try { opfsRoot = await navigator.storage.getDirectory(); } catch (e) { opfsRoot = null; }
    return opfsRoot;
  }
  async function dirHandle(root, p, create) {
    let h = root;
    for (const seg of p.split("/").filter(Boolean)) {
      h = await h.getDirectoryHandle(seg, { create });
    }
    return h;
  }
  async function flush() {
    const root = await opfs();
    if (!root) return;
    const toWrite = [...dirty]; dirty.clear();
    const toRemove = [...removed]; removed.clear();
    for (const p of toWrite) {
      const f = files.get(p);
      if (!f) continue;
      try {
        const dh = await dirHandle(root, parent(p), true);
        const fh = await dh.getFileHandle(base(p), { create: true });
        const w = await fh.createWritable();
        await w.write(f.data);
        await w.close();
      } catch (e) { /* keep in memory; retry next flush */ dirty.add(p); }
    }
    for (const p of toRemove) {
      try {
        const dh = await dirHandle(root, parent(p), false);
        await dh.removeEntry(base(p), { recursive: true });
      } catch (e) { /* already gone */ }
    }
  }
  function scheduleFlush() {
    if (flushTimer) return;
    flushTimer = setTimeout(() => { flushTimer = null; flush(); }, 250);
  }
  addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") flush(); });
  addEventListener("pagehide", () => flush());

  async function hydrate() {
    const root = await opfs();
    if (!root) return;
    async function walk(handle, prefix) {
      for await (const [name, h] of handle.entries()) {
        const p = prefix + "/" + name;
        if (h.kind === "directory") { dirs.add(p); await walk(h, p); }
        else {
          const file = await h.getFile();
          const buf = new Uint8Array(await file.arrayBuffer());
          files.set(p, { data: buf, mtime: file.lastModified || Date.now() });
        }
      }
    }
    try { await walk(root, ""); } catch (e) { /* start empty */ }
  }

  // ---- the fs object Go's syscall reads ----
  const fs = {
    constants: { O_RDONLY, O_WRONLY, O_RDWR, O_CREAT, O_EXCL, O_TRUNC, O_APPEND, O_DIRECTORY },
    writeSync(fd, buf) {
      if (fd === 1 || fd === 2) {
        const s = new TextDecoder().decode(buf);
        (fd === 2 ? console.error : console.log)(s);
        return buf.length;
      }
      const o = fds.get(fd);
      if (!o) throw err("EBADF");
      let f = files.get(o.path);
      if (!f) { f = { data: new Uint8Array(0), mtime: Date.now() }; files.set(o.path, f); }
      const at = o.flags & O_APPEND ? f.data.length : o.at;
      const end = at + buf.length;
      if (end > f.data.length) {
        const nd = new Uint8Array(end); nd.set(f.data); f.data = nd;
      }
      f.data.set(buf, at);
      f.mtime = Date.now();
      o.at = end;
      touch(o.path);
      return buf.length;
    },
    readSync(fd, buf, offset, length, position) {
      const o = fds.get(fd);
      if (!o) throw err("EBADF");
      const f = files.get(o.path);
      if (!f) return 0;
      const at = position == null ? o.at : position;
      const n = Math.min(length, Math.max(f.data.length - at, 0));
      if (n > 0) buf.set(f.data.subarray(at, at + n), offset);
      if (position == null) o.at = at + n;
      return n;
    },
    open(path, flags, mode, cb) {
      const p = clean(path);
      const write = (flags & WRITES) !== 0;
      if (isDir(p)) {
        if (write) return cb(err("EISDIR"));
        // Reading a directory opens it too: Go asks for an fd, then fstat
        // and readdir of the path. Without this every ReadDir failed ENOENT.
        const fd = nextFd++;
        fds.set(fd, { path: p, at: 0, flags });
        return cb(null, fd);
      }
      let f = files.get(p);
      if (!f) {
        if (!(flags & O_CREAT)) return cb(err("ENOENT"));
        if (!isDir(parent(p))) return cb(err("ENOENT"));
        f = { data: new Uint8Array(0), mtime: Date.now() };
        files.set(p, f);
        touch(p);
      } else if (flags & O_EXCL) return cb(err("EEXIST"));
      if (flags & O_TRUNC) { f.data = new Uint8Array(0); f.mtime = Date.now(); touch(p); }
      const fd = nextFd++;
      fds.set(fd, { path: p, at: 0, flags });
      cb(null, fd);
    },
    close(fd, cb) { fds.delete(fd); cb(null); },
    read(fd, buf, offset, length, position, cb) {
      const o = fds.get(fd);
      if (!o) return cb(err("EBADF"));
      const f = files.get(o.path);
      if (!f) return cb(err("EISDIR"));
      const at = position == null ? o.at : position;
      const n = Math.min(length, Math.max(f.data.length - at, 0));
      if (n > 0) buf.set(f.data.subarray(at, at + n), offset);
      if (position == null) o.at = at + n;
      cb(null, n);
    },
    write(fd, buf, offset, length, position, cb) {
      const o = fds.get(fd);
      if (!o) return cb(err("EBADF"));
      const slice = buf.subarray(offset, offset + length);
      const save = o.at;
      if (position != null) o.at = position;
      const n = fs.writeSync(fd, slice);
      if (position != null) o.at = save;
      cb(null, n);
    },
    fsync(fd, cb) { flush(); cb(null); },
    fstat(fd, cb) {
      const o = fds.get(fd);
      if (!o) return cb(err("EBADF"));
      cb(null, statOf(o.path));
    },
    stat(path, cb) { const p = clean(path); (isFile(p) || isDir(p)) ? cb(null, statOf(p)) : cb(err("ENOENT")); },
    lstat(path, cb) { fs.stat(path, cb); },
    readdir(path, cb) {
      const p = clean(path);
      if (!isDir(p)) return cb(isFile(p) ? err("ENOTDIR") : err("ENOENT"));
      const names = new Set();
      for (const f of files.keys()) if (parent(f) === p) names.add(base(f));
      for (const d of dirs) if (parent(d) === p && d !== p) names.add(base(d));
      cb(null, [...names].sort());
    },
    mkdir(path, perm, cb) {
      const p = clean(path);
      if (isFile(p) || isDir(p)) return cb(err("EEXIST"));
      if (!isDir(parent(p))) return cb(err("ENOENT"));
      dirs.add(p);
      cb(null);
    },
    rmdir(path, cb) {
      const p = clean(path);
      if (!isDir(p)) return cb(err("ENOTDIR"));
      for (const f of files.keys()) if (parent(f) === p) return cb(err("ENOTEMPTY"));
      for (const d of dirs) if (d !== p && parent(d) === p) return cb(err("ENOTEMPTY"));
      dirs.delete(p);
      removed.add(p); scheduleFlush();
      cb(null);
    },
    unlink(path, cb) {
      const p = clean(path);
      if (!isFile(p)) return cb(err("ENOENT"));
      files.delete(p);
      dirty.delete(p); removed.add(p); scheduleFlush();
      cb(null);
    },
    rename(from, to, cb) {
      const a = clean(from), b = clean(to);
      const f = files.get(a);
      if (f) { files.delete(a); files.set(b, f); dirty.delete(a); removed.add(a); touch(b); return cb(null); }
      if (isDir(a)) {
        const move = new Map();
        for (const [p, v] of files) if (p === a || p.startsWith(a + "/")) move.set(p, v);
        for (const p of move.keys()) { files.delete(p); files.set(b + p.slice(a.length), move.get(p)); removed.add(p); }
        for (const d of [...dirs]) if (d === a || d.startsWith(a + "/")) { dirs.delete(d); dirs.add(b + d.slice(a.length)); }
        touch(b); scheduleFlush();
        return cb(null);
      }
      cb(err("ENOENT"));
    },
    truncate(path, length, cb) {
      const p = clean(path); const f = files.get(p);
      if (!f) return cb(err("ENOENT"));
      const nd = new Uint8Array(length); nd.set(f.data.subarray(0, length)); f.data = nd; f.mtime = Date.now();
      touch(p); cb(null);
    },
    ftruncate(fd, length, cb) { const o = fds.get(fd); if (!o) return cb(err("EBADF")); fs.truncate(o.path, length, cb); },
    chmod(path, mode, cb) { cb(null); },
    fchmod(fd, mode, cb) { cb(null); },
    chown(path, uid, gid, cb) { cb(null); },
    fchown(fd, uid, gid, cb) { cb(null); },
    lchown(path, uid, gid, cb) { cb(null); },
    utimes(path, atime, mtime, cb) { const f = files.get(clean(path)); if (f) f.mtime = +mtime * 1000; cb(null); },
    futimes(fd, atime, mtime, cb) { const o = fds.get(fd); if (o) { const f = files.get(o.path); if (f) f.mtime = +mtime * 1000; } cb(null); },
    link(a, b, cb) { cb(err("ENOSYS")); },
    symlink(a, b, cb) { cb(err("ENOSYS")); },
    readlink(p, cb) { cb(err("ENOSYS")); },
  };

  globalThis.fs = fs;
  globalThis.__mygoFs = { files, dirs, flush, hydrate, statOf };
  globalThis.__mygoFsReady = hydrate();
})();