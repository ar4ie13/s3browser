package ui

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"s3browser/internal/config"
	"s3browser/internal/s3client"
)

// App holds all application state and UI references.
type App struct {
	fyneApp fyne.App
	window  fyne.Window
	client  *s3client.Client
	cfg     *config.AppConfig

	// UI state
	mu            sync.Mutex
	treeCache     map[string][]string
	treeLoading   map[string]bool
	objects       []s3client.Object
	currentBucket string
	currentPrefix string
	selectedRows  map[int]bool

	// Widgets
	tree        *widget.Tree
	table       *widget.Table
	statusLabel *widget.Label
	progressBar *widget.ProgressBar
	pathLabel   *widget.Label
}

// NewMainWindow creates and returns the application's main window.
func NewMainWindow(a fyne.App) fyne.Window {
	app := &App{
		fyneApp:     a,
		treeCache:   make(map[string][]string),
		treeLoading: make(map[string]bool),
		selectedRows: make(map[int]bool),
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = &config.AppConfig{LastConnection: -1}
	}
	app.cfg = cfg

	w := a.NewWindow("S3 Browser")
	w.Resize(fyne.NewSize(1200, 700))
	app.window = w

	app.buildUI()

	// Set drag & drop handler
	w.SetOnDropped(func(pos fyne.Position, uris []fyne.URI) {
		app.handleDroppedFiles(uris)
	})

	// Auto-connect to last connection
	if cfg.LastConnection >= 0 && cfg.LastConnection < len(cfg.Connections) {
		conn := cfg.Connections[cfg.LastConnection]
		go app.connectToServer(conn)
	} else {
		app.showConnectDialog()
	}

	return w
}

// buildUI assembles the main window layout.
func (a *App) buildUI() {
	toolbar := a.buildToolbar()

	treePane := a.buildTree()
	filePane := a.buildFileList()

	split := container.NewHSplit(treePane, filePane)
	split.Offset = 0.25

	a.progressBar = widget.NewProgressBar()
	a.progressBar.Hide()

	a.statusLabel = widget.NewLabel("Ready")
	a.pathLabel = widget.NewLabel("")

	statusBar := container.NewBorder(nil, nil, a.pathLabel, nil,
		container.NewHBox(a.progressBar, a.statusLabel),
	)

	content := container.NewBorder(toolbar, statusBar, nil, nil, split)
	a.window.SetContent(content)
}

// setStatus updates the status label text.
func (a *App) setStatus(msg string) {
	a.statusLabel.SetText(msg)
}

// showProgress shows or hides the progress bar.
func (a *App) showProgress(show bool) {
	if show {
		a.progressBar.Show()
	} else {
		a.progressBar.Hide()
	}
}

// setProgress sets the progress bar value (0.0-1.0).
func (a *App) setProgress(v float64) {
	a.progressBar.SetValue(v)
}
