package ui

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"s3browser/internal/config"
)

// showConnectDialog shows the connection dialog.
func (a *App) showConnectDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("My S3 Connection")

	endpointEntry := widget.NewEntry()
	endpointEntry.SetPlaceHolder("https://s3.amazonaws.com (leave empty for AWS)")

	accessEntry := widget.NewEntry()
	accessEntry.SetPlaceHolder("Access Key ID")

	secretEntry := widget.NewPasswordEntry()
	secretEntry.SetPlaceHolder("Secret Access Key")

	regionEntry := widget.NewEntry()
	regionEntry.SetText("us-east-1")

	// Pre-fill from last connection if available
	if a.cfg.LastConnection >= 0 && a.cfg.LastConnection < len(a.cfg.Connections) {
		conn := a.cfg.Connections[a.cfg.LastConnection]
		nameEntry.SetText(conn.Name)
		endpointEntry.SetText(conn.Endpoint)
		accessEntry.SetText(conn.AccessKey)
		secretEntry.SetText(conn.SecretKey)
		regionEntry.SetText(conn.Region)
	}

	formItems := []*widget.FormItem{
		{Text: "Profile Name", Widget: nameEntry},
		{Text: "Endpoint URL", Widget: endpointEntry},
		{Text: "Access Key", Widget: accessEntry},
		{Text: "Secret Key", Widget: secretEntry},
		{Text: "Region", Widget: regionEntry},
	}

	// Track selected connection index for the delete button
	selectedConnIdx := -1

	// Saved connections list
	savedConnList := widget.NewList(
		func() int {
			return len(a.cfg.Connections)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			lbl := obj.(*widget.Label)
			if id < len(a.cfg.Connections) {
				lbl.SetText(a.cfg.Connections[id].Name)
			}
		},
	)

	savedConnList.OnSelected = func(id widget.ListItemID) {
		selectedConnIdx = id
		if id < len(a.cfg.Connections) {
			conn := a.cfg.Connections[id]
			nameEntry.SetText(conn.Name)
			endpointEntry.SetText(conn.Endpoint)
			accessEntry.SetText(conn.AccessKey)
			secretEntry.SetText(conn.SecretKey)
			regionEntry.SetText(conn.Region)
		}
	}

	formWidget := widget.NewForm(formItems...)

	var d dialog.Dialog

	connectBtn := widget.NewButton("Connect", func() {
		conn := config.Connection{
			Name:      nameEntry.Text,
			Endpoint:  endpointEntry.Text,
			AccessKey: accessEntry.Text,
			SecretKey: secretEntry.Text,
			Region:    regionEntry.Text,
		}

		if conn.Name == "" {
			conn.Name = conn.Endpoint
		}
		if conn.Name == "" {
			conn.Name = "Default"
		}

		// Save or update connection in config
		found := false
		for i, c := range a.cfg.Connections {
			if c.Name == conn.Name {
				a.cfg.Connections[i] = conn
				a.cfg.LastConnection = i
				found = true
				break
			}
		}
		if !found {
			a.cfg.Connections = append(a.cfg.Connections, conn)
			a.cfg.LastConnection = len(a.cfg.Connections) - 1
		}
		_ = a.cfg.Save()

		if d != nil {
			d.Hide()
		}

		go a.connectToServer(conn)
	})

	deleteBtn := widget.NewButton("Delete Profile", func() {
		if selectedConnIdx >= 0 && selectedConnIdx < len(a.cfg.Connections) {
			a.cfg.Connections = append(
				a.cfg.Connections[:selectedConnIdx],
				a.cfg.Connections[selectedConnIdx+1:]...,
			)
			if a.cfg.LastConnection >= len(a.cfg.Connections) {
				a.cfg.LastConnection = len(a.cfg.Connections) - 1
			}
			selectedConnIdx = -1
			_ = a.cfg.Save()
			savedConnList.Refresh()
		}
	})

	content := container.NewVSplit(
		container.NewBorder(widget.NewLabel("Saved Connections"), nil, nil, nil,
			container.NewScroll(savedConnList),
		),
		container.NewVBox(
			formWidget,
			container.NewHBox(connectBtn, deleteBtn),
		),
	)
	content.Offset = 0.3

	d = dialog.NewCustom("Connect to S3", "Cancel", content, a.window)
	d.Resize(fyne.NewSize(500, 500))
	d.Show()
}

// showCreateBucketDialog shows the create bucket dialog.
func (a *App) showCreateBucketDialog() {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect to an S3 server first.", a.window)
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("my-bucket")

	regionEntry := widget.NewEntry()
	regionEntry.SetText("us-east-1")

	items := []*widget.FormItem{
		{Text: "Bucket Name", Widget: nameEntry},
		{Text: "Region", Widget: regionEntry},
	}

	dialog.ShowForm("Create Bucket", "Create", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(nameEntry.Text)
		region := strings.TrimSpace(regionEntry.Text)
		if name == "" {
			dialog.ShowError(fmt.Errorf("bucket name cannot be empty"), a.window)
			return
		}
		go func() {
			ctx := context.Background()
			if err := a.client.CreateBucket(ctx, name, region); err != nil {
				dialog.ShowError(err, a.window)
				return
			}
			a.setStatus("Bucket created: " + name)
			a.refreshCurrentView()
		}()
	}, a.window)
}

// showRenameDialog shows the rename dialog for the selected object.
func (a *App) showRenameDialog() {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect first.", a.window)
		return
	}

	selected := a.getSelectedObjects()
	if len(selected) == 0 {
		dialog.ShowInformation("No Selection", "Please select an object to rename.", a.window)
		return
	}

	obj := selected[0]
	if obj.IsPrefix {
		dialog.ShowInformation("Cannot Rename", "Cannot rename folders directly. Please rename individual objects.", a.window)
		return
	}

	// Show just the filename portion
	oldKey := obj.Key
	oldName := oldKey
	if a.currentPrefix != "" && strings.HasPrefix(oldName, a.currentPrefix) {
		oldName = oldName[len(a.currentPrefix):]
	}

	newNameEntry := widget.NewEntry()
	newNameEntry.SetText(oldName)

	items := []*widget.FormItem{
		{Text: "New Name", Widget: newNameEntry},
	}

	dialog.ShowForm("Rename Object", "Rename", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		newName := strings.TrimSpace(newNameEntry.Text)
		if newName == "" {
			dialog.ShowError(fmt.Errorf("name cannot be empty"), a.window)
			return
		}
		newKey := a.currentPrefix + newName

		go func() {
			ctx := context.Background()
			if err := a.client.CopyObject(ctx, a.currentBucket, oldKey, a.currentBucket, newKey); err != nil {
				dialog.ShowError(err, a.window)
				return
			}
			if err := a.client.DeleteObject(ctx, a.currentBucket, oldKey); err != nil {
				dialog.ShowError(err, a.window)
				return
			}
			a.setStatus("Renamed to " + newName)
			go a.refreshFileList()
		}()
	}, a.window)
}

// showCopyDialog shows the copy object dialog.
func (a *App) showCopyDialog() {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect first.", a.window)
		return
	}

	selected := a.getSelectedObjects()
	if len(selected) == 0 {
		dialog.ShowInformation("No Selection", "Please select an object to copy.", a.window)
		return
	}

	obj := selected[0]
	if obj.IsPrefix {
		dialog.ShowInformation("Cannot Copy", "Cannot copy folders directly. Please copy individual objects.", a.window)
		return
	}

	dstBucketEntry := widget.NewEntry()
	dstBucketEntry.SetText(a.currentBucket)

	dstKeyEntry := widget.NewEntry()
	dstKeyEntry.SetText(obj.Key)

	items := []*widget.FormItem{
		{Text: "Destination Bucket", Widget: dstBucketEntry},
		{Text: "Destination Key", Widget: dstKeyEntry},
	}

	dialog.ShowForm("Copy Object", "Copy", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		dstBucket := strings.TrimSpace(dstBucketEntry.Text)
		dstKey := strings.TrimSpace(dstKeyEntry.Text)
		if dstBucket == "" || dstKey == "" {
			dialog.ShowError(fmt.Errorf("destination bucket and key cannot be empty"), a.window)
			return
		}

		go func() {
			ctx := context.Background()
			if err := a.client.CopyObject(ctx, a.currentBucket, obj.Key, dstBucket, dstKey); err != nil {
				dialog.ShowError(err, a.window)
				return
			}
			a.setStatus("Copied to " + dstBucket + "/" + dstKey)
			go a.refreshFileList()
		}()
	}, a.window)
}

// showPropertiesDialog shows the properties/metadata for the selected object or bucket.
func (a *App) showPropertiesDialog() {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect first.", a.window)
		return
	}

	selected := a.getSelectedObjects()

	if len(selected) == 0 {
		// Show bucket properties
		if a.currentBucket == "" {
			dialog.ShowInformation("No Selection", "Please select a bucket or object.", a.window)
			return
		}

		ctx := context.Background()
		go func() {
			objs, err := a.client.ListObjects(ctx, a.currentBucket, "")
			count := 0
			if err == nil {
				count = len(objs)
			}
			info := fmt.Sprintf("Bucket: %s\nObject Count: %d", a.currentBucket, count)
			dialog.ShowInformation("Bucket Properties", info, a.window)
		}()
		return
	}

	obj := selected[0]

	if obj.IsPrefix {
		info := fmt.Sprintf("Folder: %s\nBucket: %s", obj.Key, a.currentBucket)
		dialog.ShowInformation("Folder Properties", info, a.window)
		return
	}

	ctx := context.Background()
	go func() {
		meta, err := a.client.GetObjectMetadata(ctx, a.currentBucket, obj.Key)
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}

		// Build a key-value display
		rows := make([]fyne.CanvasObject, 0, len(meta)*2)
		for k, v := range meta {
			rows = append(rows,
				widget.NewLabelWithStyle(k+":", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				widget.NewLabel(v),
			)
		}

		grid := container.NewGridWithColumns(2, rows...)
		scroll := container.NewScroll(grid)

		d := dialog.NewCustom("Object Properties: "+obj.Key, "Close", scroll, a.window)
		d.Resize(fyne.NewSize(500, 400))
		d.Show()
	}()
}

// showACLDialog shows the ACL management dialog.
func (a *App) showACLDialog() {
	if a.client == nil {
		dialog.ShowInformation("Not Connected", "Please connect first.", a.window)
		return
	}

	aclOptions := []string{"private", "public-read", "public-read-write", "authenticated-read"}

	selected := a.getSelectedObjects()
	isBucket := len(selected) == 0 || (len(selected) > 0 && selected[0].IsPrefix)

	var target string
	if len(selected) > 0 && !selected[0].IsPrefix {
		target = selected[0].Key
	} else {
		target = a.currentBucket
	}

	if target == "" {
		dialog.ShowInformation("No Selection", "Please select a bucket or object.", a.window)
		return
	}

	ctx := context.Background()

	go func() {
		var currentACL string
		var err error

		if isBucket {
			currentACL, err = a.client.GetBucketACL(ctx, a.currentBucket)
		} else {
			currentACL, err = a.client.GetObjectACL(ctx, a.currentBucket, target)
		}

		if err != nil {
			// Some endpoints don't support ACL; show dialog anyway with default
			currentACL = "private"
		}

		radio := widget.NewRadioGroup(aclOptions, nil)
		radio.SetSelected(currentACL)

		targetLabel := widget.NewLabel("Target: " + target)

		content := container.NewVBox(
			targetLabel,
			widget.NewLabel("Select ACL:"),
			radio,
		)

		var d dialog.Dialog
		applyBtn := widget.NewButton("Apply", func() {
			newACL := radio.Selected
			if newACL == "" {
				return
			}
			d.Hide()
			go func() {
				var applyErr error
				if isBucket {
					applyErr = a.client.SetBucketACL(ctx, a.currentBucket, newACL)
				} else {
					applyErr = a.client.SetObjectACL(ctx, a.currentBucket, target, newACL)
				}
				if applyErr != nil {
					dialog.ShowError(applyErr, a.window)
					return
				}
				a.setStatus("ACL updated to " + newACL)
			}()
		})

		content = container.NewVBox(content, applyBtn)

		d = dialog.NewCustom("Manage ACL", "Cancel", content, a.window)
		d.Resize(fyne.NewSize(350, 250))
		d.Show()
	}()
}

