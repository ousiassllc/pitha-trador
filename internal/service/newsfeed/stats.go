package newsfeed

import "context"

// IndexStats is what YanoshinClient.Fetch saw in the index it decoded, for a
// caller that must know how the returned items relate to the raw response
// (the disclosure-skip recorder: a full response may have been cut off even
// when empty-titled entries were dropped before the items were returned).
// One IndexStats belongs to one Fetch; it is not safe to share between
// concurrent fetches.
type IndexStats struct {
	// Raw is the number of entries in the decoded response.
	Raw int
	// Dropped is how many of them were left out of the returned items
	// because they had no title.
	Dropped int
}

type indexStatsKey struct{}

// WithIndexStats returns a context that asks YanoshinClient.Fetch to write the
// index it decoded into stats. Feeds that do not know it ignore it, and the
// items and error Fetch returns are the same with or without it.
func WithIndexStats(ctx context.Context, stats *IndexStats) context.Context {
	return context.WithValue(ctx, indexStatsKey{}, stats)
}

func indexStatsFrom(ctx context.Context) *IndexStats {
	stats, _ := ctx.Value(indexStatsKey{}).(*IndexStats)
	return stats
}
