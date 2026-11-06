// Theme switch of the header (issue #978; molecules.ThemeToggle). CSP forbids
// inline JS, so the markup is wired here by attribute: a
// `[data-theme-choice="auto|light|dark"]` button saves that choice, applies it
// to <html data-theme> (lib/theme.ts) and marks itself `aria-pressed`, which is
// also what styles the selected segment. The pressed state is set from
// localStorage on load because the server cannot know the browser's choice.

import { isThemeChoice, setTheme, storedTheme } from '../lib/theme';

/** Marks the button of the saved choice as pressed and the others as not. */
export function sync(): void {
  const current = storedTheme();
  for (const button of document.querySelectorAll<HTMLElement>('[data-theme-choice]')) {
    button.setAttribute('aria-pressed', String(button.dataset.themeChoice === current));
  }
}

/** `click`: apply the choice of a `[data-theme-choice]` button. */
export function onClick(event: Event): void {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const choice = target.closest<HTMLElement>('[data-theme-choice]')?.dataset.themeChoice;
  if (!isThemeChoice(choice)) return;
  setTheme(choice);
  sync();
}

export function init(): void {
  document.addEventListener('click', onClick);
  sync();
}

init();
