package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"s3browser/internal/config"
	"s3browser/internal/s3client"
)

// connectToServer establishes an S3 connection and initialises the tree.
func (a *App) connectToServer(conn config.Connection) {
	a.setStatus("Connecting to " + conn.Endpoint + "...")

	region := conn.Region
	if region == "" {
		region = "us-east-1"
	}

	client, err := s3client.New(conn.Endpoint, conn.AccessKey, conn.SecretKey, region)
	if err != nil {
		dialog.ShowError(err, a.window)
		a.setStatus("Connection failed")
		return
	}

	a.client = client

	a.mu.Lock()
	a.treeCache = make(map[string][]string)
	a.treeLoading = make(map[string]bool)
	a.mu.Unlock()

	// Trigger bucket list load
	a.tree.Refresh()
	go a.loadTreeChildren("")

	a.setStatus("Connected to " + conn.Name)
}

// showUploadDialog opens the file chooser to upload files.
func (a *App) showUploadDialog() {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect to an S3 server first.", a.window)
		return
	}
	if a.currentBucket == "" {
		dialog.ShowInformation("No Bucket Selected", "Please select a bucket before uploading.", a.window)
		return
	}

	fd := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if uc == nil {
			return
		}
		defer uc.Close()

		localPath := uc.URI().Path()
		filename := filepath.Base(localPath)
		key := a.currentPrefix + filename

		go a.uploadFile(localPath, a.currentBucket, key)
	}, a.window)
	fd.Show()
}

// uploadFile uploads a local file to the current S3 bucket/prefix.
func (a *App) uploadFile(localPath, bucket, key string) {
	a.setStatus("Uploading " + filepath.Base(localPath) + "...")
	a.showProgress(true)
	a.setProgress(0)

	ctx := context.Background()
	err := a.client.UploadFile(ctx, bucket, key, localPath, func(written, total int64) {
		if total > 0 {
			a.setProgress(float64(written) / float64(total))
		}
	})

	a.showProgress(false)

	if err != nil {
		dialog.ShowError(err, a.window)
		a.setStatus("Upload failed: " + err.Error())
		return
	}

	a.setStatus("Uploaded: " + key)
	go a.refreshFileList()
}

// downloadSelected downloads the selected objects.
func (a *App) downloadSelected() {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect first.", a.window)
		return
	}

	selected := a.getSelectedObjects()
	if len(selected) == 0 {
		dialog.ShowInformation("No Selection", "Please select one or more objects to download.", a.window)
		return
	}

	// Filter out prefixes
	var toDownload []s3client.Object
	for _, obj := range selected {
		if !obj.IsPrefix {
			toDownload = append(toDownload, obj)
		}
	}

	if len(toDownload) == 0 {
		dialog.ShowInformation("No Files", "No downloadable files selected (folders cannot be downloaded directly).", a.window)
		return
	}

	dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if uri == nil {
			return
		}
		destDir := uri.Path()

		go func() {
			ctx := context.Background()
			a.showProgress(true)
			defer a.showProgress(false)

			for i, obj := range toDownload {
				filename := filepath.Base(obj.Key)
				localPath := filepath.Join(destDir, filename)

				a.setStatus(fmt.Sprintf("Downloading %d/%d: %s", i+1, len(toDownload), filename))
				a.setProgress(float64(i) / float64(len(toDownload)))

				idx := i // capture for closure
				if err := a.client.DownloadFile(ctx, a.currentBucket, obj.Key, localPath, func(written, total int64) {
					if total > 0 {
						progress := (float64(idx) + float64(written)/float64(total)) / float64(len(toDownload))
						a.setProgress(progress)
					}
				}); err != nil {
					dialog.ShowError(err, a.window)
					return
				}
			}

			a.setStatus(fmt.Sprintf("Downloaded %d file(s) to %s", len(toDownload), destDir))
		}()
	}, a.window)
}

// deleteSelected deletes the selected objects or bucket.
func (a *App) deleteSelected() {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect first.", a.window)
		return
	}

	selected := a.getSelectedObjects()

	if len(selected) == 0 && a.currentBucket != "" && a.currentPrefix == "" {
		// Delete the currently viewed bucket
		msg := fmt.Sprintf("Delete bucket '%s'? This cannot be undone.", a.currentBucket)
		dialog.ShowConfirm("Delete Bucket", msg, func(ok bool) {
			if !ok {
				return
			}
			go func() {
				ctx := context.Background()
				if err := a.client.DeleteBucket(ctx, a.currentBucket); err != nil {
					dialog.ShowError(err, a.window)
					return
				}
				a.currentBucket = ""
				a.currentPrefix = ""
				a.pathLabel.SetText("")
				a.setStatus("Bucket deleted")
				a.refreshCurrentView()
			}()
		}, a.window)
		return
	}

	if len(selected) == 0 {
		dialog.ShowInformation("No Selection", "Please select objects to delete.", a.window)
		return
	}

	// Build list of keys to delete
	var keys []string
	for _, obj := range selected {
		if !obj.IsPrefix {
			keys = append(keys, obj.Key)
		}
	}

	if len(keys) == 0 {
		dialog.ShowInformation("No Files", "Only files can be deleted. Select individual files.", a.window)
		return
	}

	var names []string
	for _, k := range keys {
		names = append(names, filepath.Base(k))
	}
	msg := fmt.Sprintf("Delete %d object(s)?\n%s\n\nThis cannot be undone.", len(keys), strings.Join(names, "\n"))

	dialog.ShowConfirm("Delete Objects", msg, func(ok bool) {
		if !ok {
			return
		}
		go func() {
			ctx := context.Background()
			if err := a.client.DeleteObjects(ctx, a.currentBucket, keys); err != nil {
				dialog.ShowError(err, a.window)
				return
			}
			a.setStatus(fmt.Sprintf("Deleted %d object(s)", len(keys)))
			go a.refreshFileList()
		}()
	}, a.window)
}

// handleDroppedFiles handles drag & drop file uploads.
func (a *App) handleDroppedFiles(uris []fyne.URI) {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect to an S3 server before dropping files.", a.window)
		return
	}
	if a.currentBucket == "" {
		dialog.ShowInformation("No Bucket Selected", "Please select a bucket before dropping files.", a.window)
		return
	}

	for _, uri := range uris {
		localPath := uri.Path()
		filename := filepath.Base(localPath)
		key := a.currentPrefix + filename
		lp := localPath
		bkt := a.currentBucket
		k := key
		go a.uploadFile(lp, bkt, k)
	}
}
