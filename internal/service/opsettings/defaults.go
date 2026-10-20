package opsettings

import (
	"context"
	"strconv"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/logsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/orderflowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/snapshotagesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

// defaultText is what applies to key while nothing is stored.
func (s *Service) defaultText(ctx context.Context, key string) string {
	switch {
	case key == config.KeyBackupDir:
		return ""
	case key == config.KeyLogDir:
		return s.defaultLogDir
	case kindOf(key) == kindPrivateKey || kindOf(key) == kindBroker:
		return config.BrokerSettingDefault(key)
	case kindOf(key) == kindSource:
		return tachibanasource.TachibanaSourceDefault(key)
	case kindOf(key) == kindEntryQuality:
		v, _ := config.PolicySettingValue(s.policy, key)
		return v.(string)
	case strings.HasPrefix(key, "policy."):
		v, _ := config.PolicySettingValue(s.policy, key)
		return formatNumber(v.(float64))
	case orderflowsettings.IsKey(key):
		seconds, _ := orderflowsettings.Default().Seconds(key)
		return strconv.Itoa(seconds)
	case snapshotagesettings.IsKey(key):
		return strconv.Itoa(snapshotagesettings.DefaultSeconds)
	case key == config.KeyLogLevel:
		return logsettings.DefaultLevelText
	case key == config.KeyLogRetentionDays:
		return strconv.Itoa(logsettings.DefaultRetentionDays)
	case dailyguardsettings.IsKey(key):
		return "0"
	case papersettings.IsKey(key):
		return s.paperDefaultText(key)
	case bucketsettings.IsKey(key):
		return s.bucketDefaultText(ctx, key)
	case barriersettings.IsKey(key):
		text, _ := barriersettings.Default().Text(key)
		return text
	case costsettings.IsKey(key):
		text, _ := costsettings.Default().Text(key)
		return text
	case metalabelsettings.IsKey(key):
		text, _ := metalabelsettings.Default().Text(key)
		return text
	case paperbootstrapsettings.IsKey(key):
		return paperbootstrapsettings.Text(paperbootstrapsettings.Default)
	case regimesettings.IsKey(key):
		text, _ := regimesettings.Default().Text(key)
		return text
	default:
		v, _ := config.FastScreenerSettingValue(s.screener, key)
		return formatNumber(v)
	}
}
