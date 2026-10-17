import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import type { AlignmentResponse, Distribution } from './exit-alignment-view';

await import('./pitha-exit-alignment');

type Element = HTMLElement & { updateComplete: Promise<boolean> };

let originalFetch: typeof fetch;
beforeEach(() => {
  originalFetch = globalThis.fetch;
  document.body.innerHTML = '';
});
afterEach(() => {
  globalThis.fetch = originalFetch;
  document.body.innerHTML = '';
});

const dist = (mean: number): Distribution => ({
  count: 4,
  mean,
  std_dev: 12,
  min: -30,
  p5: -25,
  p25: -10,
  median: mean,
  p75: 5,
  p95: 20,
  max: 30,
});

const body = (overrides: Partial<AlignmentResponse> = {}): AlignmentResponse => ({
  rows: [
    {
      key: 'take_profit',
      item: '利確',
      label_text: 'グロス 40 bps',
      exit_text: '120 bps',
      comparable: true,
      aligned: false,
    },
    {
      key: 'trailing_stop',
      item: 'トレーリングストップ',
      label_text: '表現できない',
      exit_text: '0.5 %',
      comparable: false,
      aligned: false,
    },
  ],
  warnings: [
    {
      code: 'take_profit_mismatch',
      message: '利確がラベル 40 bps と実出口 120 bps で一致しません',
    },
  ],
  tolerance_bps: 5,
  tolerance_minutes: 1,
  ...overrides,
});

async function mount(data: AlignmentResponse) {
  globalThis.fetch = mock(() =>
    Promise.resolve(new Response(JSON.stringify(data))),
  ) as unknown as typeof fetch;
  const el = document.createElement('pitha-exit-alignment') as Element;
  el.setAttribute('alignment-url', '/api/v1/calibration/exit-alignment');
  document.body.appendChild(el);
  // fetch -> json() -> state -> Lit update is promise-based: one real macrotask settles it.
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, 0);
  await promise;
  await el.updateComplete;
  return el;
}

const text = (el: Element, testid: string) =>
  el.shadowRoot
    ?.querySelector(`[data-testid="${testid}"]`)
    ?.textContent?.replace(/\s+/g, ' ')
    .trim();

// Issue #774: a label/exit mismatch shows as a warning beside the table.
describe('pitha-exit-alignment', () => {
  test('shows the warnings and the table rows', async () => {
    const el = await mount(body());
    expect(text(el, 'exit-alignment-warnings')).toContain('利確がラベル 40 bps');
    const rows = [...(el.shadowRoot?.querySelectorAll('[data-testid="exit-alignment-row"]') ?? [])];
    expect(rows.map((r) => r.getAttribute('data-key'))).toEqual(['take_profit', 'trailing_stop']);
    expect(rows[0]?.textContent).toContain('不一致');
    expect(rows[1]?.textContent).toContain('比較不可');
  });

  test('says so when label and exit agree', async () => {
    const el = await mount(body({ warnings: [] }));
    expect(el.shadowRoot?.querySelector('[data-testid="exit-alignment-warnings"]')).toBeNull();
    expect(text(el, 'exit-alignment-ok')).toContain('一致');
  });

  test('renders the comparison and the OOS EV beside the paper return, データ無し without positions', async () => {
    const el = await mount(
      body({
        comparison: {
          from: '2026-09-01T00:00:00Z',
          to: '2026-10-01T00:00:00Z',
          horizon_minutes: 10,
          replayed_count: 4,
          skipped_count: 1,
          label_net_bps: dist(30),
          replay_net_bps: dist(9),
          diff_bps: dist(-21),
          label_tp_count: 3,
          label_sl_count: 1,
          label_timeout_count: 0,
          replay_exits: [{ reason: 'trailing_stop', count: 4 }],
          paper_exits: [],
          not_evaluated_exits: ['jev_direction_reversed'],
          cost_long_bps: 10,
          cost_short_bps: 10,
        },
        oos_paper: {
          model_version: 'v1',
          oos_start: '2026-09-01T00:00:00Z',
          oos_end: '2026-10-01T00:00:00Z',
          oos_ev_bps: 12,
          paper_trades: 0,
          paper_mean_bps: null,
          diff_bps: null,
        },
      }),
    );
    expect(text(el, 'exit-alignment-label-hits')).toBe('3 / 1 / 0');
    expect(text(el, 'exit-alignment-replay-exits')).toBe('trailing_stop 4');
    expect(text(el, 'exit-alignment-paper-exits')).toBe('データ無し');
    expect(text(el, 'exit-alignment-oos-ev')).toBe('+12.0 bps');
    expect(text(el, 'exit-alignment-paper-mean')).toBe('データ無し');
    expect(text(el, 'exit-alignment-oos-diff')).toBe('データ無し');
    expect(
      el.shadowRoot?.querySelector('[data-testid="exit-alignment-comparison"]')?.textContent,
    ).toContain('-21.0 bps');
  });
});
