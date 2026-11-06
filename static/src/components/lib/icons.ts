// UI icons from Iconoir (https://iconoir.com, MIT, iconoir 7.12.2 "regular"
// set), per the Ousiass design guide §6: 24x24 viewBox, stroke 1.5,
// `currentColor`, 16px (--icon-sm) here. Decorative (`aria-hidden`): the
// control that holds the icon carries the accessible name. The templ
// counterpart is internal/web/atoms/icon.templ (same paths).
import { html } from 'lit';

const svg = (path: string) =>
  html`<svg class="inline-block icon-sm shrink-0 align-text-bottom" viewBox="0 0 24 24" fill="none" stroke-width="1.5" aria-hidden="true" focusable="false" xmlns="http://www.w3.org/2000/svg"><path d=${path} stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"></path></svg>`;

/** Iconoir `nav-arrow-up`. */
export const navArrowUp = () => svg('M6 15L12 9L18 15');
/** Iconoir `nav-arrow-down`. */
export const navArrowDown = () => svg('M6 9L12 15L18 9');
