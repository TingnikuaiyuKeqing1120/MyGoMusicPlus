// Android owns audio decoding and output. Only commands, source descriptions
// and state cross this bridge; the browser path uses MyGo's existing output.
(() => {
  const bridge = globalThis.AndroidBridge;
  if (!bridge?.hasNativePlayback?.()) return;
  let onState, onResolve;
  let serial = 0;
  let generation = 0;
  const preparing = new Map();

  function base64(bytes) {
    let text = '';
    for (let at = 0; at < bytes.length; at += 8192) {
      text += String.fromCharCode(...bytes.subarray(at, at + 8192));
    }
    return btoa(text);
  }

  async function nativeUri(url, type) {
    const address = new URL(url);
    if (address.origin === 'https://appassets.androidplatform.net' && address.pathname.startsWith('/assets/')) {
      return 'asset:///' + address.pathname.slice('/assets/'.length);
    }
    if (!url.startsWith('file:')) return url;
    const path = decodeURIComponent(new URL(url).pathname);
    const folders = globalThis.__mygoMusicFolders;
    if (folders?.contains(path)) {
      const file = await folders.entry(path);
      if (file.nativeUri) return file.nativeUri;
      throw new Error('This folder cannot be played by Android');
    }
    const file = globalThis.__mygoFs.files.get(path);
    if (!file) throw new Error('The downloaded file is missing');
    const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', file.data));
    const hash = [...digest].map(byte => byte.toString(16).padStart(2, '0')).join('');
    if (!preparing.has(hash)) {
      const job = (async () => {
        const result = JSON.parse(bridge.nativeAudioBegin(hash, file.data.length, type || 'audio'));
        if (result.error) throw new Error(result.error);
        if (result.uri) return result.uri;
        try {
          for (let at = 0; at < file.data.length; at += 192 * 1024) {
            const error = bridge.nativeAudioWrite(result.token, base64(file.data.subarray(at, at + 192 * 1024)));
            if (error) throw new Error(error);
            await new Promise(resolve => setTimeout(resolve, 0));
          }
          const complete = JSON.parse(bridge.nativeAudioCommit(result.token));
          if (complete.error) throw new Error(complete.error);
          return complete.uri;
        } catch (error) {
          bridge.nativeAudioAbort(result.token);
          throw error;
        }
      })();
      preparing.set(hash, job);
      job.finally(() => preparing.delete(hash)).catch(() => {});
    }
    return preparing.get(hash);
  }

  globalThis.__mygoNativeState = json => {
    const state = JSON.parse(json);
    if (state.generation === generation && state.serial >= serial) onState?.(json);
  };
  globalThis.__mygoNativeResolve = (gen, slot) => {
    if (gen === generation) onResolve?.(gen, slot);
  };
  globalThis.__mygoNativePlayback = {
    cache(action, limit) { return bridge.nativeCacheCommand(JSON.stringify({action,limit})); },
    connect(state, resolve) { onState = state; onResolve = resolve; },
    command(json) {
      const command = JSON.parse(json);
      generation = command.generation;
      command.serial = ++serial;
      bridge.nativePlaybackCommand(JSON.stringify(command));
    },
    async resolve(json) {
      const result = JSON.parse(json);
      if (result.generation !== generation) return;
      try {
        if (result.stream) result.stream.url = await nativeUri(result.stream.url, result.stream.type);
      } catch (error) {
        result.error = error.message;
        delete result.stream;
      }
      if (result.generation === generation) bridge.nativePlaybackResolved(JSON.stringify(result));
    },
  };
})();
