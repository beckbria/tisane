import '@testing-library/jest-dom/vitest';

// Provide ResizeObserver for @tanstack/svelte-virtual in jsdom.
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

// Stub window.fetch in jsdom so @wailsio/runtime's boot handshake does not throw network errors.
if (typeof globalThis.fetch === 'undefined' || !('mock' in globalThis.fetch)) {
  globalThis.fetch = async () => new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } });
}

if (typeof Element !== 'undefined' && !Element.prototype.scrollTo) {
  Element.prototype.scrollTo = function () {};
}

// jsdom has no layout engine and returns 0 for clientWidth/clientHeight; give scroll containers realistic viewport dimensions.
if (typeof HTMLElement !== 'undefined') {
  Object.defineProperty(HTMLElement.prototype, 'clientWidth', {
    configurable: true,
    get() {
      return 480;
    }
  });
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get() {
      return 480;
    }
  });
}
