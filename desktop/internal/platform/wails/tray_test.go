package wails

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type trayTestWindow struct {
	application.Window
	hidden, shown, focused bool
}

func (w *trayTestWindow) Hide() application.Window { w.hidden = true; return w }
func (w *trayTestWindow) Show() application.Window { w.shown = true; return w }
func (w *trayTestWindow) Focus()                   { w.focused = true }

func TestTrayActionTargetsMainWindow(t *testing.T) {
	for _, action := range []string{"show", "hide", "dismiss", "invalid"} {
		t.Run(action, func(t *testing.T) {
			main, popup := &trayTestWindow{}, &trayTestWindow{}
			host := &DesktopHost{window: main, trayWindow: popup}
			err := host.TrayAction(action)
			if (err != nil) != (action == "invalid") {
				t.Fatalf("unexpected error: %v", err)
			}
			if popup.hidden != (action != "invalid") {
				t.Fatal("incorrect popup dismissal")
			}
			if main.hidden != (action == "hide") {
				t.Fatal("hide must target main window")
			}
			if main.shown != (action == "show") || main.focused != (action == "show") {
				t.Fatal("show must reveal and focus main window")
			}
		})
	}
}
