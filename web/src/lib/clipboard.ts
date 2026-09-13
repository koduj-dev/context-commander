/**
 * Copying a value to the clipboard, on the origins this product is actually
 * reached on.
 *
 * `navigator.clipboard` exists only in a secure context. A self-hosted
 * instance is very often reached first over plain http on a LAN address —
 * exactly how this one is — and there the API is not merely refused, it is
 * absent. Relying on it alone gave a button that did nothing at all and said
 * nothing either, which is worse than having no button.
 *
 * So: the modern API when it is there, the old `execCommand` path when it is
 * not, and an honest `false` when neither worked so the caller can say so.
 */
export async function copyToClipboard(value: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      return true;
    } catch {
      // Permission refused, or a browser that exposes the API on an
      // insecure origin and then rejects. Fall through.
    }
  }

  // Deprecated and synchronous, and the only thing that works over plain
  // http. The textarea is placed off-screen rather than hidden: a
  // `display: none` element cannot be selected, so it cannot be copied.
  try {
    const area = document.createElement("textarea");
    area.value = value;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.top = "-1000px";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    document.body.removeChild(area);
    return ok;
  } catch {
    return false;
  }
}
