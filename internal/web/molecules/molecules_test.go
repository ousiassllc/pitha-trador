package molecules_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return sb.String()
}

// Symbol Detail shows the same rounded confidence as the Scanner SSR table
// (issue #677): 0.125 is exactly representable, so %.0f would give 12%.
func TestSignalBadgeGroup_ConfidenceRoundsHalfAwayFromZero(t *testing.T) {
	direction, confidence := "LONG", 0.125
	html := render(t, molecules.SignalBadgeGroup(&direction, &confidence, nil))
	if !strings.Contains(html, ">13%<") || strings.Contains(html, "12%") {
		t.Errorf("confidence 0.125 must render 13%%: %s", html)
	}
}

func TestSignalBadgeGroup_PendingAndMissingConfidence(t *testing.T) {
	if html := render(t, molecules.SignalBadgeGroup(nil, nil, nil)); !strings.Contains(html, "判定待ち") {
		t.Errorf("nil direction must render pending: %s", html)
	}
	direction := "LONG"
	if html := render(t, molecules.SignalBadgeGroup(&direction, nil, nil)); !strings.Contains(html, ">—<") {
		t.Errorf("nil confidence must render a dash: %s", html)
	}
}

// htmx restores focus after a swap only to a same-id element (issue #676),
// so the row's buttons carry stable ids; the delete button, which vanishes
// once the value is gone, hands focus back to the input.
func TestSecretFieldRow_ButtonsHaveStableIDs(t *testing.T) {
	configured := render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "jquants_api_key", Label: "J-Quants", Configured: true}))
	for _, want := range []string{`id="save-jquants_api_key"`, `id="delete-jquants_api_key"`, `data-focus-after-swap="secret-input-jquants_api_key"`, `id="secret-input-jquants_api_key"`} {
		if !strings.Contains(configured, want) {
			t.Errorf("configured row missing %q: %s", want, configured)
		}
	}
	unconfigured := render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "jquants_api_key", Label: "J-Quants"}))
	if !strings.Contains(unconfigured, `id="save-jquants_api_key"`) || strings.Contains(unconfigured, "delete-jquants_api_key") {
		t.Errorf("unconfigured row must keep the save id and have no delete button: %s", unconfigured)
	}
}

// Closing a position swaps its row (outerHTML) for a closed row that has no
// Close button, so the focus would fall to <body> (issue #681): the button
// has a stable id and hands focus to the replaced row, whose id the
// response fragment also renders.
func TestPositionRow_CloseButtonHandsFocusToReplacedRow(t *testing.T) {
	now := time.Now()
	pnl, reason := 12.5, domain.ExitReasonManual
	open := domain.Position{ID: 7, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, OpenedAt: now}
	closed := open
	closed.RealizedPnL, closed.ClosedAt, closed.ExitReason = &pnl, &now, &reason

	openHTML := render(t, molecules.PositionRow(open))
	for _, want := range []string{`id="close-position-7"`, `data-focus-after-swap="position-row-7"`, `hx-target="#position-row-7"`} {
		if !strings.Contains(openHTML, want) {
			t.Errorf("open row missing %q: %s", want, openHTML)
		}
	}
	closedHTML := render(t, molecules.PositionRow(closed))
	if !strings.Contains(closedHTML, `id="position-row-7"`) {
		t.Errorf("closed row must keep the focus target id: %s", closedHTML)
	}
	if strings.Contains(closedHTML, "close-position-7") {
		t.Errorf("closed row must not render the Close button: %s", closedHTML)
	}
}

// The 計算結果 panel is announced politely when an out-of-band swap replaces it
// (issue #782), keeps its id and test id, and a line without a value renders
// no empty value span.
func TestSettingGroupDerived_AnnouncesUpdatesAndSkipsEmptyValue(t *testing.T) {
	props := molecules.SettingGroupProps{ID: "g", Derived: []molecules.SettingDerivedItem{
		{Key: "error", Label: "読み取れませんでした", Off: true},
		{Key: "ok", Label: "上限", Value: "5,000 円"},
	}}
	for _, oob := range []bool{false, true} {
		html := render(t, molecules.SettingGroupDerived(props, oob))
		for _, want := range []string{`id="setting-group-derived-g"`, `data-testid="setting-group-derived-g"`, `aria-live="polite"`, `aria-label="計算結果"`, "読み取れませんでした", ">5,000 円<"} {
			if !strings.Contains(html, want) {
				t.Errorf("oob=%v: missing %q: %s", oob, want, html)
			}
		}
		if strings.Contains(html, "></span>") {
			t.Errorf("oob=%v: an empty value span is rendered: %s", oob, html)
		}
		if got := strings.Contains(html, `hx-swap-oob="true"`); got != oob {
			t.Errorf("hx-swap-oob present = %v, want %v", got, oob)
		}
	}
}

// Warning lines are amber with a 注意: prefix and headings group the lines
// below them; both escape their text.
func TestSettingGroupDerived_RendersWarningsAndHeadings(t *testing.T) {
	props := molecules.SettingGroupProps{ID: "g", Derived: []molecules.SettingDerivedItem{
		{Key: "h", Heading: true, Label: "整合チェック", Detail: "表示のみ"},
		{Key: "w", Warn: true, Label: "<script>alert(1)</script> が異なります"},
	}}
	html := render(t, molecules.SettingGroupDerived(props, false))
	for _, want := range []string{"整合チェック", "表示のみ", "text-warning", "注意:", "&lt;script&gt;alert(1)&lt;/script&gt; が異なります"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "<script>") {
		t.Errorf("warning text is not escaped: %s", html)
	}
}

// A group may title its read-only panel (issue #809); without a title it stays
// 計算結果.
func TestSettingGroupDerived_UsesTheGroupsTitle(t *testing.T) {
	props := molecules.SettingGroupProps{ID: "g", DerivedTitle: "LLM cutoff の状態と確認手順", Derived: []molecules.SettingDerivedItem{{Key: "a", Label: "状態"}}}
	html := render(t, molecules.SettingGroupDerived(props, false))
	for _, want := range []string{`aria-label="LLM cutoff の状態と確認手順"`, ">LLM cutoff の状態と確認手順<"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "計算結果") {
		t.Errorf("the default title leaks into a titled panel: %s", html)
	}
}

// "注意:" is a line-leading label, not a heading: Regular, told apart by the
// warning colour (brand-guide §2: SemiBold is for headings and buttons only).
func TestSettingGroupDerived_WarningLabelIsRegular(t *testing.T) {
	props := molecules.SettingGroupProps{ID: "g", Derived: []molecules.SettingDerivedItem{{Key: "w", Label: "x が異なります", Warn: true}}}
	html := render(t, molecules.SettingGroupDerived(props, false))
	_, row, _ := strings.Cut(html, "<ul")
	if !strings.Contains(row, "<span>注意:</span>") || strings.Contains(row, "font-semibold") {
		t.Errorf("the 注意: label must be Regular: %s", row)
	}
}

// A row's label sits 8 above its field (brand-guide §3: label→input 6〜8).
func TestFieldRows_LabelIsEightAboveTheField(t *testing.T) {
	for name, html := range map[string]string{
		"setting": render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "k", Label: "L"})),
		"secret":  render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "k", Label: "L"})),
	} {
		if !strings.Contains(html, `class="flex flex-col gap-2 text-sm text-ink"`) {
			t.Errorf("%s row must be gap-2 between label and field: %s", name, html)
		}
	}
}

// issue #972: the form must not be --mute, or a typed value and the field
// name inherit it (brand-guide §1: --mute is for secondary text only).
func TestFieldRows_FormIsNotMute(t *testing.T) {
	for name, html := range map[string]string{
		"setting": render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "k", Label: "L"})),
		"secret":  render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "k", Label: "L"})),
	} {
		form, _, _ := strings.Cut(html, ">")
		if strings.Contains(form, "text-mute") {
			t.Errorf("%s row form must not be text-mute: %s", name, form)
		}
	}
}

// issue #961: a rejected value is shown as an inline alert in the row's own
// error slot (design guide O3), so both field rows carry that slot.
func TestFieldRows_HaveAnErrorSlotForValidation(t *testing.T) {
	secret := render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "JEV_API_KEY", Label: "L"}))
	if want := `id="` + molecules.SecretErrorID("JEV_API_KEY") + `"`; !strings.Contains(secret, want) {
		t.Errorf("SecretFieldRow lacks the error slot %s: %s", want, secret)
	}
	setting := render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "screener.min_price", Label: "L"}))
	if want := `id="` + molecules.SettingErrorID("screener.min_price") + `"`; !strings.Contains(setting, want) {
		t.Errorf("SettingFieldRow lacks the error slot %s: %s", want, setting)
	}
}

// The hint under an input sits 4 below it (brand-guide §3: input→hint 4), and
// the empty validation slot takes no flex gap of its own.
func TestFieldRows_HintSitsFourBelowTheInputAndEmptyErrorSlotTakesNoGap(t *testing.T) {
	for name, html := range map[string]string{
		"setting": render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "k", Label: "L", DefaultLabel: "1"})),
		"secret":  render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "k", Label: "L", Notice: "保存しました"})),
	} {
		if !strings.Contains(html, `<div class="flex flex-col gap-1">`) {
			t.Errorf("%s row must group the input row and its hint with gap-1: %s", name, html)
		}
		if !strings.Contains(html, `class="empty:hidden" data-testid="error-k"`) {
			t.Errorf("%s row's error slot must be empty:hidden: %s", name, html)
		}
	}
}

// issue #987: the 計算結果 panel nests inside a card, so it is a --bone panel on --r-md
// (brand-guide S1/§4) with the small-card padding (16).
func TestSettingGroupDerived_IsANestedMediumRadiusPanel(t *testing.T) {
	html := render(t, molecules.SettingGroupDerived(molecules.SettingGroupProps{ID: "g"}, false))
	if !strings.Contains(html, "rounded-md border border-border bg-bone p-4") || strings.Contains(html, "rounded-lg") {
		t.Errorf("the derived panel must be rounded-md p-4: %s", html)
	}
}

// The theme switch (issue #978) offers follow-the-OS, light and dark as
// buttons the theme-toggle script reads by `data-theme-choice`; 自動 starts
// pressed, and the control is hidden when printing.
func TestThemeToggle_OffersAutoLightDarkWithAutoPressed(t *testing.T) {
	html := render(t, molecules.ThemeToggle())
	for _, want := range []string{
		`role="group"`,
		`aria-label="テーマ"`,
		`print:hidden`,
		`aria-pressed="true" data-theme-choice="auto"`,
		`aria-pressed="false" data-theme-choice="light"`,
		`aria-pressed="false" data-theme-choice="dark"`,
		`>自動<`,
		`>ライト<`,
		`>ダーク<`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("ThemeToggle lacks %q: %s", want, html)
		}
	}
	if got := strings.Count(html, "<button"); got != 3 {
		t.Errorf("ThemeToggle renders %d buttons, want 3", got)
	}
}

// A settings row's title is neither a heading nor a button, so it is Regular
// (brand-guide §2 T2: SemiBold is for headings and buttons only; issue #986).
func TestSettingsCards_NameIsRegular(t *testing.T) {
	card := render(t, molecules.SettingsCard(molecules.SettingsCardProps{ID: "c", ModalID: "m", Name: "接続名", ActionLabel: "設定"}))
	link := render(t, molecules.SettingsLinkCard(molecules.SettingsLinkCardProps{ID: "l", Name: "画面名", Href: "/x", ActionLabel: "開く"}))
	for name, html := range map[string]string{"SettingsCard": card, "SettingsLinkCard": link} {
		title, _, _ := strings.Cut(html, "</li>")
		if i := strings.Index(title, "<button"); i >= 0 {
			title = title[:i]
		}
		if i := strings.Index(title, "<a "); i >= 0 {
			title = title[:i]
		}
		if strings.Contains(title, "font-semibold") {
			t.Errorf("%s: the title must be Regular: %s", name, title)
		}
	}
}

// The focus outline appears at once (brand-guide M1/§5), so the hover/pressed
// transition covers text and background colour only, not outline-color, which
// `transition-colors` would add in Tailwind v4 (issue #988).
func TestThemeToggle_TransitionExcludesFocusOutline(t *testing.T) {
	html := render(t, molecules.ThemeToggle())
	if !strings.Contains(html, "transition-[color,background-color] duration-base ease-out") {
		t.Errorf("ThemeToggle must transition only color and background-color: %s", html)
	}
	if strings.Contains(html, "transition-colors") {
		t.Errorf("transition-colors also animates outline-color: %s", html)
	}
}

// issue #989 (C6): a connection card's description carries what the operator
// must do, so it is body text (text-ink), not an auxiliary caption (text-mute).
func TestSettingsCard_DescriptionIsInk(t *testing.T) {
	html := render(t, molecules.SettingsCard(molecules.SettingsCardProps{ID: "c", ModalID: "m", Name: "接続名", Description: "本番の第二暗証番号は保存できません", ActionLabel: "設定"}))
	if !strings.Contains(html, `<p class="text-sm text-ink">本番の第二暗証番号は保存できません</p>`) {
		t.Errorf("description must be text-sm text-ink: %s", html)
	}
	if strings.Contains(html, "text-mute") {
		t.Errorf("description must not be text-mute: %s", html)
	}
}

// issue #990 (T3): the save/delete outcome is a notice, so its body is text-sm.
func TestSecretFieldRow_NoticeIsBodySize(t *testing.T) {
	html := render(t, molecules.SecretFieldRow(molecules.SecretFieldRowProps{Key: "k", Label: "L", Notice: "保存しました"}))
	if !strings.Contains(html, `<p class="text-sm text-success" role="status" data-testid="notice-k">`) {
		t.Errorf("notice must be text-sm: %s", html)
	}
}

// The panel title is a heading (h3 under the modal's h2, text-xl) and a list
// item's label is Regular, told apart by size and colour (brand-guide T2;
// issue #1005).
func TestSettingGroupDerived_TitleIsAHeadingAndItemLabelIsRegular(t *testing.T) {
	props := molecules.SettingGroupProps{ID: "g", Derived: []molecules.SettingDerivedItem{{Key: "h", Heading: true, Label: "整合チェック"}}}
	html := render(t, molecules.SettingGroupDerived(props, false))
	for _, want := range []string{`<h3 class="text-xl font-semibold text-ink">計算結果</h3>`, `<span class="text-base text-ink">整合チェック</span>`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, `<p class="font-semibold`) || strings.Contains(html, `<span class="font-semibold`) {
		t.Errorf("body text must not be SemiBold: %s", html)
	}
}

// issue #1006 (T3): "N/M 項目" is a count the operator reads, so it is text-sm
// text-mute; only the status pills beside it stay at 12px.
func TestSettingCounts_AreValueLinesNotNotes(t *testing.T) {
	connection := render(t, molecules.ConnectionStatus(molecules.ConnectionProps{ID: "c"}, false))
	group := render(t, molecules.SettingGroupStatus(molecules.SettingGroupProps{ID: "g"}, false))
	for name, tc := range map[string]struct{ html, want string }{
		"ConnectionStatus":   {connection, `<span class="text-sm text-mute">0/0 項目</span>`},
		"SettingGroupStatus": {group, `<span class="text-sm font-normal text-mute">0/0 項目</span>`},
	} {
		if !strings.Contains(tc.html, tc.want) {
			t.Errorf("%s: the item count must be text-sm: %s", name, tc.html)
		}
	}
}
