package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// runtime_settings keys of the broker selection and of the 立花証券 e支店API
// adapter (issue #723, #733; docs/architecture/er/tables-system.md
// §runtime_settings). Values are JSON scalars; a missing row means the
// default below applies. The 立花 credentials themselves (認証ID, デモの第二暗証番号)
// are `secrets` rows (KeyTachibana*), and the 秘密鍵 is never stored in the
// database: only its file path is (the `secrets` encryption key is embedded in
// the binary, so a PEM stored there would be no real protection).
const (
	// KeyBrokerProvider selects the broker adapter: BrokerKabu (default) or
	// BrokerTachibana. Read once at process start.
	KeyBrokerProvider = "broker.provider"
	// KeyTachibanaEnvironment is TachibanaEnvDemo (default) or
	// TachibanaEnvProduction.
	KeyTachibanaEnvironment = "broker.tachibana.environment"
	// KeyTachibanaDemoBaseURL / KeyTachibanaProdBaseURL override the API base
	// URL per environment (to follow a version-prefix change); https only,
	// stored with a trailing "/".
	KeyTachibanaDemoBaseURL = "broker.tachibana.demo.base_url"
	KeyTachibanaProdBaseURL = "broker.tachibana.production.base_url"
	// KeyTachibanaDemoPrivateKeyPath / KeyTachibanaProdPrivateKeyPath are the
	// absolute paths of the RSA private key (PEM) of each environment.
	KeyTachibanaDemoPrivateKeyPath = "broker.tachibana.demo.private_key_path"
	KeyTachibanaProdPrivateKeyPath = "broker.tachibana.production.private_key_path"
	// KeyTachibanaRequestMaxPerSecond caps the adapter's REST requests per
	// second (TachibanaRequestMaxPerSecondMin..Max).
	KeyTachibanaRequestMaxPerSecond = "broker.tachibana.request_max_per_second"
	// KeyTachibanaReauthTime is the daily re-login time of day ("HH:MM", JST)
	// within TachibanaReauthTimeMin..Max, after the broker's 05:30 reset.
	KeyTachibanaReauthTime = "broker.tachibana.reauth_time"
)

// Values of KeyBrokerProvider and KeyTachibanaEnvironment.
const (
	BrokerKabu      = "kabu"
	BrokerTachibana = "tachibana"

	TachibanaEnvDemo       = "demo"
	TachibanaEnvProduction = "production"
)

// Defaults and bounds of the broker settings. The request rate default is
// provisional until the live measurement of issue #725.
const (
	DefaultTachibanaDemoBaseURL = "https://demo-kabuka.e-shiten.jp/e_api_v4r10/"
	DefaultTachibanaProdBaseURL = "https://kabuka.e-shiten.jp/e_api_v4r10/"

	DefaultTachibanaRequestMaxPerSecond = 3
	TachibanaRequestMaxPerSecondMin     = 1
	TachibanaRequestMaxPerSecondMax     = 10

	DefaultTachibanaReauthTime = "05:35"
	TachibanaReauthTimeMin     = "05:30"
	TachibanaReauthTimeMax     = "08:00"
)

var brokerSettingKeys = []string{
	KeyBrokerProvider, KeyTachibanaEnvironment,
	KeyTachibanaDemoBaseURL, KeyTachibanaProdBaseURL,
	KeyTachibanaDemoPrivateKeyPath, KeyTachibanaProdPrivateKeyPath,
	KeyTachibanaRequestMaxPerSecond, KeyTachibanaReauthTime,
}

// BrokerSettingKeys returns every broker runtime_settings key, in display order.
func BrokerSettingKeys() []string { return slices.Clone(brokerSettingKeys) }

// IsBrokerSettingKey reports whether key is one of BrokerSettingKeys.
func IsBrokerSettingKey(key string) bool { return slices.Contains(brokerSettingKeys, key) }

// IsPrivateKeyPathKey reports whether key holds a 秘密鍵 file path.
func IsPrivateKeyPathKey(key string) bool {
	return key == KeyTachibanaDemoPrivateKeyPath || key == KeyTachibanaProdPrivateKeyPath
}

// BrokerSettingDefault is the text that applies to key while nothing is
// stored ("" for the key paths: there is no default key).
func BrokerSettingDefault(key string) string {
	switch key {
	case KeyBrokerProvider:
		return BrokerKabu
	case KeyTachibanaEnvironment:
		return TachibanaEnvDemo
	case KeyTachibanaDemoBaseURL:
		return DefaultTachibanaDemoBaseURL
	case KeyTachibanaProdBaseURL:
		return DefaultTachibanaProdBaseURL
	case KeyTachibanaRequestMaxPerSecond:
		return strconv.Itoa(DefaultTachibanaRequestMaxPerSecond)
	case KeyTachibanaReauthTime:
		return DefaultTachibanaReauthTime
	}
	return ""
}

// TachibanaSettings are the effective 立花 adapter settings.
type TachibanaSettings struct {
	Environment                            string
	DemoBaseURL, ProdBaseURL               string
	DemoPrivateKeyPath, ProdPrivateKeyPath string
	RequestMaxPerSecond                    int
	ReauthTime                             string
}

// Production reports whether the 本番 environment is selected.
func (t TachibanaSettings) Production() bool { return t.Environment == TachibanaEnvProduction }

// BaseURL is the selected environment's API base URL.
func (t TachibanaSettings) BaseURL() string {
	if t.Production() {
		return t.ProdBaseURL
	}
	return t.DemoBaseURL
}

// PrivateKeyPath is the selected environment's 秘密鍵 path ("" = not set).
func (t TachibanaSettings) PrivateKeyPath() string {
	if t.Production() {
		return t.ProdPrivateKeyPath
	}
	return t.DemoPrivateKeyPath
}

// BrokerSettings are the effective broker selection and adapter settings.
type BrokerSettings struct {
	Provider  string
	Tachibana TachibanaSettings
}

// SetupRequirements are what must be stored before the app is usable: the
// `secrets` keys and the runtime_settings keys (SettingKeys, whose value is a
// path that must be set). Which ones depend on the selected broker.
type SetupRequirements struct {
	SecretKeys  []string
	SettingKeys []string
}

// RequiredSetup returns what the broker b needs (issue #734). kabu: Jev API
// key and kabuステーション API password. tachibana: Jev API key plus the
// selected environment's 認証ID and 秘密鍵 path; KABU_API_PASSWORD is optional
// then (only the fallback broker uses it).
func RequiredSetup(b BrokerSettings) SetupRequirements {
	if b.Provider != BrokerTachibana {
		return SetupRequirements{SecretKeys: []string{KeyJevAPIKey, KeyKabuAPIPassword}}
	}
	if b.Tachibana.Production() {
		return SetupRequirements{SecretKeys: []string{KeyJevAPIKey, KeyTachibanaProdAuthID}, SettingKeys: []string{KeyTachibanaProdPrivateKeyPath}}
	}
	return SetupRequirements{SecretKeys: []string{KeyJevAPIKey, KeyTachibanaDemoAuthID}, SettingKeys: []string{KeyTachibanaDemoPrivateKeyPath}}
}

var reauthTimePattern = regexp.MustCompile(`^\d{1,2}:\d{2}$`)

// NormalizeBrokerSetting validates raw (trimmed, non-empty) for the broker
// setting key (not a private key path: that is a path, checked by the caller)
// and returns the value to store as a JSON scalar: a string, or an int for
// KeyTachibanaRequestMaxPerSecond. The error message is safe to show.
func NormalizeBrokerSetting(key, raw string) (any, error) {
	switch key {
	case KeyBrokerProvider:
		return oneOf(raw, BrokerKabu, BrokerTachibana)
	case KeyTachibanaEnvironment:
		return oneOf(raw, TachibanaEnvDemo, TachibanaEnvProduction)
	case KeyTachibanaDemoBaseURL, KeyTachibanaProdBaseURL:
		return normalizeBaseURL(raw)
	case KeyTachibanaRequestMaxPerSecond:
		n, err := strconv.Atoi(raw)
		if err != nil || n < TachibanaRequestMaxPerSecondMin || n > TachibanaRequestMaxPerSecondMax {
			return nil, fmt.Errorf("%d〜%d の整数で指定してください", TachibanaRequestMaxPerSecondMin, TachibanaRequestMaxPerSecondMax)
		}
		return n, nil
	case KeyTachibanaReauthTime:
		return normalizeReauthTime(raw)
	}
	return nil, fmt.Errorf("config: %q is not a broker setting", key)
}

func oneOf(raw string, allowed ...string) (string, error) {
	v := strings.ToLower(raw)
	if !slices.Contains(allowed, v) {
		return "", fmt.Errorf("%s のいずれかを指定してください", strings.Join(allowed, " / "))
	}
	return v, nil
}

// normalizeBaseURL requires an https URL with a host and no credentials,
// query or fragment, and returns it with exactly one trailing "/".
func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", errors.New("https:// で始まるホスト名付きの URL を指定してください")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", errors.New("URL に認証情報・クエリ・フラグメントは含められません")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	u.RawPath = ""
	return u.String(), nil
}

// normalizeReauthTime accepts "H:MM"/"HH:MM" within the allowed window and
// returns it as zero-padded "HH:MM".
func normalizeReauthTime(raw string) (string, error) {
	window := fmt.Sprintf("%s〜%s の範囲で指定してください", TachibanaReauthTimeMin, TachibanaReauthTimeMax)
	if !reauthTimePattern.MatchString(raw) {
		return "", fmt.Errorf("HH:MM 形式で指定してください（%s）", window)
	}
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return "", fmt.Errorf("HH:MM 形式で指定してください（%s）", window)
	}
	v := t.Format("15:04")
	if v < TachibanaReauthTimeMin || v > TachibanaReauthTimeMax {
		return "", errors.New(window)
	}
	return v, nil
}
