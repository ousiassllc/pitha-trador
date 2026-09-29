package activityfeed

import (
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func jobEvent(job repository.Job) domain.ActivityEvent {
	ts := job.CreatedAt
	switch {
	case job.FinishedAt != nil:
		ts = *job.FinishedAt
	case job.StartedAt != nil:
		ts = *job.StartedAt
	}

	detail := fmt.Sprintf("queue=%s status=%s attempts=%d", job.Queue, job.Status, job.Attempts)
	if job.LastError != nil && *job.LastError != "" {
		detail += " error=" + *job.LastError
	}

	var latency *int
	if job.StartedAt != nil && job.FinishedAt != nil {
		ms := int(job.FinishedAt.Sub(*job.StartedAt).Milliseconds())
		latency = &ms
	}

	// Scheduler payloads for per-symbol jobs carry a top-level "symbol";
	// other payloads (or none) simply yield no symbol.
	var payload struct {
		Symbol string `json:"symbol"`
	}
	_ = json.Unmarshal([]byte(job.PayloadJSON), &payload)

	return domain.ActivityEvent{
		Type:      domain.ActivityTypeJob,
		Timestamp: ts,
		Queue:     job.Queue,
		Symbol:    payload.Symbol,
		Detail:    detail,
		LatencyMs: latency,
	}
}

func decisionEvent(d domain.JevDecision) domain.ActivityEvent {
	eventType := domain.ActivityTypeJevScout
	detail := "question_version=" + d.QuestionVersion
	if d.DecisionType == domain.JevDecisionTypeTrader {
		eventType = domain.ActivityTypeJevTrader
		detail = ""
		if d.Direction != nil {
			detail = "direction=" + *d.Direction
		}
		if d.Confidence != nil {
			if detail != "" {
				detail += " "
			}
			detail += fmt.Sprintf("confidence=%.2f", *d.Confidence)
		}
	}
	latency := d.LatencyMs
	return domain.ActivityEvent{
		Type:      eventType,
		Timestamp: d.Timestamp,
		Symbol:    d.Symbol,
		Detail:    detail,
		LatencyMs: &latency,
	}
}

func killSwitchEvent(ke domain.KillSwitchEvent) domain.ActivityEvent {
	detail := "reason=" + ke.Reason
	if ke.ResolvedBy != nil {
		detail += " resolved_by=" + *ke.ResolvedBy
	}
	return domain.ActivityEvent{
		Type:      domain.ActivityTypeKillSwitch,
		Timestamp: ke.TriggeredAt,
		Detail:    detail,
	}
}
