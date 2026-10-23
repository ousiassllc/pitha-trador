// Package homepanels renders the Home screen (issue #896) from plain, already
// worked-out views (views.go): internal/web/handler/home maps what it reads
// onto them, so a Templ file never imports a service. Every card and block
// carries its own Err, so a failed read shows as that card's error line and
// never takes the screen down. It is its own package because
// internal/web/organisms is at its line limit.
package homepanels
