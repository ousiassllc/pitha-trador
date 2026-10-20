import { afterEach, describe, expect, test } from 'bun:test';
import { forward, readRoutes, routeFor } from './pitha-settings-hub';

const routes = { 'modal-jev': '/settings/integrations', 'update-panel': '/settings/maintenance' };

afterEach(() => {
  document.body.innerHTML = '';
  location.hash = '';
});

describe('routeFor', () => {
  test('a dialog id and an alias map to their page', () => {
    expect(routeFor(routes, '#modal-jev')).toBe('/settings/integrations');
    expect(routeFor(routes, '#update-panel')).toBe('/settings/maintenance');
  });

  test('no hash, an unknown hash and inherited names map to nothing', () => {
    expect(routeFor(routes, '')).toBeNull();
    expect(routeFor(routes, '#modal-unknown')).toBeNull();
    expect(routeFor(routes, '#toString')).toBeNull();
  });

  test('a malformed percent-encoding does not throw', () => {
    expect(routeFor(routes, '#%E0%A4%A')).toBeNull();
  });
});

describe('readRoutes', () => {
  test('parses the hub attribute; missing or malformed gives an empty map', () => {
    document.body.innerHTML = `<div data-hash-routes='{"modal-jev":"/settings/integrations"}'></div>`;
    expect(readRoutes()).toEqual({ 'modal-jev': '/settings/integrations' });
    document.body.innerHTML = `<div data-hash-routes="not json"></div>`;
    expect(readRoutes()).toEqual({});
    document.body.innerHTML = '';
    expect(readRoutes()).toEqual({});
  });
});

describe('forward', () => {
  test('replaces the hub with the page path keeping the hash', () => {
    document.body.innerHTML = `<div data-hash-routes='{"modal-jev":"/settings/integrations"}'></div>`;
    location.hash = '#modal-jev';
    const seen: string[] = [];
    forward((url) => seen.push(url));
    expect(seen).toEqual(['/settings/integrations#modal-jev']);
  });

  test('stays on the hub without a known hash', () => {
    document.body.innerHTML = `<div data-hash-routes='{"modal-jev":"/settings/integrations"}'></div>`;
    const seen: string[] = [];
    forward((url) => seen.push(url));
    expect(seen).toEqual([]);
  });
});
