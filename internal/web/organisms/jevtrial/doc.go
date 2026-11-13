// Package jevtrial renders the Settings screen's model-comparison trial
// section: the switch (Paper only, off by default, turned on only here), the
// arms (a JSON array), the overall daily cost cap, and the state: the cost
// counted today and in total, and per arm its cost against its own cap, its
// failed calls and why it stopped. It takes plain, already formatted props
// (internal/web/handler/jevtrial maps internal/service/jevtrial's State onto
// them), so it never sees internal/service.
package jevtrial
