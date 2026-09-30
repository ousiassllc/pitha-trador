// Shared "live updates are not arriving" notice rendered by every
// `pitha-*` component that owns a WsClient (issue #133), so a dropped
// connection is visible instead of the UI silently going stale.
import { html, nothing } from 'lit';
import { isWsDisconnected, type WsStatus } from './ws';

export function renderWsDisconnected(status: WsStatus) {
  if (!isWsDisconnected(status)) return nothing;
  return html`<p class="pitha-ws-disconnected" role="status" data-ws-status=${status}>
    接続が切れています。再接続を試行しています（更新が反映されない場合はページを再読み込みしてください）。
  </p>`;
}
