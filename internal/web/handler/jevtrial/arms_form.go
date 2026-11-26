package jevtrial

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	view "github.com/ousiassllc/pitha-trador/internal/web/organisms/jevtrial"
)

// The arm form posts its rows as the fields `arm-<n>-<field>` (n numbers the
// rows, field is one of the names below). An `arm-` field of any other name is
// refused, like an unknown key of the stored JSON.
const (
	fieldID             = "id"
	fieldProvider       = "provider"
	fieldModel          = "model"
	fieldPromptVersion  = "prompt_version"
	fieldDailyCap       = "daily_cost_cap_usd"
	fieldListPrice      = "list_price_usd_per_mtok"
	fieldListPriceOut   = "list_price_output_usd_per_mtok"
	fieldProviderOnly   = "provider_only"
	fieldDataCollection = "data_collection"
	fieldZDR            = "zdr"
	fieldEndpoint       = "endpoint"
)

// rowsError is a reason a posted row cannot be read, worded for the screen.
type rowsError string

func (e rowsError) Error() string { return string(e) }

// readRows collects the posted `arm-<n>-<field>` fields into rows, in the order
// of n. The values are kept as typed.
func readRows(c *gin.Context) ([]view.ArmField, error) {
	if err := c.Request.ParseForm(); err != nil {
		return nil, rowsError("フォームを読み取れませんでした")
	}
	byIndex := map[int]*view.ArmField{}
	for key, values := range c.Request.PostForm {
		rest, ok := strings.CutPrefix(key, "arm-")
		if !ok {
			continue
		}
		num, field, ok := strings.Cut(rest, "-")
		n, err := strconv.Atoi(num)
		if !ok || err != nil || n < 0 {
			return nil, rowsError("アームの項目名が正しくありません")
		}
		row := byIndex[n]
		if row == nil {
			row = &view.ArmField{}
			byIndex[n] = row
		}
		value := ""
		if len(values) > 0 {
			value = strings.TrimSpace(values[0])
		}
		if !setField(row, field, value) {
			return nil, rowsError("アームの項目名が正しくありません")
		}
	}
	order := make([]int, 0, len(byIndex))
	for n := range byIndex {
		order = append(order, n)
	}
	sort.Ints(order)
	rows := make([]view.ArmField, 0, len(order))
	for _, n := range order {
		rows = append(rows, *byIndex[n])
	}
	return rows, nil
}

// setField stores value in row's field; false for a name the form does not have.
func setField(row *view.ArmField, field, value string) bool {
	switch field {
	case fieldID:
		row.ID = value
	case fieldProvider:
		row.Provider = value
	case fieldModel:
		row.Model = value
	case fieldPromptVersion:
		row.PromptVersion = value
	case fieldDailyCap:
		row.DailyCap = value
	case fieldListPrice:
		row.ListPrice = value
	case fieldListPriceOut:
		row.ListPriceOutput = value
	case fieldProviderOnly:
		row.ProviderOnly = value
	case fieldDataCollection:
		row.DataCollection = value
	case fieldZDR:
		row.ZDR = value != ""
	case fieldEndpoint:
		row.Endpoint = value
	default:
		return false
	}
	return true
}

// buildArms turns the rows into the arms the service validates. It converts
// only the shape (numbers, the provider list); every rule (required fields,
// ranges, ids, at most 8, duplicates, the registered prompt versions) is the
// service's, the same as for the stored JSON. A number that is not a number is
// the one error it words itself.
func buildArms(rows []view.ArmField) ([]jevtrialsettings.Arm, error) {
	var arms []jevtrialsettings.Arm
	for i, r := range rows {
		label := "アーム " + strconv.Itoa(i+1)
		if r.ID != "" {
			label = "アーム " + strconv.Quote(r.ID)
		}
		capUSD, err := parseUSD(r.DailyCap)
		if err != nil {
			return nil, rowsError(label + " の 1 日の上限は数値（USD）で入力してください")
		}
		price, err := parseUSD(r.ListPrice)
		if err != nil {
			return nil, rowsError(label + " の定価は数値（USD）で入力してください")
		}
		priceOut, err := parseUSD(r.ListPriceOutput)
		if err != nil {
			return nil, rowsError(label + " の出力の定価は数値（USD）で入力してください")
		}
		dataCollection := r.DataCollection
		if r.Provider != jevtrialsettings.ProviderOpenRouter && dataCollection == jevtrialsettings.DataCollectionDeny {
			// The select always sends a value; its default is not a choice made for
			// a provider the OpenRouter routing preferences do not apply to.
			dataCollection = ""
		}
		arms = append(arms, jevtrialsettings.Arm{
			ID: r.ID, Provider: r.Provider, Model: r.Model, PromptVersion: r.PromptVersion,
			DailyCostCapUSD: capUSD, ListPriceUSDPerMtok: price, ListPriceOutputUSDPerMtok: priceOut,
			ProviderOnly: splitProviders(r.ProviderOnly), DataCollection: dataCollection, ZDR: r.ZDR, Endpoint: r.Endpoint,
		})
	}
	return arms, nil
}

// parseUSD reads an optional number: blank is 0 (not set).
func parseUSD(text string) (float64, error) {
	if text == "" {
		return 0, nil
	}
	return strconv.ParseFloat(text, 64)
}

// splitProviders splits "a, b" on commas and spaces; blank is none.
func splitProviders(text string) []string {
	names := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '、' || unicode.IsSpace(r) })
	if len(names) == 0 {
		return nil
	}
	return names
}

// AddArm implements `POST /settings/jev-trial/arms/add`: the rows of the form
// plus one blank row, as the editor fragment. Nothing is saved.
func (h *Handler) AddArm(c *gin.Context) {
	rows, err := readRows(c)
	if err != nil {
		shared.RespondFormError(c, formError, err.Error()+"。")
		return
	}
	if len(rows) >= jevtrialsettings.MaxArms {
		shared.RespondFormError(c, formError, "アームは最大 "+strconv.Itoa(jevtrialsettings.MaxArms)+" 件までです。")
		return
	}
	rows = append(rows, view.ArmField{Provider: jevtrialsettings.ProviderOpenRouter, DataCollection: jevtrialsettings.DataCollectionDeny})
	renderEditor(c, rows)
}

// RemoveArm implements `POST /settings/jev-trial/arms/remove?index=<n>`: the
// rows of the form without the n-th one (counted from 0), as the editor
// fragment. Nothing is saved.
func (h *Handler) RemoveArm(c *gin.Context) {
	rows, err := readRows(c)
	if err != nil {
		shared.RespondFormError(c, formError, err.Error()+"。")
		return
	}
	n, convErr := strconv.Atoi(c.Query("index"))
	if convErr != nil || n < 0 || n >= len(rows) {
		shared.RespondFormError(c, formError, "削除するアームが見つかりません。")
		return
	}
	renderEditor(c, append(rows[:n], rows[n+1:]...))
}

func renderEditor(c *gin.Context, rows []view.ArmField) {
	c.Header("Cache-Control", "no-store")
	shared.RenderHTML(c, http.StatusOK, view.ArmEditor(editorProps(rows)))
}

func editorProps(rows []view.ArmField) view.ArmEditorProps {
	return view.ArmEditorProps{Fields: rows, PromptVersions: jevtrial.PromptVersions(), MaxArms: jevtrialsettings.MaxArms}
}

// storedRows shows the saved arms as form rows.
func storedRows(st jevtrial.State) []view.ArmField {
	rows := make([]view.ArmField, 0, len(st.Arms))
	for _, a := range st.Arms {
		arm := a.Arm
		rows = append(rows, view.ArmField{
			ID: arm.ID, Provider: arm.Provider, Model: arm.Model, PromptVersion: arm.PromptVersion,
			DailyCap: number(arm.DailyCostCapUSD), ListPrice: number(arm.ListPriceUSDPerMtok), ListPriceOutput: number(arm.ListPriceOutputUSDPerMtok),
			ProviderOnly: strings.Join(arm.ProviderOnly, ", "), DataCollection: arm.DataCollectionOrDefault(), ZDR: arm.ZDR, Endpoint: arm.Endpoint,
		})
	}
	return rows
}

// number formats a stored number for its field; 0 (not set) is blank.
func number(v float64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
