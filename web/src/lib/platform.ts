/**
 * What to call the modifier key on this keyboard.
 *
 * The shortcut itself accepts both — `metaKey || ctrlKey` — because a Mac
 * keyboard plugged into Linux, a remote session and a virtual machine all
 * exist. Only the label has to pick one, and picking wrong makes the hint
 * actively misleading: someone on Windows reading ⌘K has been told to press
 * a key their keyboard does not have.
 *
 * `navigator.platform` is deprecated but still the most reliable signal for
 * this; `userAgentData` is not available in every browser this runs in, so
 * it is preferred when present and the old field is the fallback.
 */
export function isApplePlatform(): boolean {
  const ua = navigator as Navigator & { userAgentData?: { platform?: string } };
  const platform = ua.userAgentData?.platform ?? navigator.platform ?? "";
  return /mac|iphone|ipad|ipod/i.test(platform);
}

/** "⌘K" on an Apple keyboard, "Ctrl K" everywhere else. */
export function modifierLabel(key: string): string {
  return isApplePlatform() ? `⌘${key}` : `Ctrl ${key}`;
}
