package updater

import "os/exec"

// silentInstallFlag is NSIS' standard "run fully unattended" flag every
// `wails build -nsis` installer supports out of the box (issue #65: "別
// プロセスとしてインストーラーをサイレント実行する(installer.exe /S)").
const silentInstallFlag = "/S"

// BuildSilentInstallCommand builds (but does not start) the *exec.Cmd
// that runs installerPath fully unattended. Actually starting it - and
// the Wails runtime.Quit this must follow - is cmd/desktop's Quitter
// implementation's job (package doc comment):
// this stays a pure, easily unit-testable argument-construction function,
// since actually running a Windows installer is not possible from this
// repository's Linux CI (issue #65's acceptance criteria explicitly scope
// verification down to "呼び出すコマンド構築ロジック自体のユニットテス
// ト").
func BuildSilentInstallCommand(installerPath string) *exec.Cmd {
	return exec.Command(installerPath, silentInstallFlag)
}
