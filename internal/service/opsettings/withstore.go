package opsettings

// WithStore returns a copy of s that reads and writes through store instead,
// with every other setting (risk reference, trading mode, ...) unchanged. It
// lets a caller run Save / Reset inside its own transaction by passing a Store
// over that transaction (the Settings screen's audit trail, issue #813).
func (s *Service) WithStore(store Store) *Service {
	c := *s
	c.store = store
	return &c
}
