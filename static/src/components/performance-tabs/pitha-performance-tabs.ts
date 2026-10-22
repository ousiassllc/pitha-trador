// Forwards the pre-#895 `/performance#backtest` link to the バックテスト tab
// (issue #895). The 成績 page used to show the Walk Forward form below the
// realized performance, and the form's anchor was `#backtest`; the form now
// lives on its own tab (`/performance?tab=backtest`), and a URL fragment
// never reaches the server, so this script on the 実績 tab does the redirect.

/** The URL of the バックテスト tab when the current URL is the old backtest anchor, else null. */
export function backtestTabUrl(pathname: string, search: string, hash: string): string | null {
  if (pathname !== '/performance' || hash !== '#backtest') return null;
  const params = new URLSearchParams(search);
  params.set('tab', 'backtest');
  return `${pathname}?${params.toString()}`;
}

/** Replaces the 実績 tab with the バックテスト tab for an old `#backtest` link. */
export function forward(replace: (url: string) => void = (url) => location.replace(url)): void {
  const url = backtestTabUrl(location.pathname, location.search, location.hash);
  if (url) replace(url);
}

forward();
