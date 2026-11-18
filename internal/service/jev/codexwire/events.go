package codexwire

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

// maxStdout bounds what is read from the CLI's stdout (the JSONL events).
const maxStdout = 8 << 20

// maxEventLine bounds one JSONL line.
const maxEventLine = 4 << 20

// usage is the token usage of a turn.completed event.
type usage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

// outcome is what the JSONL events of one run say. failedText and errorText are
// used only to label a failure (MatchUsageLimit, ParseResetTime); they are never
// stored.
type outcome struct {
	threadID string
	// completed: a turn.completed was seen; failed: a turn.failed was.
	completed, failed bool
	usage             usage
	// message is the text of the last agent_message item (the model's answer).
	message string
	// failedText is the turn.failed error message, errorText the last error event's.
	failedText, errorText string
}

type event struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Usage    usage  `json:"usage"`
	Message  string `json:"message"`
	Error    struct {
		Message string `json:"message"`
	} `json:"error"`
	Item struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

// parseEvents reads the JSONL events of stdout. A line that is not a JSON event
// is skipped; the judgement of the run is made by the caller from the exit code
// and the turn events (turn.completed, turn.failed) only. An error event is not
// a failure: the CLI can emit one while it retries.
func parseEvents(r io.Reader) outcome {
	var out outcome
	sc := bufio.NewScanner(io.LimitReader(r, maxStdout))
	sc.Buffer(make([]byte, 0, 64<<10), maxEventLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev event
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "thread.started":
			out.threadID = ev.ThreadID
		case "turn.completed":
			out.completed, out.usage = true, ev.Usage
		case "turn.failed":
			out.failed, out.failedText = true, ev.Error.Message
		case "error":
			out.errorText = ev.Message
		case "item.completed":
			if ev.Item.Type == "agent_message" {
				out.message = ev.Item.Text
			}
		}
	}
	return out
}
