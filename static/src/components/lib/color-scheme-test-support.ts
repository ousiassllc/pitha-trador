// Fake `matchMedia('(prefers-color-scheme: dark)')` for the chart tests: the OS
// theme switch is `fire()`, and `listeners()` shows who is still subscribed.

export function installColorSchemeSwitch(): {
  fire: () => void;
  listeners: () => number;
  restore: () => void;
} {
  const original = globalThis.matchMedia;
  const subscribed = new Set<() => void>();
  globalThis.matchMedia = (() => ({
    matches: false,
    addEventListener: (_type: string, listener: () => void) => subscribed.add(listener),
    removeEventListener: (_type: string, listener: () => void) => subscribed.delete(listener),
  })) as unknown as typeof matchMedia;
  return {
    fire: () => {
      for (const listener of [...subscribed]) listener();
    },
    listeners: () => subscribed.size,
    restore: () => {
      globalThis.matchMedia = original;
    },
  };
}

/** Dark values for the tokens the charts read, as app.css sets them under `prefers-color-scheme: dark`. */
export function setDarkTokens(): void {
  const style = document.documentElement.style;
  style.setProperty('--ink', '#ffffff');
  style.setProperty('--surface', '#141414');
  style.setProperty('--mute', '#8b8b8e');
  style.setProperty('--border', '#2a2a2a');
  style.setProperty('--info', '#9aabbc');
  style.setProperty('--success', '#8fafa0');
  style.setProperty('--warning', '#c4b87a');
}
