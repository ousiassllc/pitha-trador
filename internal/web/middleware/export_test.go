package middleware

// NewHeartbeatWithClock exposes newHeartbeat so tests can drive the
// throttle interval and clock deterministically.
var NewHeartbeatWithClock = newHeartbeat
