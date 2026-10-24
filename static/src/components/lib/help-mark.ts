// The 「?」 help mark for Lit components (issue #902): the same markup and
// behaviour as `atoms.HelpMark` (internal/web/atoms/help_mark.templ), so a
// screen reads the same wherever the text is rendered. The behaviour comes
// from static/src/components/help-mark/pitha-help-mark.ts, which layout.document
// loads on every page and which also works inside a shadow root.
//
// Use `renderHelpMark` next to the item name; a component that keeps Lit's
// shadow root adds `helpMarkStyles` to its `static styles` (Tailwind is
// document CSS and does not cross the boundary); one that renders into the
// light DOM (scanner-table, activity-feed) uses the Tailwind classes from the
// templ atom and needs no styles.
import { css, html, type TemplateResult } from 'lit';

export interface HelpMarkOptions {
  /** Unique id of the help text on the page (the button's `aria-describedby`). */
  id: string;
  /** Accessible name of the 「?」 button, e.g. 「新規エントリーの説明」. */
  label: string;
  /** The help text (plain text; Lit escapes it). */
  text: string;
}

export function renderHelpMark({ id, label, text }: HelpMarkOptions): TemplateResult {
  return html`<span class="help-mark" data-help-mark
    ><button type="button" class="help-mark-button" aria-label=${label} aria-describedby=${id} data-help-mark-button>?</button
    ><span id=${id} role="tooltip" class="help-mark-text" data-help-mark-text>${text}</span></span
  >`;
}

/** Shadow DOM styles for `renderHelpMark`; mirror the classes of `atoms.HelpMark`. */
export const helpMarkStyles = css`
  .help-mark {
    position: relative;
    display: inline-flex;
    vertical-align: middle;
  }
  .help-mark-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
    width: 1rem;
    height: 1rem;
    padding: 0;
    border: 1px solid #94a3b8;
    border-radius: 9999px;
    background: #ffffff;
    color: #475569;
    font: inherit;
    font-size: 10px;
    font-weight: 600;
    line-height: 1;
    cursor: help;
  }
  .help-mark-button:hover {
    border-color: #475569;
    color: #0f172a;
    background: #ffffff;
  }
  .help-mark-button:focus-visible {
    outline: 2px solid #2563eb;
    outline-offset: 2px;
  }
  .help-mark-text {
    visibility: hidden;
    position: fixed;
    top: 0;
    left: 0;
    z-index: 50;
    box-sizing: border-box;
    width: 18rem;
    max-width: 80vw;
    padding: 0.5rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.375rem;
    background: #ffffff;
    color: #334155;
    box-shadow: 0 10px 15px -3px rgb(0 0 0 / 0.1);
    font-size: 0.75rem;
    font-weight: 400;
    line-height: 1.625;
    text-align: left;
  }
  .help-mark-text[data-open] {
    visibility: visible;
  }
`;
