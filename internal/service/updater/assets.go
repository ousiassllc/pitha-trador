package updater

import (
	"fmt"
	"strings"
)

// selectAssets finds the NSIS installer and its checksums.txt among
// assets (issue #64's naming: `*-installer.exe` + `checksums.txt`).
func selectAssets(assets []Asset) (installer, checksums Asset, err error) {
	var foundInstaller, foundChecksums bool
	for _, a := range assets {
		switch {
		case strings.HasSuffix(a.Name, installerAssetSuffix):
			installer, foundInstaller = a, true
		case a.Name == checksumsAssetName:
			checksums, foundChecksums = a, true
		}
	}
	if !foundInstaller {
		return Asset{}, Asset{}, fmt.Errorf("no %s asset found", installerAssetSuffix)
	}
	if !foundChecksums {
		return Asset{}, Asset{}, fmt.Errorf("no %s asset found", checksumsAssetName)
	}
	return installer, checksums, nil
}
