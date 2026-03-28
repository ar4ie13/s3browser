package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"s3browser/internal/s3client"
)

// buildTree creates the left-side tree panel for buckets/folders.
func (a *App) buildTree() fyne.CanvasObject {
	a.tree = widget.NewTree(
		// childUIDs
		func(uid widget.TreeNodeID) []widget.TreeNodeID {
			a.mu.Lock()
			defer a.mu.Unlock()

			children, ok := a.treeCache[uid]
			if !ok {
				loading := a.treeLoading[uid]
				if !loading {
					a.treeLoading[uid] = true
					go a.loadTreeChildren(uid)
				}
				return []widget.TreeNodeID{}
			}
			result := make([]widget.TreeNodeID, len(children))
			for i, c := range children {
				result[i] = widget.TreeNodeID(c)
			}
			return result
		},
		// isBranch
		func(uid widget.TreeNodeID) bool {
			if uid == "" {
				return true
			}
			// Bucket node (no slash) is a branch
			if !strings.Contains(string(uid), "/") {
				return true
			}
			// Folder node (ends with slash) is a branch
			if strings.HasSuffix(string(uid), "/") {
				return true
			}
			return false
		},
		// create
		func(branch bool) fyne.CanvasObject {
			icon := widget.NewIcon(nil)
			label := widget.NewLabel("")
			return container.NewHBox(icon, label)
		},
		// update
		func(uid widget.TreeNodeID, branch bool, obj fyne.CanvasObject) {
			box := obj.(*fyne.Container)
			icon := box.Objects[0].(*widget.Icon)
			label := box.Objects[1].(*widget.Label)

			uidStr := string(uid)

			if uid == "" {
				icon.SetResource(theme.HomeIcon())
				label.SetText("Buckets")
				return
			}

			if strings.HasSuffix(uidStr, "/") || strings.Contains(uidStr, "/") && !strings.HasSuffix(uidStr, "/") {
				// Determine display name
				parts := strings.Split(strings.TrimSuffix(uidStr, "/"), "/")
				name := parts[len(parts)-1]
				if strings.HasSuffix(uidStr, "/") {
					icon.SetResource(theme.FolderIcon())
				} else {
					icon.SetResource(theme.DocumentIcon())
				}
				label.SetText(name)
			} else {
				// Bucket
				icon.SetResource(theme.StorageIcon())
				label.SetText(uidStr)
			}
		},
	)

	a.tree.OnSelected = func(uid widget.TreeNodeID) {
		a.selectTreeNode(uid)
	}

	scroll := container.NewScroll(a.tree)
	return container.NewBorder(widget.NewLabel("Buckets"), nil, nil, nil, scroll)
}

// loadTreeChildren fetches children for a tree node in the background.
func (a *App) loadTreeChildren(uid string) {
	defer func() {
		a.mu.Lock()
		a.treeLoading[uid] = false
		a.mu.Unlock()
		a.tree.Refresh()
	}()

	if a.client == nil {
		a.mu.Lock()
		a.treeCache[uid] = []string{}
		a.mu.Unlock()
		return
	}

	ctx := context.Background()

	if uid == "" {
		// Root: load buckets
		buckets, err := a.client.ListBuckets(ctx)
		if err != nil {
			a.setStatus("Error loading buckets: " + err.Error())
			a.mu.Lock()
			a.treeCache[uid] = []string{}
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		a.treeCache[uid] = buckets
		a.mu.Unlock()
		return
	}

	uidStr := uid

	var bucket, prefix string
	if !strings.Contains(uidStr, "/") {
		// Direct bucket
		bucket = uidStr
		prefix = ""
	} else {
		// folder: "bucket/prefix/"
		idx := strings.Index(uidStr, "/")
		bucket = uidStr[:idx]
		prefix = uidStr[idx+1:]
	}

	objects, err := a.client.ListObjects(ctx, bucket, prefix)
	if err != nil {
		a.setStatus("Error listing objects: " + err.Error())
		a.mu.Lock()
		a.treeCache[uid] = []string{}
		a.mu.Unlock()
		return
	}

	var children []string
	for _, obj := range objects {
		if obj.IsPrefix {
			// uid = "bucket/prefix/"
			children = append(children, bucket+"/"+obj.Key)
		}
	}

	a.mu.Lock()
	a.treeCache[uid] = children
	a.mu.Unlock()
}

// selectTreeNode handles tree node selection.
func (a *App) selectTreeNode(uid widget.TreeNodeID) {
	if uid == "" {
		return
	}

	uidStr := string(uid)

	var bucket, prefix string
	if !strings.Contains(uidStr, "/") {
		bucket = uidStr
		prefix = ""
	} else {
		idx := strings.Index(uidStr, "/")
		bucket = uidStr[:idx]
		prefix = uidStr[idx+1:]
	}

	a.currentBucket = bucket
	a.currentPrefix = prefix

	path := bucket
	if prefix != "" {
		path += "/" + prefix
	}
	a.pathLabel.SetText(path)

	go a.refreshFileList()
}

// refreshFileList reloads the file list for the current bucket/prefix.
func (a *App) refreshFileList() {
	if a.client == nil || a.currentBucket == "" {
		a.mu.Lock()
		a.objects = nil
		a.selectedRows = make(map[int]bool)
		a.mu.Unlock()
		a.table.Refresh()
		return
	}

	ctx := context.Background()
	objs, err := a.client.ListObjects(ctx, a.currentBucket, a.currentPrefix)
	if err != nil {
		a.setStatus("Error: " + err.Error())
		return
	}

	a.mu.Lock()
	a.objects = objs
	a.selectedRows = make(map[int]bool)
	a.mu.Unlock()

	a.table.Refresh()
	a.setStatus(fmt.Sprintf("%d objects", len(objs)))
}

// buildFileList creates the main file list table.
func (a *App) buildFileList() fyne.CanvasObject {
	headers := []string{"", "Name", "Size", "Modified", "Storage Class"}

	// Header row
	headerCells := make([]fyne.CanvasObject, len(headers))
	for i, h := range headers {
		lbl := widget.NewLabelWithStyle(h, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		headerCells[i] = lbl
	}
	headerRow := container.NewGridWithColumns(len(headers), headerCells...)

	a.table = widget.NewTable(
		// length
		func() (int, int) {
			a.mu.Lock()
			defer a.mu.Unlock()
			return len(a.objects), 5
		},
		// create cell
		func() fyne.CanvasObject {
			return container.NewStack(
				widget.NewCheck("", nil),
				widget.NewLabel(""),
			)
		},
		// update cell
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			c := cell.(*fyne.Container)
			check := c.Objects[0].(*widget.Check)
			lbl := c.Objects[1].(*widget.Label)

			a.mu.Lock()
			if id.Row >= len(a.objects) {
				a.mu.Unlock()
				check.Hide()
				lbl.SetText("")
				return
			}
			obj := a.objects[id.Row]
			selected := a.selectedRows[id.Row]
			a.mu.Unlock()

			switch id.Col {
			case 0:
				check.Show()
				lbl.Hide()
				check.Checked = selected
				check.OnChanged = func(checked bool) {
					a.mu.Lock()
					if checked {
						a.selectedRows[id.Row] = true
					} else {
						delete(a.selectedRows, id.Row)
					}
					a.mu.Unlock()
				}
				check.Refresh()
			case 1:
				check.Hide()
				lbl.Show()
				name := obj.Key
				if a.currentPrefix != "" && strings.HasPrefix(name, a.currentPrefix) {
					name = name[len(a.currentPrefix):]
				}
				name = strings.TrimSuffix(name, "/")
				if name == "" {
					name = "/"
				}
				lbl.SetText(name)
			case 2:
				check.Hide()
				lbl.Show()
				if obj.IsPrefix {
					lbl.SetText("")
				} else {
					lbl.SetText(formatSize(obj.Size))
				}
			case 3:
				check.Hide()
				lbl.Show()
				if obj.IsPrefix || obj.LastModified.IsZero() {
					lbl.SetText("")
				} else {
					lbl.SetText(obj.LastModified.Format("2006-01-02 15:04:05"))
				}
			case 4:
				check.Hide()
				lbl.Show()
				if obj.IsPrefix {
					lbl.SetText("")
				} else {
					lbl.SetText(obj.StorageClass)
				}
			}
		},
	)

	// Column widths
	a.table.SetColumnWidth(0, 32)
	a.table.SetColumnWidth(1, 350)
	a.table.SetColumnWidth(2, 100)
	a.table.SetColumnWidth(3, 160)
	a.table.SetColumnWidth(4, 120)

	// Track last tap time for double-click detection
	var lastTapRow int = -1
	var lastTapTime time.Time

	a.table.OnSelected = func(id widget.TableCellID) {
		a.mu.Lock()
		if id.Row >= len(a.objects) {
			a.mu.Unlock()
			return
		}
		obj := a.objects[id.Row]
		a.mu.Unlock()

		if id.Col == 0 {
			// Toggle checkbox
			a.mu.Lock()
			if a.selectedRows[id.Row] {
				delete(a.selectedRows, id.Row)
			} else {
				a.selectedRows[id.Row] = true
			}
			a.mu.Unlock()
			a.table.Refresh()
			return
		}

		// Check for double-click
		now := time.Now()
		if lastTapRow == id.Row && now.Sub(lastTapTime) < 500*time.Millisecond {
			// Double-click: navigate into folder if it's a prefix
			if obj.IsPrefix {
				a.currentPrefix = obj.Key
				a.pathLabel.SetText(a.currentBucket + "/" + a.currentPrefix)
				go a.refreshFileList()
			}
		} else {
			a.mu.Lock()
			a.selectedRows = map[int]bool{id.Row: true}
			a.mu.Unlock()
			a.table.Refresh()
		}

		lastTapRow = id.Row
		lastTapTime = now
	}

	tableScroll := container.NewScroll(a.table)

	return container.NewBorder(headerRow, nil, nil, nil, tableScroll)
}

// getSelectedObjects returns the currently selected objects.
func (a *App) getSelectedObjects() []s3client.Object {
	a.mu.Lock()
	defer a.mu.Unlock()

	var result []s3client.Object
	for row := range a.selectedRows {
		if row < len(a.objects) {
			result = append(result, a.objects[row])
		}
	}
	return result
}

// formatSize formats a byte size into human-readable form.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// refreshCurrentView clears the tree cache and reloads everything.
func (a *App) refreshCurrentView() {
	a.mu.Lock()
	a.treeCache = make(map[string][]string)
	a.treeLoading = make(map[string]bool)
	a.mu.Unlock()

	a.tree.Refresh()
	go a.refreshFileList()
}

