import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

function fixture() {
  const listeners = new Map();
  const events = [];
  const canvas = { tagName: 'CANVAS', dispatchEvent(event) {
    events.push(event);
    if (event.type === 'pointercancel') emit(event.type, event);
  } };
  class Input {
    constructor(type, values) { this.type = type; Object.assign(this, values); }
    preventDefault() { this.prevented = true; }
    stopImmediatePropagation() { this.stopped = true; }
  }
  const context = vm.createContext({
    PointerEvent: Input, WheelEvent: Input,
    addEventListener(type, fn) {
      const handlers = listeners.get(type) || [];
      handlers.push(fn);
      listeners.set(type, handlers);
    },
  });
  function emit(type, values = {}) {
    const event = new Input(type, { pointerId: 1, pointerType: 'touch', isPrimary: true, target: canvas, clientX: 100, clientY: 200, ...values });
    for (const fn of listeners.get(type) || []) fn(event);
    return event;
  }
  vm.runInContext(readFileSync(new URL('./touch-scroll.js', import.meta.url), 'utf8'), context);
  return { emit, events, allow: () => context.__mygoTouchScroll.allow() };
}

// Vertical drags scroll only an opted-in canvas region, canceling the tap.
test('vertical touch pans without clicking on release', () => {
  const f = fixture();
  f.emit('pointerdown');
  f.allow();
  assert.equal(f.emit('pointermove', { clientY: 197 }).stopped, undefined);
  assert.equal(f.emit('pointermove', { clientY: 170 }).stopped, true);
  f.emit('pointermove', { clientY: 150 });
  assert.equal(f.emit('pointerup', { clientY: 150 }).stopped, true);
  assert.deepEqual(f.events.map(e => [e.type, e.deltaY]), [['pointercancel', undefined], ['wheel', 30], ['wheel', 20]]);
});

// Taps, horizontal control drags and unregistered regions retain their input.
test('leaves taps, horizontal drags, mouse and non-scroll areas alone', () => {
  for (const mode of ['tap', 'horizontal', 'mouse', 'non-scroll']) {
    const f = fixture();
    const pointerType = mode === 'mouse' ? 'mouse' : 'touch';
    f.emit('pointerdown', { pointerType });
    if (mode !== 'non-scroll') f.allow();
    const move = mode === 'tap' ? { clientY: 198 } : mode === 'horizontal' ? { clientX: 140, clientY: 198 } : { clientY: 100 };
    assert.equal(f.emit('pointermove', { pointerType, ...move }).stopped, undefined);
    if (mode === 'horizontal') f.emit('pointermove', { clientX: 150, clientY: 100 });
    assert.equal(f.emit('pointerup', { pointerType, ...move }).stopped, undefined);
    assert.equal(f.events.length, 0, mode);
  }
});

// Secondary fingers and system cancellation cannot take over an active scroll.
test('ignores secondary pointers and stops on cancellation', () => {
  const f = fixture();
  f.emit('pointerdown'); f.allow();
  f.emit('pointerdown', { pointerId: 2, isPrimary: false });
  f.emit('pointermove', { pointerId: 2, isPrimary: false, clientY: 100 });
  assert.equal(f.events.length, 0);
  f.emit('pointercancel');
  f.emit('pointermove', { clientY: 100 });
  assert.equal(f.events.length, 0);
});
