// Shared Shadow DOM styles for the `pitha-*` components that keep Lit's
// default shadow root (issue #145). Tailwind (static/src/css/app.css) is
// document CSS and does not cross a shadow boundary, so these components
// carry their own `static styles`; scanner-table / activity-feed render into
// the light DOM instead and do not use the `css` blocks below; they use the
// Tailwind class constants at the end of this file for the same look.
import { css } from 'lit';

export const buttonStyles = css`
  button {
    position: relative;
    overflow: hidden;
    cursor: pointer;
    border: 0;
    border-radius: var(--r-pill);
    background: var(--surface-sel);
    color: var(--ink);
    box-sizing: border-box;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 2rem;
    padding: 0 0.75rem;
    white-space: nowrap;
    font: inherit;
    font-size: 0.9375rem;
    line-height: 1.6;
    font-weight: 600;
    transition: background-color var(--motion-base) var(--ease-out);
  }
  button:hover:not(:disabled) {
    background: var(--surface-sel-hover);
  }
  button:focus-visible {
    outline: 2px solid var(--focus);
    outline-offset: 2px;
  }
  button:disabled {
    cursor: not-allowed;
    opacity: 0.4;
  }
  @media (prefers-reduced-motion: reduce) {
    button {
      transition: none;
    }
  }
`;

// Table recipe (brand-guide app UI): header row Regular 13px --mute without an
// underline, 15px body cells separated by a thin --line-soft rule (none under
// the last row), cells padded 10 / 12. Numeric columns carry `.num`
// (right-aligned); only the `td.num` cells are JetBrains Mono / tabular, so the
// column headings (`th.num`) stay in the body font; label columns stay in the
// body font too.
export const tableStyles = css`
  table {
    border-collapse: collapse;
    font-size: 0.9375rem;
    line-height: 1.6;
  }
  th {
    padding: 0.625rem 0.75rem;
    border-bottom: 0;
    color: var(--mute);
    font-size: 0.8125rem;
    line-height: 1.6;
    font-weight: 400;
    text-align: left;
  }
  tbody th {
    border-bottom: 1px solid var(--line-soft);
    color: var(--ink);
  }
  td {
    padding: 0.625rem 0.75rem;
    border-bottom: 1px solid var(--line-soft);
  }
  tbody tr:last-child td,
  tbody tr:last-child th {
    border-bottom: 0;
  }
  tbody tr:hover {
    background: var(--surface-sel);
  }
  .num {
    text-align: right;
  }
  td.num {
    font-family: "JetBrains Mono", ui-monospace, monospace;
    font-variant-numeric: tabular-nums;
  }
`;

// Error text (`role="alert"`) and the shared ws-status notice
// (lib/ws-status.ts).
export const noticeStyles = css`
  [role='alert'] {
    margin: 0;
    color: var(--danger);
    font-size: 0.9375rem;
    line-height: 1.6;
  }
  .pitha-ws-disconnected {
    margin: 0;
    color: var(--warning);
    font-size: 0.9375rem;
    line-height: 1.6;
  }
`;

// Tailwind counterparts of `noticeStyles` for the light-DOM components
// (issue #355). Kept as whole literals so Tailwind's source scan emits them.
export const lightDomErrorClass = 'text-item text-danger';
export const lightDomWsNoticeClass = 'text-item text-warning';

// Light-DOM form control class (brand-guide C3/O8): the same --surface ground, border, disabled look (opacity .5) and
// 2px --focus outline that atoms.Select draws (internal/web/atoms/input.templ),
// for a bare <select>/<input> a Lit component renders itself.
export const lightDomFieldClass =
  'h-10 rounded-md border border-border bg-surface px-3 disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
