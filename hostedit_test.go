package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
	"gorex/locales"
)

func TestHostProfilesPreserveIdentityAndRestart(t *testing.T) {
	isolateSettings(t, "")
	a := &App{}
	a.hello.Host.Name = "system-mac"
	h := newHostConn(savedHost{ID: "saved-id", Name: "office", Address: "original-address"})
	a.hosts = []*hostConn{h}
	for _, item := range []struct {
		h *hostConn
		p hostProfile
	}{{nil, hostProfile{"开发机", "本机用途"}}, {h, hostProfile{"训练机", "训练\n评测"}}} {
		a.openHostEditor(item.h, false)
		a.hostEdit.draft = item.p
		if !a.saveHostEditor(a.hostEdit) {
			t.Fatal(a.hostEdit.err.text())
		}
	}
	if a.hostName(nil) != "开发机" || a.hostName(h) != "训练机" {
		t.Fatal("display names did not use aliases")
	}
	if a.localName() != "system-mac" || h.Name != "office" || h.Address != "original-address" || h.ID != "saved-id" {
		t.Fatal("a display edit changed a machine's identity")
	}
	want := prefs.HostProfiles
	prefs = settings{}
	loadSettings()
	if !reflect.DeepEqual(prefs.HostProfiles, want) {
		t.Fatalf("profiles after restart: %+v", prefs.HostProfiles)
	}
	if name, _, _ := a.hostIdentity(); name != "开发机" {
		t.Fatalf("the host chip uses %q", name)
	}
	if !a.setHostProfile(h.ID, hostProfile{}) || a.hostName(h) != "office" {
		t.Fatal("clearing an alias lost the original saved name")
	}
	if !a.setHostProfile("local", hostProfile{}) || a.hostName(nil) != thisMachine() {
		t.Fatal("clearing the local alias changed its default label")
	}
	if name, _, _ := a.hostIdentity(); name != "system-mac" {
		t.Fatal("clearing the alias lost the system name")
	}
	if info, err := os.Stat(settingsPath()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings permission: %v, %v", info, err)
	}
}

func TestHostProfilesSurviveDiscovery(t *testing.T) {
	isolateSettings(t, "")
	a := &App{}
	ids := []string{networkHostID("public-key"), "tailscale:scope:stable-node"}
	for _, id := range ids {
		h := newHostConn(savedHost{ID: id, Name: "old-name", Address: "old-address"})
		a.hosts = []*hostConn{h}
		if !a.setHostProfile(id, hostProfile{"自定义", "保留"}) {
			t.Fatal("save")
		}
		// Directory refreshes may replace the connection and both facts.
		a.hosts = nil
		h = newHostConn(savedHost{ID: id, Name: "new-name", Address: "new-address"})
		a.hosts = []*hostConn{h}
		if a.hostName(h) != "自定义" || prefs.HostProfiles[id].Note != "保留" {
			t.Fatal("discovery overwrote client information")
		}
		other := newHostConn(savedHost{ID: id + "-other", Name: "other"})
		if a.hostName(other) != "other" {
			t.Fatal("a different identity inherited an alias")
		}
	}
}

func TestHostProfileWriteFailureKeepsDraftAndFile(t *testing.T) {
	isolateSettings(t, "")
	a := &App{}
	prefs.Appearance = "dark"
	if !a.setHostProfile("local", hostProfile{"before", "keep"}) {
		t.Fatal("save initial profile")
	}
	old := []byte("not valid JSON")
	if err := os.WriteFile(settingsPath(), old, 0o600); err != nil {
		t.Fatal(err)
	}
	a.openHostEditor(nil, false)
	a.hostEdit.draft = hostProfile{"after", "new draft"}
	if a.saveHostEditor(a.hostEdit) || a.hostEdit.err != hostEditWriteFailed {
		t.Fatal("a failed write reported success")
	}
	if prefs.HostProfiles["local"].Alias != "before" || prefs.Appearance != "dark" || a.hostEdit.draft.Alias != "after" {
		t.Fatal("failure changed preferences or discarded the draft")
	}
	if b, _ := os.ReadFile(settingsPath()); string(b) != string(old) {
		t.Fatal("a malformed settings file was overwritten")
	}
}

func TestHostProfileValidation(t *testing.T) {
	for _, c := range []struct {
		p hostProfile
		e hostEditError
	}{
		{hostProfile{Alias: strings.Repeat("机", 64)}, hostEditOK},
		{hostProfile{Alias: strings.Repeat("机", 65)}, hostAliasLong},
		{hostProfile{Alias: "a\nb"}, hostAliasInvalid},
		{hostProfile{Alias: "a\u2028b"}, hostAliasInvalid},
		{hostProfile{Note: strings.Repeat("机", 500)}, hostEditOK},
		{hostProfile{Note: strings.Repeat("机", 501)}, hostNoteLong},
	} {
		if got := validateHostProfile(c.p); got != c.e {
			t.Errorf("validation = %v, want %v", got, c.e)
		}
	}
}

func TestHostEditorMenusAndKeyboard(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, tt := hostsPage(t)
			t.Cleanup(func() { settingsUI.hostEdit = nil })
			if lang == "zh-Hans" {
				inChinese(t, tt)
			}
			if err := tt.Click(locales.HostActions(a.hostName(nil))); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if err := tt.ChooseMenuItem(locales.HostEdit()); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if settingsUI.hostEdit == nil || !tt.HasText(locales.HostEditTitle()) {
				t.Fatalf("the menu did not open the editor: %q", tt.Texts())
			}
			tt.Type("开发机")
			tt.Key(0, ui.KeyTab)
			tt.Type("训练")
			tt.Key(0, ui.KeyEnter)
			tt.Type("评测")
			tt.Frame()
			if settingsUI.hostEdit.draft.Note != "训练\n评测" {
				t.Fatalf("Enter did not insert a note line: %+v", settingsUI.hostEdit.draft)
			}
			tt.Key(ui.Cmd, ui.KeyEnter)
			tt.Frame()
			if settingsUI.hostEdit != nil || a.hostName(nil) != "开发机" {
				t.Fatal("keyboard save failed")
			}
			if !tt.HasText("开发机") || prefs.HostProfiles["local"].Note != "训练\n评测" {
				t.Fatal("the settings card did not refresh")
			}
			// A dirty backdrop is inert, while Escape explicitly cancels.
			a.openHostEditor(nil, true)
			tt.Frame()
			settingsUI.hostEdit.draft.Alias = "取消的修改"
			tt.ClickAt(2, 2)
			tt.Frame()
			if settingsUI.hostEdit == nil {
				t.Fatal("the backdrop discarded a dirty draft")
			}
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			if settingsUI.hostEdit != nil || a.hostName(nil) != "开发机" {
				t.Fatal("Escape saved or retained the canceled draft")
			}
		})
	}
}

func TestHostEditorOfflineAndRemoved(t *testing.T) {
	a, tt := hostsPage(t)
	t.Cleanup(func() { settingsUI.hostEdit = nil })
	h := newHostConn(savedHost{ID: "offline", Name: "server"})
	h.state = hostFailed
	a.hosts = []*hostConn{h}
	a.openHostEditor(h, true)
	settingsUI.hostEdit.draft = hostProfile{"离线主机", "稍后连接"}
	tt.Frame()
	if err := tt.Click(locales.HostEditSave()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.hostName(h) != "离线主机" || h.state != hostFailed || h.current() != nil {
		t.Fatal("an offline edit opened a connection")
	}
	a.openHostEditor(h, true)
	settingsUI.hostEdit.draft.Alias = "保留草稿"
	a.hosts = nil
	tt.Frame()
	if !tt.HasText(locales.HostEditGone()) {
		t.Fatal("a removed host was not reported")
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if settingsUI.hostEdit == nil || prefs.HostProfiles[h.ID].Alias != "离线主机" {
		t.Fatal("a removed host was saved or its draft was discarded")
	}
}

func TestHostEditorDraftsAndSessionStaySeparate(t *testing.T) {
	isolateSettings(t, "")
	a, tt := newTestApp(t)
	t.Cleanup(func() { settingsUI.hostEdit = nil })
	tt.Frame()
	before, err := a.client.List()
	if err != nil {
		t.Fatal(err)
	}
	a.openHostEditor(nil, false)
	a.hostEdit.draft = hostProfile{"主机", "本机编辑器"}
	a.openHostEditor(nil, true)
	settingsUI.hostEdit.draft.Note = "设置中的草稿"
	if !a.saveHostEditor(a.hostEdit) {
		t.Fatal(a.hostEdit.err.text())
	}
	if settingsUI.hostEdit.draft.Note != "设置中的草稿" {
		t.Fatal("another window's draft was overwritten")
	}
	a.hostEdit = nil
	tt.Frame()
	after, err := a.client.List()
	if err != nil || len(before) != len(after) || len(after) == 0 {
		t.Fatalf("session list changed: %v, %v", after, err)
	}
	if before[0].ID != after[0].ID || before[0].PID != after[0].PID {
		t.Fatal("editing recreated a terminal session")
	}
	if _, err := a.client.Hello(); err != nil {
		t.Fatal("editing disconnected the server")
	}
	// The server identity used in pairing is not the alias.
	if a.hello.Host != (rex.HostInfo{}) && a.localName() == "主机" {
		t.Fatal("the presentation name entered the protocol identity")
	}
}

func TestHostSidebarMenuAndStableWidth(t *testing.T) {
	isolateSettings(t, "")
	a, tt := newTestApp(t)
	prefs.TabPlacement, prefs.SidebarWidth = "left", 180
	h := newHostConn(savedHost{ID: "offline", Name: "office-server"})
	h.state = hostOffline
	a.hosts = []*hostConn{h}
	tt.Frame()
	before, ok := tt.Find(h.Name)
	if !ok {
		t.Fatal("no host header")
	}
	tt.Move(before.X+10, before.Y+4)
	tt.Frame()
	after, _ := tt.Find(h.Name)
	if before.W != after.W || before.X != after.X {
		t.Fatal("hovering made the host title jump")
	}
	if err := tt.RightClick(h.Name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem(locales.HostEdit()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.hostEdit == nil || settingsUI.hostEdit != nil {
		t.Fatal("the sidebar did not open its own editor")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("训练机")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.hostEdit != nil || !tt.HasText("训练机") {
		t.Fatal("Enter did not save and refresh the host title")
	}
}

func TestHostEditorFitsSmallWindow(t *testing.T) {
	a, tt := hostsPage(t)
	t.Cleanup(func() { settingsUI.hostEdit = nil })
	inChinese(t, tt)
	tt.SetSize(620, 400)
	a.openHostEditor(nil, true)
	tt.Frame()
	for _, label := range []string{locales.HostEditTitle(), locales.HostEditAlias(), locales.HostEditSave(), locales.RemoteCancel()} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 621 || r.Y+r.H > 401 {
			t.Errorf("%q is outside the small window: %+v, %v", label, r, ok)
		}
	}
}

func TestDuplicateHostAliasesRemainDistinctInPalette(t *testing.T) {
	isolateSettings(t, "")
	a := &App{}
	for _, name := range []string{"office-one", "office-two"} {
		h := newHostConn(savedHost{ID: name, Name: name})
		a.hosts = append(a.hosts, h)
		if !a.setHostProfile(h.ID, hostProfile{Alias: "服务器"}) {
			t.Fatal("save")
		}
	}
	items := a.newTabOnItems(func(string) bool { return true })
	if len(items) != 3 || items[1].title == items[2].title || !strings.Contains(items[1].title, "office-one") || !strings.Contains(items[2].title, "office-two") {
		t.Fatalf("ambiguous choices: %+v", items)
	}
}
