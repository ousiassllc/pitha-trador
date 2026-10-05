package updater

import (
	"fmt"
	"strings"
)

// releaseAssets are the release assets the update pipeline uses.
type releaseAssets struct {
	installer, checksums Asset
	// signature is checksums.txt.sig (issue #376); its Name is empty when
	// the release has none.
	signature Asset
}

// selectAssets finds the NSIS installer and its checksums.txt among
// assets (issue #64's naming: `*-installer.exe` + `checksums.txt`), plus
// the optional detached signature `checksums.txt.sig`.
func selectAssets(assets []Asset) (releaseAssets, error) {
	var found releaseAssets
	var foundInstaller, foundChecksums bool
	for _, a := range assets {
		switch {
		case strings.HasSuffix(a.Name, installerAssetSuffix):
			found.installer, foundInstaller = a, true
		case a.Name == checksumsAssetName:
			found.checksums, foundChecksums = a, true
		case a.Name == signatureAssetName:
			found.signature = a
		}
	}
	if !foundInstaller {
		return releaseAssets{}, fmt.Errorf("no %s asset found", installerAssetSuffix)
	}
	if !foundChecksums {
		return releaseAssets{}, fmt.Errorf("no %s asset found", checksumsAssetName)
	}
	return found, nil
}
