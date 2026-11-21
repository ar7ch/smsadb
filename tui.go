package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type item struct{ m Message }

func (i item) Title() string {
	return fmt.Sprintf("%s %s  %s", i.m.Dir(), Printable(i.m.Address), i.m.Date.Format("2006-01-02 15:04"))
}
func (i item) Description() string {
	first, _, _ := strings.Cut(Printable(i.m.Body), "\n")
	return first
}
func (i item) FilterValue() string { return i.m.Address + " " + i.m.Body }

type devItem struct{ d DeviceInfo }

func (i devItem) Title() string { return i.d.Name }
func (i devItem) Description() string {
	if i.d.State != "device" {
		return i.d.Serial + "  " + i.d.State + " (accept the USB debugging prompt on the phone)"
	}
	return i.d.Serial
}
func (i devItem) FilterValue() string { return i.d.Name + " " + i.d.Serial }

type loadedMsg struct {
	msgs []Message
	err  error
}

type devicesMsg struct {
	devs []DeviceInfo
	err  error
}

type tuiModel struct {
	ctx     context.Context
	dev     Device
	box     string
	list    list.Model
	devices list.Model
	view    viewport.Model
	detail  bool
	picking bool
	// autoPick selects the phone without showing the picker when only one
	// is attached. Only the first device scan does this.
	autoPick bool
}

func runTUI(ctx context.Context, dev Device, args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	box := fs.String("box", "all", "all, inbox, sent, draft or outbox")
	fs.Parse(args)

	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "SMS  enter: open  r: reload  d: pick phone"
	dl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	dl.Title = "Pick phone  enter: select  r: rescan"
	pick := dev.Serial == ""
	m := tuiModel{ctx: ctx, dev: dev, box: *box, list: l, devices: dl, view: viewport.New(0, 0), picking: pick, autoPick: pick}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func (t tuiModel) load() tea.Msg {
	msgs, err := t.dev.Messages(t.ctx, Query{Box: t.box})
	return loadedMsg{msgs, err}
}

func (t tuiModel) loadDevices() tea.Msg {
	devs, err := Devices(t.ctx)
	return devicesMsg{devs, err}
}

func (t tuiModel) Init() tea.Cmd {
	if t.picking {
		return t.loadDevices
	}
	return t.load
}

func (t tuiModel) selectDevice(d DeviceInfo) (tuiModel, tea.Cmd) {
	t.dev.Serial = d.Serial
	t.picking = false
	t.list.Title = d.Name + "  enter: open  r: reload  d: pick phone"
	return t, tea.Batch(t.list.SetItems(nil), t.list.NewStatusMessage("loading"), t.load)
}

func (t tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.list.SetSize(msg.Width, msg.Height)
		t.devices.SetSize(msg.Width, msg.Height)
		t.view.Width, t.view.Height = msg.Width, msg.Height
	case loadedMsg:
		if msg.err != nil {
			return t, t.list.NewStatusMessage("error: " + msg.err.Error())
		}
		items := make([]list.Item, len(msg.msgs))
		for i, m := range msg.msgs {
			items[i] = item{m}
		}
		return t, t.list.SetItems(items)
	case devicesMsg:
		if msg.err != nil {
			return t, t.devices.NewStatusMessage("error: " + msg.err.Error())
		}
		autoPick := t.autoPick
		t.autoPick = false
		if autoPick && len(msg.devs) == 1 && msg.devs[0].State == "device" {
			return t.selectDevice(msg.devs[0])
		}
		items := make([]list.Item, len(msg.devs))
		for i, d := range msg.devs {
			items[i] = devItem{d}
		}
		cmd := t.devices.SetItems(items)
		if len(items) == 0 {
			cmd = tea.Batch(cmd, t.devices.NewStatusMessage("no phones found, plug one in and press r"))
		}
		return t, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return t, tea.Quit
		}
		if t.detail {
			switch msg.String() {
			case "esc", "q", "backspace":
				t.detail = false
				return t, nil
			}
			var cmd tea.Cmd
			t.view, cmd = t.view.Update(msg)
			return t, cmd
		}
		if t.picking {
			if t.devices.FilterState() != list.Filtering {
				switch msg.String() {
				case "enter":
					if it, ok := t.devices.SelectedItem().(devItem); ok {
						if it.d.State != "device" {
							return t, t.devices.NewStatusMessage(it.d.Serial + " is " + it.d.State)
						}
						return t.selectDevice(it.d)
					}
					return t, nil
				case "r":
					return t, tea.Batch(t.devices.NewStatusMessage("scanning"), t.loadDevices)
				case "esc":
					if t.dev.Serial != "" {
						t.picking = false
						return t, nil
					}
				}
			}
			var cmd tea.Cmd
			t.devices, cmd = t.devices.Update(msg)
			return t, cmd
		}
		if t.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "enter":
				if it, ok := t.list.SelectedItem().(item); ok {
					t.detail = true
					t.view.SetContent(lipgloss.NewStyle().Width(t.view.Width).Render(
						it.Title() + "\n\n" + Printable(it.m.Body) + "\n\n(esc: back)"))
					t.view.GotoTop()
				}
				return t, nil
			case "r":
				return t, tea.Batch(t.list.NewStatusMessage("reloading"), t.load)
			case "d":
				t.picking = true
				return t, t.loadDevices
			}
		}
	}
	var cmd, devCmd tea.Cmd
	t.list, cmd = t.list.Update(msg)
	if _, ok := msg.(tea.KeyMsg); !ok {
		t.devices, devCmd = t.devices.Update(msg)
	}
	return t, tea.Batch(cmd, devCmd)
}

func (t tuiModel) View() string {
	if t.detail {
		return t.view.View()
	}
	if t.picking {
		return t.devices.View()
	}
	return t.list.View()
}
