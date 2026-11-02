package notify

import "testing"

func TestRoutesSettingsValidationAndRecovery(t *testing.T) {
	dir := t.TempDir()
	n, err := New(dir, Config{})
	if err != nil {
		t.Fatal(err)
	}
	routes := defaultRoutes()
	routes[0].URL = "http://example.com"
	revision := n.Settings().Revision
	update := SettingsRequest{ExpectedRevision: revision, URL: "https://default.example.com", AuthMode: "set", Secret: "independent", Routes: routes[:]}
	if _, err = n.UpdateSettings(update, nil); err == nil {
		t.Fatal("HTTP dedicated target accepted")
	}
	update.Routes = append(update.Routes, Route{})
	if _, err = n.UpdateSettings(update, nil); err == nil {
		t.Fatal("extra categories accepted")
	}
	update.Routes = routes[:]
	routes[0].URL = "https://container.example.com"
	routes[1].Enabled = false
	saved, err := n.UpdateSettings(update, nil)
	if err != nil {
		t.Fatal(err)
	}
	n.Close()
	n, err = New(dir, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if got := n.Settings(); got.Routes != saved.Routes || got.Revision != saved.Revision {
		t.Fatal("routes not durably restored")
	}
	update.ExpectedRevision = saved.Revision
	update.AuthMode = "retain"
	update.Secret = ""
	routes[0].URL = "https://different.example.com"
	if _, err = n.UpdateSettings(update, nil); err == nil {
		t.Fatal("new receiver reused signing secret without confirmation")
	}
}
