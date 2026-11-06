// Light / Dark theme choice (issue #978). app.css decides the theme from
// `<html data-theme>`: "light" or "dark" forces it, no attribute follows the OS
// (`prefers-color-scheme`). The header's switch offers those three states as
// "auto" | "light" | "dark" and keeps a forced choice in localStorage, so a
// reload keeps it ("auto" removes the key). Before first paint,
// static/src/vendor/theme-init.js (a classic script in <head>) applies the saved
// choice with the same key; theme.test.ts keeps the two in step.

export type ThemeChoice = 'auto' | 'light' | 'dark';

export const THEME_STORAGE_KEY = 'pitha-theme';

/** Fired on `document` after the theme changes, so canvases (charts) re-read the tokens. */
export const THEME_CHANGE_EVENT = 'pitha-theme-change';

export function isThemeChoice(value: unknown): value is ThemeChoice {
  return value === 'auto' || value === 'light' || value === 'dark';
}

/** The saved choice; "auto" when none is saved or storage is unavailable (private mode, blocked). */
export function storedTheme(): ThemeChoice {
  try {
    const saved = localStorage.getItem(THEME_STORAGE_KEY);
    return saved === 'light' || saved === 'dark' ? saved : 'auto';
  } catch {
    return 'auto';
  }
}

/** Puts `choice` on <html> ("auto" removes `data-theme`, so the OS decides) and announces it. */
export function applyTheme(choice: ThemeChoice): void {
  const root = document.documentElement;
  if (choice === 'auto') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', choice);
  document.dispatchEvent(new Event(THEME_CHANGE_EVENT));
}

/** Applies `choice` and saves it. A storage failure only costs persistence, not the switch. */
export function setTheme(choice: ThemeChoice): void {
  applyTheme(choice);
  try {
    if (choice === 'auto') localStorage.removeItem(THEME_STORAGE_KEY);
    else localStorage.setItem(THEME_STORAGE_KEY, choice);
  } catch {
    // The theme stays applied for this page view.
  }
}
