package bootstrap

// NewsIngestEnabled reports whether Start launches the News Ingest polling
// loop: the feed is not switched off and Luna (Jev or LUNA_*) is available.
func (s *Services) NewsIngestEnabled() bool { return s.newsEnabled }
