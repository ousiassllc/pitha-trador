package notify_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/notify"
)

func TestMaintenanceChannel_SlackConfiguredSelectsSlack(t *testing.T) {
	var buf bytes.Buffer
	logN := notify.NewLogNotifier(newBufferLogger(&buf))
	var got []string
	slackN, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	ch := notify.MaintenanceChannel(logN, slackN)
	if ch != notify.MaintenanceNotifier(slackN) {
		t.Fatalf("channel = %T, want the SlackNotifier", ch)
	}
	if err := ch.MaintenanceFailed(context.Background(), "DBバックアップ", 3, errors.New("drive not mounted")); err != nil {
		t.Fatalf("MaintenanceFailed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("slack posts = %v, want exactly one", got)
	}
	if buf.Len() != 0 {
		t.Errorf("log output = %q, want none when Slack is configured", buf.String())
	}
}

func TestMaintenanceChannel_NilSlackSelectsLog(t *testing.T) {
	var buf bytes.Buffer
	logN := notify.NewLogNotifier(newBufferLogger(&buf))

	ch := notify.MaintenanceChannel(logN, nil)
	if ch != notify.MaintenanceNotifier(logN) {
		t.Fatalf("channel = %T, want the LogNotifier", ch)
	}
	if err := ch.MaintenanceFailed(context.Background(), "DBバックアップ", 3, errors.New("drive not mounted")); err != nil {
		t.Fatalf("MaintenanceFailed: %v", err)
	}
	if rec := decodeRecord(t, &buf); rec["task"] != "DBバックアップ" {
		t.Errorf("record = %v, want the maintenance failure logged", rec)
	}
}
