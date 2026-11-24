export interface ModalTrapOptions {
  onClose: () => void;
  disabled?: boolean;
}

interface ModalStackEntry {
  node: HTMLElement;
  getOptions: () => ModalTrapOptions;
}

const modalStack: ModalStackEntry[] = [];

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])'
].join(', ');

function getFocusableElements(container: HTMLElement): HTMLElement[] {
  const candidates = Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR));
  return candidates.filter((el) => !el.closest('[inert], [aria-hidden="true"]'));
}

export function hasActiveModal(): boolean {
  return modalStack.length > 0;
}

export function closeTopModalOnEscape(): boolean {
  const top = modalStack[modalStack.length - 1];
  if (!top) {
    return false;
  }
  const opts = top.getOptions();
  if (!opts.disabled) {
    opts.onClose();
  }
  return true;
}

/**
 * Svelte action that manages WAI-ARIA modal focus entry, Tab/Shift+Tab focus cycling,
 * top-most Escape ownership across stacked dialogs, backdrop click dismissal, and focus restoration.
 */
export function modalTrap(node: HTMLElement, initialOptions: ModalTrapOptions) {
  let opts = initialOptions;
  const previousActive =
    typeof document !== 'undefined' && document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;

  const entry: ModalStackEntry = {
    node,
    getOptions: () => opts
  };
  modalStack.push(entry);

  if (!node.hasAttribute('tabindex')) {
    node.setAttribute('tabindex', '-1');
  }

  // Move initial focus inside the dialog once mounted.
  queueMicrotask(() => {
    if (!document.contains(node)) return;
    if (modalStack[modalStack.length - 1] !== entry) return;
    if (node.contains(document.activeElement) && document.activeElement !== node) return;
    const autoTarget = node.querySelector<HTMLElement>('[data-autofocus]:not([disabled])');
    const focusables = getFocusableElements(node);
    const first = autoTarget ?? focusables[0] ?? node;
    first.focus();
  });

  function handleKeydown(e: KeyboardEvent) {
    if (modalStack[modalStack.length - 1] !== entry) {
      return;
    }
    if (e.key !== 'Tab') {
      return;
    }
    const focusables = getFocusableElements(node);
    if (focusables.length === 0) {
      e.preventDefault();
      node.focus();
      return;
    }
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    const active = document.activeElement as HTMLElement | null;

    if (e.shiftKey) {
      if (!active || active === first || active === node || !node.contains(active)) {
        e.preventDefault();
        last.focus();
      }
    } else {
      if (!active || active === last || !node.contains(active)) {
        e.preventDefault();
        first.focus();
      }
    }
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target !== node) return;
    if (modalStack[modalStack.length - 1] !== entry) return;
    if (!opts.disabled) {
      opts.onClose();
    }
  }

  node.addEventListener('keydown', handleKeydown);
  node.addEventListener('click', handleBackdropClick);

  return {
    update(nextOptions: ModalTrapOptions) {
      opts = nextOptions;
    },
    destroy() {
      node.removeEventListener('keydown', handleKeydown);
      node.removeEventListener('click', handleBackdropClick);
      const idx = modalStack.indexOf(entry);
      if (idx >= 0) {
        modalStack.splice(idx, 1);
      }
      queueMicrotask(() => {
        const nextTop = modalStack[modalStack.length - 1];
        if (
          previousActive &&
          document.contains(previousActive) &&
          (!nextTop || nextTop.node.contains(previousActive))
        ) {
          previousActive.focus();
          return;
        }
        if (nextTop && document.contains(nextTop.node)) {
          const focusables = getFocusableElements(nextTop.node);
          (focusables[0] ?? nextTop.node).focus();
        }
      });
    }
  };
}
