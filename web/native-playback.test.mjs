import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { webcrypto } from 'node:crypto';
import test from 'node:test';
import vm from 'node:vm';

function fixture() {
  const commands = [], answers = [], blocks = [];
  const files = new Map();
  const bridge = {
    hasNativePlayback: () => true,
    nativePlaybackCommand: json => commands.push(JSON.parse(json)),
    nativePlaybackResolved: json => answers.push(JSON.parse(json)),
    nativeAudioBegin: () => JSON.stringify({token:'import'}),
    nativeAudioWrite(token, data) { blocks.push(Buffer.from(data,'base64')); return ''; },
    nativeAudioCommit: () => JSON.stringify({uri:'file:///native/copy.wav'}),
    nativeAudioAbort: () => {},
    nativeCacheCommand: () => '{"bytes":0,"keys":[]}',
  };
  const context = vm.createContext({AndroidBridge:bridge, URL, crypto:webcrypto, setTimeout,
    btoa: text => Buffer.from(text,'binary').toString('base64'), __mygoFs:{files}});
  vm.runInContext(readFileSync(new URL('./native-playback.js',import.meta.url),'utf8'),context);
  const api = context.__mygoNativePlayback;
  api.command(JSON.stringify({action:'queue',generation:1,items:[]}));
  return {context,bridge,api,commands,answers,blocks,files};
}

// Late events and source answers from a replaced queue cannot change playback.
test('native bridge rejects stale state and source answers', async () => {
  const f = fixture();
  const states = [];
  f.api.connect(json => states.push(JSON.parse(json)),() => {});
  f.context.__mygoNativeState(JSON.stringify({generation:1,serial:0}));
  f.context.__mygoNativeState(JSON.stringify({generation:1,serial:1}));
  assert.equal(states.length,1);
  f.api.command(JSON.stringify({action:'queue',generation:2,items:[]}));
  await f.api.resolve(JSON.stringify({generation:1,slot:0,stream:{url:'https://example.test/song.mp3'}}));
  assert.equal(f.answers.length,0);
});

// Native online playback receives the URL and headers without fetching audio in JS.
test('native bridge passes stream URLs and authorization headers', async () => {
  const f = fixture();
  const stream = {url:'https://example.test/song.mp3',headers:{Authorization:'fixture-value'},type:'mp3'};
  await f.api.resolve(JSON.stringify({generation:1,slot:0,stream}));
  assert.deepEqual(f.answers[0].stream,stream);
  assert.equal(f.blocks.length,0);
});

// OPFS imports retain every byte and hand a native file URI to the player.
test('native bridge copies an OPFS file in bounded blocks', async () => {
  const f = fixture();
  const data = new Uint8Array(500_001).map((_,index)=>index%251);
  f.files.set('/songs/test.wav',{data});
  await f.api.resolve(JSON.stringify({generation:1,slot:0,stream:{url:'file:///songs/test.wav',type:'wav'}}));
  assert.equal(f.blocks.length,3);
  assert.deepEqual(Buffer.concat(f.blocks),Buffer.from(data));
  assert.equal(f.answers[0].stream.url,'file:///native/copy.wav');
  assert.equal(f.files.get('/songs/test.wav').data,data);
});

// Failed imports are aborted and become visible playback errors.
test('native bridge aborts failed file imports', async () => {
  const f = fixture();
  let aborted = 0;
  f.files.set('/song.wav',{data:new Uint8Array([1,2,3])});
  f.bridge.nativeAudioWrite = () => 'disk full';
  f.bridge.nativeAudioAbort = () => { aborted++; };
  await f.api.resolve(JSON.stringify({generation:1,slot:0,stream:{url:'file:///song.wav'}}));
  assert.equal(aborted,1);
  assert.equal(f.answers[0].error,'disk full');
  assert.equal(f.answers[0].stream,undefined);
});

// Authorized document URIs let Android read music without copying its library.
test('native bridge uses an authorized document URI', async () => {
  const f = fixture();
  f.context.__mygoMusicFolders = {contains:()=>true,entry:async()=>({nativeUri:'content://fixture/document/song'})};
  await f.api.resolve(JSON.stringify({generation:1,slot:0,stream:{url:'file:///music-folders/android/song'}}));
  assert.equal(f.answers[0].stream.url,'content://fixture/document/song');
  assert.equal(f.blocks.length,0);
});
