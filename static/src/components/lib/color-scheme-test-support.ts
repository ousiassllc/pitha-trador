// Fake `matchMedia` for the chart tests: the OS theme switch is `fire()` (the
// `(prefers-color-scheme: dark)` query changing) and the page entering or leaving
// print is `firePrint(matches)` (the `print` query changing); `listeners()` counts
// who is still subscribed to either. `print` queries report the last `firePrint` value as `matches`.

export function installColorSchemeSwitch(): {
  fire: () => void;
  firePrint: (matches: boolean) => void;
  listeners: () => number;
  restore: () => void;
} {
  const original = globalThis.matchMedia;
  const scheme = new Set<(event: { matches: boolean }) => void>();
  const print = new Set<(event: { matches: boolean }) => void>();
  let printMatches = false;
  globalThis.matchMedia = ((query: string) => {
    const subscribed = query === 'print' ? print : scheme;
    return {
      matches: query === 'print' ? printMatches : false,
      addEventListener: (_type: string, listener: (event: { matches: boolean }) => void) =>
        subscribed.add(listener),
      removeEventListener: (_type: string, listener: (event: { matches: boolean }) => void) =>
        subscribed.delete(listener),
    };
  }) as unknown as typeof matchMedia;
  return {
    fire: () => {
      for (const listener of [...scheme]) listener({ matches: false });
    },
    firePrint: (matches) => {
      printMatches = matches;
      for (const listener of [...print]) listener({ matches });
    },
    listeners: () => scheme.size + print.size,
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
