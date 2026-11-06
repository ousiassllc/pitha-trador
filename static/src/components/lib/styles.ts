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
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    background: var(--surface);
    color: var(--ink);
    min-height: 2rem;
    padding: 0.25rem 0.75rem;
    font: inherit;
    font-size: 1rem;
    font-weight: 600;
    transition: background-color var(--motion-base) var(--ease-out);
  }
  button:hover:not(:disabled) {
    background: var(--accent-soft);
  }
  button:focus-visible {
    outline: 2px solid var(--focus);
    outline-offset: 2px;
  }
  button:disabled {
    cursor: not-allowed;
    opacity: 0.4;
  }
`;

// Table recipe (brand-guide §2/§3): header row SemiBold 12px --mute with a 1px
// --border underline, cells padded 10 / 12. Numeric columns carry `.num`
// (JetBrains Mono, tabular, right-aligned); label columns stay in the body font.
export const tableStyles = css`
  table {
    border-collapse: collapse;
    font-size: 0.875rem;
  }
  th {
    padding: 0.625rem 0.75rem;
    border-bottom: 1px solid var(--border);
    color: var(--mute);
    font-size: 0.75rem;
    font-weight: 600;
    text-align: left;
  }
  tbody th {
    border-bottom: 0;
    color: var(--ink);
    font-size: 0.875rem;
    font-weight: 400;
  }
  td {
    padding: 0.625rem 0.75rem;
  }
  .num {
    font-family: "JetBrains Mono", ui-monospace, monospace;
    font-variant-numeric: tabular-nums;
    text-align: right;
  }
`;

// Error text (`role="alert"`) and the shared ws-status notice
// (lib/ws-status.ts).
export const noticeStyles = css`
  [role='alert'] {
    margin: 0;
    color: var(--danger);
    font-size: 0.875rem;
  }
  .pitha-ws-disconnected {
    margin: 0;
    color: var(--warning);
    font-size: 0.75rem;
  }
`;

// Tailwind counterparts of `noticeStyles` for the light-DOM components
// (issue #355). Kept as whole literals so Tailwind's source scan emits them.
export const lightDomErrorClass = 'text-sm text-danger';
export const lightDomWsNoticeClass = 'text-xs text-warning';
