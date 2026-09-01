// React's act() only flushes state updates synchronously when this global
// is set — without it, DOM tests observe stale state right after an act()
// block (a button still reading `disabled` before React re-rendered).
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
