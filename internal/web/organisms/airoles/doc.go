// Package airoles renders the Settings screen's 役割ごとのモデル section (issue
// #1080): the AI providers with whether their key is set, one form per AI role
// (provider and model; the live Jev role is marked as changeable on this screen
// only), the pair each role has run on since the last start, and the recent
// history of changes. It takes plain, already formatted props
// (internal/web/handler/airoles maps internal/service/airoles' State onto them),
// so it never sees internal/service. A key's value is never shown: only whether
// it is set.
package airoles
