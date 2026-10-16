//go:build !windows

package core

import "res-downloader/core/shared"

// shortcut: other platforms remain unknown until native connection attribution is implemented.
func resolveSourceApplication(remote, local string) shared.ApplicationInfo {
	return shared.ApplicationInfo{}
}
