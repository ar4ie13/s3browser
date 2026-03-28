package ui

import (
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// buildToolbar creates the main toolbar.
func (a *App) buildToolbar() *widget.Toolbar {
	return widget.NewToolbar(
		widget.NewToolbarAction(theme.LoginIcon(), func() {
			a.showConnectDialog()
		}),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.ContentAddIcon(), func() {
			a.showCreateBucketDialog()
		}),
		widget.NewToolbarAction(theme.MoveUpIcon(), func() {
			a.showUploadDialog()
		}),
		widget.NewToolbarAction(theme.DownloadIcon(), func() {
			a.downloadSelected()
		}),
		widget.NewToolbarAction(theme.DeleteIcon(), func() {
			a.deleteSelected()
		}),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.DocumentCreateIcon(), func() {
			a.showRenameDialog()
		}),
		widget.NewToolbarAction(theme.ContentCopyIcon(), func() {
			a.showCopyDialog()
		}),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.InfoIcon(), func() {
			a.showPropertiesDialog()
		}),
		widget.NewToolbarAction(theme.SettingsIcon(), func() {
			a.showACLDialog()
		}),
		widget.NewToolbarSeparator(),
		widget.NewToolbarAction(theme.ViewRefreshIcon(), func() {
			a.refreshCurrentView()
		}),
	)
}
