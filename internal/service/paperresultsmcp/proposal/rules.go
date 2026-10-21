package proposal

import (
	"math"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// checkNumbers rejects a present number that is not finite and positive.
func checkNumbers(a Args) error {
	if a.Quantity != nil && *a.Quantity < 1 {
		return invalid("quantity は 1 以上の整数で指定してください")
	}
	for _, f := range []struct {
		name string
		v    *float64
	}{
		{"budget_jpy", a.BudgetJPY}, {"limit_price", a.LimitPrice}, {"stop_loss_price", a.StopLossPrice},
		{"take_profit_price", a.TakeProfitPrice}, {"max_loss_jpy", a.MaxLossJPY},
	} {
		if f.v != nil && (math.IsNaN(*f.v) || math.IsInf(*f.v, 0) || *f.v <= 0) {
			return invalid("%s は 0 より大きい有限の数で指定してください", f.name)
		}
	}
	return nil
}

// checkOrderShape applies the rules that tie the arguments to the action and
// the order type. A close takes no size, stop, target or loss estimate: it
// closes the whole position the application finds.
func checkOrderShape(a Args) error {
	switch a.OrderType {
	case OrderLimit:
		if a.LimitPrice == nil {
			return invalid("limit_price は order_type が LIMIT のとき必須です")
		}
	case OrderMarket:
		if a.LimitPrice != nil {
			return invalid("limit_price は order_type が LIMIT のときだけ指定できます")
		}
	}
	if a.Action == ActionClose {
		switch {
		case a.OrderType != OrderMarket:
			return invalid("order_type は action が close のとき MARKET だけ指定できます")
		case a.Quantity != nil, a.BudgetJPY != nil:
			return invalid("quantity と budget_jpy は open_long のときだけ指定できます")
		case a.StopLossPrice != nil, a.TakeProfitPrice != nil, a.MaxLossJPY != nil:
			return invalid("stop_loss_price・take_profit_price・max_loss_jpy は open_long のときだけ指定できます")
		}
		return nil
	}
	if (a.Quantity == nil) == (a.BudgetJPY == nil) {
		return invalid("open_long では quantity か budget_jpy のどちらか 1 つだけを指定してください")
	}
	switch {
	case a.StopLossPrice == nil:
		return invalid("stop_loss_price は open_long のとき必須です")
	case a.MaxLossJPY == nil:
		return invalid("max_loss_jpy は open_long のとき必須です")
	case a.LimitPrice != nil && *a.StopLossPrice >= *a.LimitPrice:
		return invalid("stop_loss_price は limit_price より小さくしてください（買いの損切り）")
	}
	return nil
}

// text validates a free-text argument: no control character (a newline
// included), at most maxRunes characters, and non-empty when required. It
// returns the text with surrounding spaces trimmed.
func text(name, raw string, maxRunes int, required bool) (string, error) {
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 || !utf8.ValidString(raw) {
		return "", invalid("%s に制御文字（改行・タブなど）は使えません", name)
	}
	s := strings.TrimSpace(raw)
	if s == "" && required {
		return "", invalid("%s は必須です", name)
	}
	if utf8.RuneCountInString(s) > maxRunes {
		return "", invalid("%s は %d 文字以内で指定してください", name, maxRunes)
	}
	return s, nil
}

func sources(in []Source) ([]Source, error) {
	if len(in) < MinSources || len(in) > MaxSources {
		return nil, invalid("sources は %d〜%d 件で指定してください", MinSources, MaxSources)
	}
	out := make([]Source, len(in))
	for i, s := range in {
		title, err := text("sources.title", s.Title, MaxTitleRunes, true)
		if err != nil {
			return nil, err
		}
		link, err := text("sources.url", s.URL, MaxURLRunes, true)
		if err != nil {
			return nil, err
		}
		if u, perr := url.Parse(link); perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, invalid("sources.url は http または https の URL で指定してください")
		}
		at, err := time.Parse(time.RFC3339, s.RetrievedAt)
		if err != nil {
			return nil, invalid("sources.retrieved_at は RFC3339 の時刻で指定してください")
		}
		out[i] = Source{Title: title, URL: link, RetrievedAt: at.UTC().Format(time.RFC3339)}
	}
	return out, nil
}

// CheckExpiry requires expires_at to be after now (the receipt time) and at
// most MaxExpiry later.
func (r Request) CheckExpiry(now time.Time) error {
	switch {
	case !r.ExpiresAt.After(now):
		return invalid("expires_at は受け付け時点より後にしてください")
	case r.ExpiresAt.After(now.Add(MaxExpiry)):
		return invalid("expires_at は受け付け時点から 24 時間以内にしてください")
	}
	return nil
}
