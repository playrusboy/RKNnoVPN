package updater

import (
	"fmt"
	"path/filepath"
)

type DownloadTransaction struct {
	CurrentVersion string
	DataDir        string
	Progress       func(downloaded, total int64)
	Logf           func(format string, args ...interface{})
}

func RunDownloadTransaction(tx DownloadTransaction) (*DownloadedUpdate, error) {
	tracker := NewDownloadTracker(tx.DataDir)
	if err := tracker.Begin(); err != nil {
		return nil, err
	}

	if err := tracker.Step("update-check", "running", "UPDATE_CHECKING", "checking latest release"); err != nil {
		return nil, err
	}
	info, err := CheckForUpdate(NormalizeVersionTag(tx.CurrentVersion))
	if err != nil {
		_ = tracker.Step("update-check", "failed", "UPDATE_CHECK_FAILED", err.Error())
		return nil, err
	}
	if !info.HasUpdate {
		_ = tracker.Step("update-check", "failed", "UPDATE_NOT_AVAILABLE", ErrNoUpdateAvailable.Error())
		return nil, ErrNoUpdateAvailable
	}
	if err := tracker.Step("update-download", "running", "UPDATE_DOWNLOADING", info.LatestVersion); err != nil {
		return nil, err
	}

	downloaded, err := DownloadUpdate(info, filepath.Join(tx.DataDir, "update"), func(downloaded, total int64) {
		detail := fmt.Sprintf("%d/%d", downloaded, total)
		_ = tracker.Step("update-download", "running", "UPDATE_DOWNLOADING", detail)
		if tx.Logf != nil {
			tx.Logf("download progress: %s bytes", detail)
		}
		if tx.Progress != nil {
			tx.Progress(downloaded, total)
		}
	})
	if err != nil {
		_ = tracker.Step("update-download", "failed", "UPDATE_DOWNLOAD_FAILED", err.Error())
		return nil, err
	}
	if err := tracker.Step("update-verify", "running", "UPDATE_VERIFYING", "verifying downloaded artifacts"); err != nil {
		return nil, err
	}
	if err := VerifyDownloadedUpdate(downloaded.ModulePath, downloaded.ApkPath); err != nil {
		_ = tracker.Step("update-verify", "failed", "UPDATE_VERIFY_FAILED", err.Error())
		return nil, err
	}
	if err := tracker.Step("persist-artifacts", "running", "UPDATE_PERSISTING", "persisting verified artifact state"); err != nil {
		return nil, err
	}
	if err := tracker.CompleteDownload(downloaded); err != nil {
		return nil, err
	}
	return downloaded, nil
}
