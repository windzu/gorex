package main

import (
	"log"
	"maps"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
	"gorex/locales"
)

// A profile is local presentation, never a machine's network identity.
type hostProfile struct {
	Alias string `json:"alias,omitempty"`
	Note  string `json:"note,omitempty"`
}

func hostProfileID(h *hostConn) string {
	if h == nil {
		return "local"
	}
	return h.ID
}

func hostDisplayName(h *hostConn, fallback string) string {
	if alias := prefs.HostProfiles[hostProfileID(h)].Alias; alias != "" {
		return alias
	}
	return fallback
}

// Choices with the same alias keep their source names for identification.
func (a *App) hostChoiceName(h *hostConn) string {
	name := a.hostName(h)
	for _, other := range a.allHosts() {
		if other != h && a.hostName(other) == name {
			source := a.localName()
			if h != nil {
				source = h.Name
			}
			return name + " · " + source
		}
	}
	return name
}

// setHostProfile merges only this record into the latest preferences.
// Discovery can drop and recreate a connection without losing the record.
func (a *App) setHostProfile(id string, profile hostProfile) bool {
	if prefs.HostProfiles[id] == profile {
		return true
	}
	next := prefs
	next.HostProfiles = maps.Clone(prefs.HostProfiles)
	if profile == (hostProfile{}) {
		delete(next.HostProfiles, id)
	} else {
		if next.HostProfiles == nil {
			next.HostProfiles = map[string]hostProfile{}
		}
		next.HostProfiles[id] = profile
	}
	if err := writeSettings(next); err != nil {
		log.Printf("gorex: saving host information: %v", err)
		return false
	}
	prefs = next
	if a.st != nil {
		a.st.redraw()
	} else {
		if a.win != nil {
			a.win.Update(func() {})
		}
		if w := settingsUI.win; w != nil {
			w.Update(func() {})
		}
	}
	return true
}

type hostEditError int

const (
	hostEditOK hostEditError = iota
	hostAliasLong
	hostAliasInvalid
	hostNoteLong
	hostEditGone
	hostEditWriteFailed
)

func (e hostEditError) text() string {
	switch e {
	case hostAliasLong:
		return locales.HostEditAliasLong()
	case hostAliasInvalid:
		return locales.HostEditAliasInvalid()
	case hostNoteLong:
		return locales.HostEditNoteLong()
	case hostEditGone:
		return locales.HostEditGone()
	case hostEditWriteFailed:
		return locales.HostEditSaveFailed()
	}
	return ""
}

func validateHostProfile(p hostProfile) hostEditError {
	if utf8.RuneCountInString(p.Alias) > 64 {
		return hostAliasLong
	}
	for _, r := range p.Alias {
		if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return hostAliasInvalid
		}
	}
	if utf8.RuneCountInString(p.Note) > 500 {
		return hostNoteLong
	}
	return hostEditOK
}

// Each window owns its draft; opening Settings cannot overwrite a draft
// in a terminal window. The profile itself is shared by every window.
type hostEditor struct {
	id      string
	source  *hostConn
	draft   hostProfile
	initial hostProfile
	err     hostEditError
}

func (a *App) openHostEditor(h *hostConn, inSettings bool) {
	id := hostProfileID(h)
	p := &hostEditor{id: id, source: h, draft: prefs.HostProfiles[id], initial: prefs.HostProfiles[id]}
	a.hostOpen, a.focusReq = false, nil
	if inSettings {
		settingsUI.hostEdit = p
	} else {
		a.hostEdit = p
	}
}

func (a *App) saveHostEditor(p *hostEditor) bool {
	profile := hostProfile{Alias: strings.TrimSpace(p.draft.Alias), Note: strings.TrimSpace(p.draft.Note)}
	p.err = validateHostProfile(profile)
	if p.err != hostEditOK {
		return false
	}
	if p.id != "local" && a.hostByID(p.id) == nil {
		p.err = hostEditGone
		return false
	}
	if !a.setHostProfile(p.id, profile) {
		p.err = hostEditWriteFailed
		return false
	}
	return true
}

func hostSource(h *hostConn) string {
	switch {
	case h == nil:
		return thisMachine()
	case rex.IsTailnetTarget(h.Address):
		return locales.HostSourceTailnet()
	case h.Network:
		return locales.HostSourceNetwork()
	default:
		return locales.HostSourceManual()
	}
}

// hostActions is shared by the header, its context menu and Settings.
func (a *App) hostActions(m *ui.Menu, h *hostConn, inSettings bool) {
	if m.Item(locales.HostEdit()).Chosen() {
		a.openHostEditor(h, inSettings)
	}
	if h != nil && h.state == hostConnected {
		if m.Item(locales.HostsLayout()).Chosen() {
			a.hostOpen = false
			a.openSettingsAt("hosts")
			a.openHostLayout(h)
		}
	}
	if h != nil && !rex.IsTailnetTarget(h.Address) && h.state != hostConnected {
		if m.Item(locales.HostsRetry()).Chosen() {
			h.poke()
		}
	}
	if !inSettings {
		m.Separator()
		if m.Item(locales.HostsManage()).Chosen() {
			a.hostOpen = false
			a.openSettingsAt("hosts")
		}
	}
}

func (a *App) hostActionsButton(c *ui.Context, k *colors, h *hostConn, inSettings bool, size float32) ui.Element {
	label := locales.HostActions(a.hostName(h))
	b := iconButton(c, k, "ellipsis", label, size, 15).Tooltip(label).Shrink(0)
	build := func(m *ui.Menu) { a.hostActions(m, h, inSettings) }
	if drawMenus {
		return b.PopoverMenuButton(build)
	}
	return b.Menu(build)
}

// The sheet uses the existing form theme in both kinds of window.
func (a *App) hostEditorSheet(c *ui.Context, k *colors, target **hostEditor) {
	p := *target
	if p == nil {
		return
	}
	h := p.source
	missing := false
	if p.id != "local" {
		if current := a.hostByID(p.id); current != nil {
			h = current
		} else {
			missing = true
		}
	}
	baseName, device := a.localName(), a.localName()
	state, _, _ := a.serverState(k)
	if h != nil {
		baseName, device = h.Name, h.hello.Host.Name
		state, _, _ = hostStatus(h)
	}
	if device == "" {
		device = locales.HostEditUnknown()
	}
	base := c.Theme()
	c.SetTheme(settingsTheme(base, k))
	defer c.SetTheme(base)
	open := true // Dirty drafts ignore backdrop clicks; Escape still cancels.
	ui.DialogBase(c.Key("host-editor"), &open, func(backdrop, panel ui.Element) {
		if panel.Dismissed() && (!backdrop.Clicked() || p.draft == p.initial) {
			*target = nil
			return
		}
		w, height := c.Size()
		backdrop.Background(k.backdrop)
		glassPanel(panel.Width(min(440, w-32)).MaxHeight(height-32).Padding(18).Gap(12).Radius(16), k.panel).
			Border(1, k.panelBorder).Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).
			Role(ui.RoleDialog).Label(locales.HostEditTitle())
		ui.Text(c, locales.HostEditTitle()).FontSize(15).FontWeight(700).TextColor(k.text)
		submit := false
		ui.Scroll(c).MinHeight(0).Children(func() {
			ui.Column(c).Gap(12).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, a.hostName(h)).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…")
					ui.Text(c, hostSource(h)+" · "+state).FontSize(12).TextColor(k.textMuted)
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Children(func() {
						ui.Text(c, locales.HostEditAlias()).FontSize(12).FontWeight(600).TextColor(k.textMuted).Grow(1)
						if ui.Button(c, locales.HostEditClearAlias()).FontSize(11.5).Padding(3, 8).Clicked() {
							p.draft.Alias, p.err = "", hostEditOK
						}
					})
					in := ui.TextInput(c.Key("alias"), &p.draft.Alias).Placeholder(baseName).Label(locales.HostEditAlias()).AutoFocus()
					if in.Changed() {
						p.err = hostEditOK
					}
					submit = in.Submitted()
					ui.Text(c, locales.HostEditHint()).FontSize(12).TextColor(k.textMuted).LineHeight(1.3)
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, locales.HostEditNote()).FontSize(12).FontWeight(600).TextColor(k.textMuted)
					if ui.TextArea(c.Key("note"), &p.draft.Note).Height(68).FontSize(13).Radius(12).
						Label(locales.HostEditNote()).Changed() {
						p.err = hostEditOK
					}
				})
				ui.Divider(c)
				fact := func(label, value string) {
					ui.Row(c).Gap(12).Children(func() {
						ui.Text(c, label).FontSize(12).TextColor(k.textMuted).Shrink(0)
						ui.Text(c, value).FontSize(12).TextColor(k.text).Grow(1).MinWidth(0).TextAlign(ui.End).
							SingleLine().Ellipsis("…").Tooltip(value).Selectable()
					})
				}
				fact(locales.HostEditDeviceName(), device)
				if h != nil && baseName != device {
					fact(locales.HostEditSourceName(), baseName)
				}
				if missing {
					ui.Text(c, locales.HostEditGone()).FontSize(12).TextColor(k.danger).LineHeight(1.3)
				} else if p.err != hostEditOK {
					ui.Text(c, p.err.text()).FontSize(12).TextColor(k.danger).LineHeight(1.3)
				}
			})
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c.Key("cancel"), locales.RemoteCancel()).Clicked() {
				*target = nil
			}
			if ui.Button(c.Key("save"), locales.HostEditSave()).Disabled(missing).Clicked() {
				submit = true
			}
		})
		submit = panel.OverlayShortcut(ui.Cmd, ui.KeyEnter) || submit
		if submit && !missing && a.saveHostEditor(p) {
			*target = nil
		}
	})
}
