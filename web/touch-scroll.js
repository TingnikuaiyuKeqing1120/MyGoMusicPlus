// MyGo's canvas receives pointer and wheel input. A touch beginning inside a
// Go scroll container opts into this adapter, which turns vertical drags into
// precise wheel input and cancels the original press before it can click.
(() => {
  let gesture = null;
  globalThis.__mygoTouchScroll = {
    allow() {
      if (gesture) gesture.allowed = true;
    },
  };
  const block = event => {
    event.preventDefault();
    event.stopImmediatePropagation();
  };
  addEventListener('pointerdown', event => {
    if (event.pointerType !== 'touch' || !event.isPrimary || event.target.tagName !== 'CANVAS') return;
    gesture = {
      id: event.pointerId,
      canvas: event.target,
      x: event.clientX,
      y: event.clientY,
      lastY: event.clientY,
      allowed: false,
      scrolling: false,
      horizontal: false,
    };
  }, { capture: true, passive: true });
  addEventListener('pointermove', event => {
    const active = gesture;
    if (!active || event.pointerId !== active.id || !active.allowed || active.horizontal) return;
    const dx = event.clientX - active.x;
    const dy = event.clientY - active.y;
    if (!active.scrolling) {
      if (Math.max(Math.abs(dx), Math.abs(dy)) < 8) return;
      if (Math.abs(dx) > Math.abs(dy)) {
        active.horizontal = true;
        return;
      }
      active.scrolling = true;
      active.canceling = true;
      active.canvas.dispatchEvent(new PointerEvent('pointercancel', {
        bubbles: true,
        pointerId: active.id,
        pointerType: 'touch',
        isPrimary: true,
        clientX: active.x,
        clientY: active.y,
      }));
      active.canceling = false;
    }
    block(event);
    active.canvas.dispatchEvent(new WheelEvent('wheel', {
      bubbles: true,
      cancelable: true,
      clientX: active.x,
      clientY: active.y,
      deltaY: active.lastY - event.clientY,
      deltaMode: 0,
    }));
    active.lastY = event.clientY;
  }, { capture: true, passive: false });
  addEventListener('pointerup', event => {
    if (!gesture || event.pointerId !== gesture.id) return;
    if (gesture.scrolling) block(event);
    gesture = null;
  }, { capture: true, passive: false });
  addEventListener('pointercancel', event => {
    if (gesture && event.pointerId === gesture.id && !gesture.canceling) gesture = null;
  }, true);
})();
