package home

import (
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/homepanels"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// settingsLink is a link to a Settings page, optionally to one of its dialogs
// (modal is the `<dialog>` id, "" for the page itself); label names the
// destination the way the Settings menu does.
func settingsLink(pageID, modal, label string) homepanels.Link {
	href := settingspages.BasePath + "/" + pageID
	if modal != "" {
		href += "#" + modal
	}
	return homepanels.Link{Href: href, Label: "設定 ＞ " + label}
}

// The 「どこを見るか」 destinations of the Home screen. The Settings ones are
// built from settingspages so a renamed page or moved dialog breaks the test
// that checks every link, not the operator.
var (
	linkBroker       = settingsLink(settingspages.PageBroker, "", "証券会社・データ")
	linkDailyGuard   = settingsLink(settingspages.PageRisk, "modal-daily-guard", "リスク・資金")
	linkMetaModel    = settingsLink(settingspages.PageResults, "modal-metalabel", "予測モデル")
	linkBootstrap    = settingsLink(settingspages.PageOperations, "modal-paper-bootstrap", "取引ルール")
	linkEntryQuality = settingsLink(settingspages.PageOperations, "modal-policy-long", "取引ルール")
	linkUpdate       = settingsLink(settingspages.PageMaintenance, "update-panel", "システム")

	linkActivity  = homepanels.Link{Href: "/activity", Label: "稼働ログ"}
	linkScanner   = homepanels.Link{Href: "/scanner", Label: "候補銘柄"}
	linkProposals = homepanels.Link{Href: "/order-proposals", Label: "AIの注文提案"}
	linkSettings  = homepanels.Link{Href: settingspages.BasePath, Label: "設定"}
)
