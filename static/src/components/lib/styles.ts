// Shared Shadow DOM styles for the `pitha-*` components that keep Lit's
// default shadow root (issue #145). Tailwind (static/src/css/app.css) is
// document CSS and does not cross a shadow boundary, so these components
// carry their own `static styles`; scanner-table / activity-feed render into
// the light DOM instead and do not use the `css` blocks below; they use the
// Tailwind class constants at the end of this file for the same look.
import { css } from 'lit';

export const buttonStyles = css`
  button {
    cursor: pointer;
    border: 1px solid #cbd5e1;
    border-radius: 0.375rem;
    background: #ffffff;
    color: #0f172a;
    padding: 0.25rem 0.75rem;
    font: inherit;
    font-size: 0.875rem;
  }
  button:hover:not(:disabled) {
    background: #f1f5f9;
  }
  button:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }
`;

// Error text (`role="alert"`) and the shared ws-status notice
// (lib/ws-status.ts).
export const noticeStyles = css`
  [role='alert'] {
    margin: 0;
    color: #b91c1c;
    font-size: 0.875rem;
  }
  .pitha-ws-disconnected {
    margin: 0;
    color: #b45309;
    font-size: 0.75rem;
  }
`;

// Tailwind counterparts of `noticeStyles` for the light-DOM components
// (issue #355). Kept as whole literals so Tailwind's source scan emits them.
export const lightDomErrorClass = 'text-sm text-red-700';
export const lightDomWsNoticeClass = 'text-xs text-amber-700';
