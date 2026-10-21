// Package jevprompt renders the Settings screen's Jev prompt section (issue
// #872): the live prompt version, the shadow comparison switch (Paper only,
// off by default), the candidate version, the daily cost cap with the cost
// today and in total, and the explicit promotion with its audit rows. It takes
// plain, already formatted props (internal/web/handler/jevprompt maps
// internal/service/jevprompt's State onto them), so it never sees
// internal/service.
package jevprompt
