import { describe, expect, mock, test } from 'bun:test';
import { createdCharts, installChartHarness } from './chart-test-support';

await import('./pitha-price-chart');

installChartHarness();

describe('pitha-price-chart print snapshot', () => {
  async function mount() {
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response(JSON.stringify({ symbol: '7203', candles: [] }))),
    ) as unknown as typeof fetch;
    const el = document.createElement('pitha-price-chart');
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await el.updateComplete;
    await new Promise((resolve) => setTimeout(resolve, 0));
    return el;
  }

  // The container is reused on reconnect: a snapshot left over from the print would cover the new chart.
  test('leaves no snapshot behind when removed while printing, and none on reconnect', async () => {
    const el = await mount();
    window.dispatchEvent(new Event('beforeprint'));
    const container = el.shadowRoot?.querySelector<HTMLElement>('.pitha-price-chart-container');
    expect(container?.querySelector('[data-print-snapshot]')).not.toBeNull();
    expect(container?.style.position).toBe('relative');

    el.remove();
    expect(container?.querySelector('[data-print-snapshot]')).toBeNull();
    expect(container?.style.position).toBe('');

    // afterprint arrives while the element is detached: the next connection must not stay "printing"
    window.dispatchEvent(new Event('afterprint'));
    document.body.appendChild(el);
    await el.updateComplete;
    expect(container?.querySelector('[data-print-snapshot]')).toBeNull();
    expect(createdCharts).toHaveLength(2);
    expect(createdCharts[1].takeScreenshot).not.toHaveBeenCalled();
  });
});
