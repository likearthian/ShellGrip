package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// sshDraft is the form that adds or edits a connection, with its fields
// as text while the user types.
type sshDraft struct {
	ID       string
	Alias    string
	Name     string
	Host     string
	Port     string
	User     string
	Key      string
	Jump     string
	StartDir string
	Options  string
	NoFollow bool
	err      string
}

func newSSHDraft(c sshConnection) *sshDraft {
	port := c.Port
	if port == 0 {
		port = 22
	}
	return &sshDraft{
		ID: c.ID, Alias: c.Alias, Name: c.Name, Host: c.Host, Port: strconv.Itoa(port),
		User: c.User, Key: c.Key, Jump: c.Jump, StartDir: c.StartDir,
		Options: strings.Join(c.Options, ", "), NoFollow: c.NoFollow,
	}
}

// connection reads the form, and reports what must change before it can
// be saved.
func (d *sshDraft) connection() (sshConnection, error) {
	port := 22
	if p := strings.TrimSpace(d.Port); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return sshConnection{}, errors.New("the port must be a number")
		}
		port = n
	}
	var options []string
	for _, o := range strings.Split(d.Options, ",") {
		if o = strings.TrimSpace(o); o != "" {
			options = append(options, o)
		}
	}
	c := sshConnection{
		ID: strings.TrimSpace(d.ID), Name: strings.TrimSpace(d.Name),
		Alias: strings.TrimSpace(d.Alias), Host: strings.TrimSpace(d.Host), Port: port,
		User: strings.TrimSpace(d.User), Key: strings.TrimSpace(d.Key),
		Jump: strings.TrimSpace(d.Jump), StartDir: strings.TrimSpace(d.StartDir),
		Options: options, NoFollow: d.NoFollow,
	}
	if c.ID == "" {
		c.ID = newSSHID()
	}
	if c.Name == "" {
		c.Name = c.label()
	}
	if err := c.valid(); err != nil {
		return sshConnection{}, err
	}
	return c, nil
}

// openSSHSettings shows the connections in Settings, a new form open when
// edit is set.
func (a *App) openSSHSettings(edit bool) {
	a.profilesOpen = false
	a.sshNotice = ""
	a.settingsCategory = "SSH"
	a.openSettings()
	a.settingsCategory = "SSH"
	if edit {
		a.sshDraft = &sshDraft{Port: "22"}
	}
}

// sshSettings lists the connections and the form that edits one.
func (a *App) sshSettings(c *ui.Context, k *colors) {
	ui.Column(c).Gap(14).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "SSH connections").FontSize(13).FontWeight(600).TextColor(k.text).Grow(1)
			if ui.Button(c, "New connection").Clicked() {
				a.sshDraft = &sshDraft{Port: "22"}
				a.sshNotice = ""
			}
			if ui.Button(c, "Import from ~/.ssh/config").Clicked() {
				n, err := a.importSSHConfig()
				switch {
				case err != nil:
					a.sshNotice = "Could not read ~/.ssh/config: " + err.Error()
				case n == 0:
					a.sshNotice = "No new hosts in ~/.ssh/config."
				default:
					a.sshNotice = fmt.Sprintf("Imported %d connection(s).", n)
				}
			}
		})
		ui.Text(c, "Open one from + in the title bar. The Files panel of an SSH tab browses the host.").
			FontSize(11.5).TextColor(k.textFaint)
		if a.sshNotice != "" {
			ui.Text(c, a.sshNotice).FontSize(11.5).TextColor(k.textMuted)
		}
		if len(a.sshConns) == 0 {
			ui.Row(c).Padding(12).Radius(8).Background(k.hover).Children(func() {
				ui.Text(c, "No SSH connections yet. Add one, or import your ssh config.").
					FontSize(12.5).TextColor(k.textMuted)
			})
		}
		for _, conn := range a.sshConns {
			a.sshConnectionRow(c, k, conn)
		}
		if a.sshDraft != nil {
			a.sshForm(c, k)
		}
	})
}

func (a *App) sshConnectionRow(c *ui.Context, k *colors, conn sshConnection) {
	ui.Row(c.Key(conn.ID)).Padding(10).Radius(8).Gap(10).AlignItems(ui.Center).Background(k.hover).Children(func() {
		ui.Icon(c, icon("globe")).Size(17, 17).Shrink(0).TextColor(k.iconMuted)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, conn.label()).FontSize(13).TextColor(k.text).SingleLine().Ellipsis("…")
			if detail := conn.detail(); detail != "" && detail != conn.label() {
				ui.Text(c, detail).FontSize(11.5).TextColor(k.textFaint).SingleLine().Ellipsis("…")
			}
		})
		if ui.Button(c, "Connect").Clicked() {
			a.newSSHTab(conn)
		}
		if ui.Button(c, "Edit").Clicked() {
			a.sshDraft = newSSHDraft(conn)
			a.sshNotice = ""
		}
		if ui.Button(c, "Remove").Clicked() {
			a.removeSSH(conn.ID)
			if a.sshDraft != nil && a.sshDraft.ID == conn.ID {
				a.sshDraft = nil
			}
		}
	})
}

func (a *App) sshForm(c *ui.Context, k *colors) {
	d := a.sshDraft
	ui.Column(c).Gap(10).Padding(14).Radius(10).Background(k.hover).Children(func() {
		ui.Text(c, "Connection").FontSize(13).FontWeight(600).TextColor(k.text)
		field := func(label string, value *string, placeholder string) {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(k.textMuted).Width(118)
				ui.TextInput(c, value).Grow(1).FontSize(12.5).Placeholder(placeholder)
			})
		}
		field("Name", &d.Name, "omni")
		field("SSH config alias", &d.Alias, "optional: a Host of ~/.ssh/config")
		field("Host", &d.Host, "10.0.0.5 or example.com")
		field("Port", &d.Port, "22")
		field("User", &d.User, "root")
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Identity file").FontSize(12).TextColor(k.textMuted).Width(118)
			ui.TextInput(c, &d.Key).Grow(1).FontSize(12.5).Placeholder("~/.ssh/id_ed25519")
			if ui.Button(c, "Browse…").Clicked() {
				a.browseSSHKey()
			}
		})
		field("ProxyJump", &d.Jump, "optional: user@bastion")
		field("Start folder", &d.StartDir, "optional: /var/www")
		field("Extra options", &d.Options, "comma separated, e.g. Compression=yes")
		follow := !d.NoFollow
		if ui.Checkbox(c, &follow, "Follow the shell's folder").Changed() {
			d.NoFollow = !follow
		}
		ui.Text(c, "Installs a prompt hook for bash, zsh and fish, so the Files tree follows cd. "+
			"Turn it off for hosts without a POSIX shell (Windows).").
			FontSize(11).TextColor(k.textFaint)
		ui.Text(c, "An alias wins over the fields above: ssh resolves the host, user, port and keys of its own config.").
			FontSize(11).TextColor(k.textFaint)
		if d.err != "" {
			ui.Text(c, d.err).FontSize(11.5).TextColor(ui.Hex("#ef4444"))
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "Save connection").Clicked() {
				a.saveSSHDraft(false)
			}
			if ui.Button(c, "Save and connect").Clicked() {
				a.saveSSHDraft(true)
			}
			ui.Spacer(c)
			if d.ID != "" {
				if ui.Button(c, "Delete").Clicked() {
					a.removeSSH(d.ID)
					a.sshDraft = nil
				}
			}
			if ui.Button(c, "Cancel").Clicked() {
				a.sshDraft = nil
			}
		})
	})
}

func (a *App) saveSSHDraft(connect bool) {
	d := a.sshDraft
	if d == nil {
		return
	}
	conn, err := d.connection()
	if err != nil {
		d.err = err.Error()
		return
	}
	a.upsertSSH(conn)
	a.sshDraft = nil
	a.sshNotice = "Saved " + conn.label() + "."
	if connect {
		a.newSSHTab(conn)
	}
}

// browseSSHKey asks for an identity file, off the frame that asked, as
// native dialogs block until they close.
func (a *App) browseSSHKey() {
	if a.win == nil {
		return
	}
	opts := mygo.OpenDialogOptions{Title: "Choose an SSH identity file", Parent: a.win}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if err != nil || len(paths) == 0 {
			return
		}
		a.post(func() {
			if a.sshDraft != nil {
				a.sshDraft.Key = paths[0]
			}
		})
	}()
}
