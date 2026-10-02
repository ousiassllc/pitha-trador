package logging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// endlessLines yields the same newline-terminated line forever and counts
// how many bytes were read from it.
type endlessLines struct {
	line string
	pos  int
	read int
}

func (r *endlessLines) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		c := copy(p[n:], r.line[r.pos:])
		n += c
		r.pos = (r.pos + c) % len(r.line)
	}
	r.read += n
	return n, nil
}

// issue #289: once fn asks to stop, readCompleteLines must not read on.
func TestReadCompleteLines_StopsWhenCallbackSaysSo(t *testing.T) {
	src := &endlessLines{line: rec("2026-10-01T00:00:00Z", "ERROR", "boom")}
	calls := 0
	err := readCompleteLines(context.Background(), src, func([]byte) bool {
		calls++
		return true
	})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d, want nil and 1", err, calls)
	}
	if src.read > 1<<20 {
		t.Errorf("read %d bytes of an endless source after the stop request", src.read)
	}
}

// issue #289: cancelling ctx stops the read inside a single file.
func TestReadCompleteLines_StopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &endlessLines{line: rec("2026-10-01T00:00:00Z", "ERROR", "boom")}
	calls := 0
	err := readCompleteLines(ctx, src, func([]byte) bool {
		calls++
		if calls == 3 {
			cancel()
		}
		return false
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls > 3+1024 { // a few buffered lines may still be consumed per read
		t.Errorf("fn called %d times after cancel", calls)
	}
}

func TestExporter_CancelledContextIsError(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-10-01.log", rec("2026-10-01T00:00:00Z", "ERROR", "x"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newTestExporter(dir).Export(ctx, 1, slog.LevelError); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// issue #289: with the byte budget spent, an older day only decides
// Truncated: any matching record sets it, none leaves it false.
func TestExporter_SpentBudgetOnlyProbesOlderDays(t *testing.T) {
	line := rec("2026-10-01T00:00:00Z", "ERROR", "t0")
	tests := []struct {
		name          string
		older         string
		wantTruncated bool
	}{
		{"older day has matching records", rec("2026-09-30T00:00:00Z", "INFO", "i") + rec("2026-09-30T00:00:01Z", "ERROR", "y0") + rec("2026-09-30T00:00:02Z", "ERROR", "y1"), true},
		{"older day has nothing at the level", rec("2026-09-30T00:00:00Z", "INFO", "i") + "garbage\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLogFile(t, dir, "2026-10-01.log", line)
			writeLogFile(t, dir, "2026-09-30.log", tt.older)
			e := newTestExporter(dir)
			e.maxBytes = len(line) // exactly spent by today's record

			res := export(t, e, 2, slog.LevelError)
			if got := msgs(t, res); strings.Join(got, ",") != "t0" {
				t.Errorf("messages = %v, want only the newest record", got)
			}
			if res.Truncated != tt.wantTruncated {
				t.Errorf("Truncated = %v, want %v", res.Truncated, tt.wantTruncated)
			}
		})
	}
}

// FR-ERRLOG-6: reading a daily file that is being appended to concurrently
// yields only complete records, never a half-written line.
func TestExporter_ReadsFileBeingWrittenConcurrently(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10-01.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer func() { _ = f.Close() }()

	const records = 300
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := range records {
			line := rec("2026-10-01T00:00:00Z", "ERROR", fmt.Sprintf("w%03d", i))
			cut := len(line) / 2
			// Two writes: a reader can land between them and see a torn line.
			if _, err := io.WriteString(f, line[:cut]); err != nil {
				t.Errorf("write: %v", err)
				return
			}
			if _, err := io.WriteString(f, line[cut:]); err != nil {
				t.Errorf("write: %v", err)
				return
			}
		}
	}()

	e := newTestExporter(dir)
	last := 0
	for done := false; !done; {
		select {
		case <-finished:
			done = true // one more pass below sees the finished file
		default:
		}
		res := export(t, e, 1, slog.LevelError)
		for i, line := range strings.Split(strings.TrimSuffix(string(res.Data), "\n"), "\n") {
			if line == "" {
				continue
			}
			var r struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Fatalf("torn or invalid line %q: %v", line, err)
			}
			if want := fmt.Sprintf("w%03d", i); r.Msg != want {
				t.Fatalf("record %d = %q, want %q (records must be a gapless prefix)", i, r.Msg, want)
			}
		}
		if res.Records < last {
			t.Fatalf("Records went from %d down to %d", last, res.Records)
		}
		last = res.Records
	}
	if last != records {
		t.Errorf("final Records = %d, want %d", last, records)
	}
}
