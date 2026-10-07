// Package activity serves the System Activity Log: the /activity page,
// GET /api/v1/activity and the /ws/activity push stream.
// It reads events through the ActivitySource port (internal/service/activityfeed)
// and depends only on internal/service, internal/domain and handler/shared;
// it MUST NOT import sibling handler subpackages.
package activity
