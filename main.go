package main

import (
	"fyne.io/fyne/v2/app"
	"s3browser/internal/ui"
)

func main() {
	a := app.NewWithID("com.s3browser.app")
	w := ui.NewMainWindow(a)
	w.ShowAndRun()
}
