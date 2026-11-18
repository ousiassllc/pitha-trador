package airole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
)

// MaxModelLen bounds a model name.
const MaxModelLen = 100

// Setting is one role's provider and model. The zero Setting is invalid; the
// default is DefaultSetting.
type Setting struct {
	Provider string `json:"provider"`
	// Model is the model name. Empty means "not specified": the provider's own
	// default applies (Jev's JEV_MODEL for the custom provider); OpenRouter
	// needs one.
	Model string `json:"model,omitempty"`
}

// DefaultSetting is what applies while nothing is stored: the custom provider
// without a model, which is the connection every role had before issue #1080.
func DefaultSetting() Setting { return Setting{Provider: ProviderCustom} }

// InvalidError is a rejected setting; Message is a fixed Japanese sentence
// safe to show to the operator.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return "airole: " + e.Message }

// Normalize trims the model and validates the setting for role. It returns the
// value to store.
func Normalize(role string, s Setting) (Setting, error) {
	s.Model = strings.TrimSpace(s.Model)
	switch {
	case !IsRole(role):
		return Setting{}, &InvalidError{"役割が正しくありません。"}
	case !IsProvider(s.Provider):
		return Setting{}, &InvalidError{"プロバイダーは「独自 API」か「OpenRouter」を選んでください。"}
	case len(s.Model) > MaxModelLen:
		return Setting{}, &InvalidError{"モデル名は 100 文字以内で入力してください。"}
	case strings.ContainsFunc(s.Model, unicode.IsControl) || strings.ContainsFunc(s.Model, unicode.IsSpace):
		return Setting{}, &InvalidError{"モデル名に空白・改行・制御文字は使えません。"}
	case s.Provider == ProviderOpenRouter && s.Model == "":
		return Setting{}, &InvalidError{"OpenRouter を選ぶ役割にはモデル名（例: provider/model）が必要です。"}
	case s.Provider == ProviderCustom && s.Model != "" && role != RoleJev:
		return Setting{}, &InvalidError{"独自 API のモデル名を指定できるのは Jev だけです。ほかの役割は、独自 API ではその接続先のモデルが使われます。"}
	}
	return s, nil
}

// Parse decodes and validates the runtime_settings value of Key(role). Unknown
// fields are refused so a misspelt key is not silently dropped.
func Parse(role, jsonValue string) (Setting, error) {
	var s Setting
	dec := json.NewDecoder(strings.NewReader(jsonValue))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Setting{}, errors.New("airole: decode " + Key(role) + " as a JSON object with provider and model")
	}
	if _, err := dec.Token(); err != io.EOF {
		return Setting{}, errors.New("airole: " + Key(role) + " must hold exactly one JSON object")
	}
	return Normalize(role, s)
}

// Encode renders the stored JSON of a validated setting.
func Encode(role string, s Setting) (string, error) {
	s, err := Normalize(role, s)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(s); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// Getter is the runtime_settings read Load needs;
// *system.RuntimeSettingsRepository implements it.
type Getter interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Loaded is every role's effective setting.
type Loaded struct {
	// Settings holds each role's effective setting: the stored one, or
	// DefaultSetting when nothing is stored or the stored value is unusable.
	Settings map[string]Setting
	// Stored is true for a role whose stored value is in force.
	Stored map[string]bool
	// Damaged says why a role's stored value is ignored (the role then runs on
	// the default); the entry is absent when it is fine.
	Damaged map[string]string
}

// Load reads every role's setting. It never fails: a value that cannot be
// read or parsed falls back to the default (the connection the role had
// before this layer), and Damaged tells why so the screen can show it.
func Load(ctx context.Context, g Getter) Loaded {
	out := Loaded{Settings: map[string]Setting{}, Stored: map[string]bool{}, Damaged: map[string]string{}}
	for _, role := range Roles() {
		out.Settings[role] = DefaultSetting()
		raw, ok, err := g.Get(ctx, Key(role))
		if err != nil {
			out.Damaged[role] = "保存した値を読み込めませんでした。"
			continue
		}
		if !ok {
			continue
		}
		s, err := Parse(role, raw)
		if err != nil {
			out.Damaged[role] = "保存した値が正しくないため、既定を使っています。"
			continue
		}
		out.Settings[role], out.Stored[role] = s, true
	}
	return out
}
