package codexwire

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// The prompt and the schema of protocolVersion (codex-exec-v1) are built from the
// pieces the chat Completions arm shares (trialwire.QuestionsPrompt, StatePrompt,
// AnswerSchema, issue #1224). Sharing them must not move a byte of what Codex is
// sent: this pins trader-v3's prompt (over an empty state) and schema as they were
// before they were shared. A change of these bytes is a change of the protocol: it
// needs a new protocolVersion and a new digest here, not a silent edit.
func TestPromptAndSchemaOfTheCodexProtocolAreUnchanged(t *testing.T) {
	const wantPrompt = "ad5b4dbf3c78a3c507b1874a39bf8bfc2e2bb837177e6f091d16282383ba073a"
	const wantSchema = "1ee29dc65db33ca78fd6cacb0b72dae6c2533bb3736a91d59eebe4dd5ea83b68"
	questions, err := jev.TraderQuestionsFor("trader-v3")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := buildPrompt(questions, trialwire.StateOf(jev.TraderRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := schemaFor(questions)
	if err != nil {
		t.Fatal(err)
	}
	if got := digest([]byte(prompt)); got != wantPrompt {
		t.Errorf("prompt digest = %s, want %s (protocol %s)", got, wantPrompt, protocolVersion)
	}
	if got := digest(schema); got != wantSchema {
		t.Errorf("schema digest = %s, want %s (protocol %s)", got, wantSchema, protocolVersion)
	}
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
