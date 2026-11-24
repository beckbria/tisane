/**
 * Copies plain text to the system clipboard, falling back to an off-screen textarea
 * when navigator.clipboard is unavailable (e.g. non-HTTPS server mode or test environments).
 */
export async function copyTextToClipboard(text: string): Promise<boolean> {
  if (!text) return false;

  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to execCommand fallback
    }
  }

  if (typeof document === 'undefined' || !document.body) {
    return false;
  }

  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', 'true');
  textarea.setAttribute('aria-hidden', 'true');
  textarea.style.position = 'fixed';
  textarea.style.top = '-9999px';
  textarea.style.left = '-9999px';
  textarea.style.opacity = '0';
  document.body.appendChild(textarea);

  const prevFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  textarea.select();
  let ok = false;
  try {
    ok = typeof document.execCommand === 'function' && document.execCommand('copy');
  } catch {
    ok = false;
  } finally {
    document.body.removeChild(textarea);
    if (prevFocus && document.contains(prevFocus)) {
      prevFocus.focus({ preventScroll: true });
    }
  }
  return ok;
}
