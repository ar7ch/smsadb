package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type item struct{ m Message }

func (i item) Title() string {
	return fmt.Sprintf("%s%s %s  %s", i.m.Mark(), i.m.Dir(), Printable(i.m.Address), i.m.Date.Format("2006-01-02 15:04"))
}
func (i item) Description() string {
	first, _, _ := strings.Cut(Printable(i.m.Body), "\n")
	return first
}
func (i item) FilterValue() string { return i.m.Address + " " + i.m.Body }

// msgDelegate draws unread messages in bold accent colors.
type msgDelegate struct{ read, unread list.DefaultDelegate }

func newMsgDelegate() msgDelegate {
	d := msgDelegate{list.NewDefaultDelegate(), list.NewDefaultDelegate()}
	accent := lipgloss.AdaptiveColor{Light: "#1D4ED8", Dark: "#60A5FA"}
	st := &d.unread.Styles
	st.NormalTitle = st.NormalTitle.Bold(true).Foreground(accent)
	st.NormalDesc = st.NormalDesc.Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})
	st.SelectedTitle = st.SelectedTitle.Bold(true)
	return d
}

func (d msgDelegate) Height() int                               { return d.read.Height() }
func (d msgDelegate) Spacing() int                              { return d.read.Spacing() }
func (d msgDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d msgDelegate) Render(w io.Writer, m list.Model, index int, it list.Item) {
	if i, ok := it.(item); ok && !i.m.Read {
		d.unread.Render(w, m, index, it)
		return
	}
	d.read.Render(w, m, index, it)
}

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
	seq  int
	err  error
}

type tickMsg struct{}

const reloadEvery = 15 * time.Second

func tick() tea.Cmd {
	return tea.Tick(reloadEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

type markedMsg struct {
	serial string
	id     int64
	err    error
}

type devicesMsg struct {
	devs []DeviceInfo
	err  error
}

type tuiModel struct {
	ctx     context.Context
	dev     Device
	box     string
	unread  bool
	list    list.Model
	devices list.Model
	view    viewport.Model
	detail  bool
	opened  item // message shown in the detail view
	picking bool
	loading bool
	loadSeq int // bumped by each reload, only the latest result is shown
	// autoPick selects the phone without showing the picker when only one
	// is attached. Only the first device scan does this.
	autoPick bool
}

func runTUI(ctx context.Context, dev Device, args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	box := fs.String("box", "all", "all, inbox, sent, draft or outbox")
	fs.Parse(args)

	l := list.New(nil, newMsgDelegate(), 0, 0)
	l.Title = "SMS  enter: open  r: reload  R: mark read  u: unread  d: pick phone"
	dl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	dl.Title = "Pick phone  enter: select  r: rescan"
	pick := dev.Serial == ""
	m := tuiModel{ctx: ctx, dev: dev, box: *box, list: l, devices: dl, view: viewport.New(0, 0), picking: pick, autoPick: pick}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func (t tuiModel) load() tea.Msg {
	msgs, err := t.dev.Messages(t.ctx, Query{Box: t.box, Unread: t.unread})
	return loadedMsg{msgs, t.loadSeq, err}
}

// markRead marks a message as read on the phone.
func (t tuiModel) markRead(it item, ok bool) tea.Cmd {
	if !ok || it.m.Read {
		return nil
	}
	dev, id := t.dev, it.m.ID
	return func() tea.Msg { return markedMsg{dev.Serial, id, dev.MarkRead(t.ctx, id)} }
}

func (t tuiModel) loadDevices() tea.Msg {
	devs, err := Devices(t.ctx)
	return devicesMsg{devs, err}
}

// reload starts loading messages and shows the spinner until they arrive.
func (t *tuiModel) reload() tea.Cmd {
	t.loading = true
	t.loadSeq++
	return tea.Batch(t.list.StartSpinner(), t.load)
}

func (t tuiModel) Init() tea.Cmd {
	if t.picking {
		return tea.Batch(t.loadDevices, tick())
	}
	// the first tickMsg starts the first load
	return func() tea.Msg { return tickMsg{} }
}

func (t tuiModel) selectDevice(d DeviceInfo) (tuiModel, tea.Cmd) {
	t.dev.Serial = d.Serial
	t.picking = false
	t.list.Title = d.Name + "  enter: open  r: reload  R: mark read  u: unread  d: pick phone"
	cmd := t.reload()
	return t, tea.Batch(t.list.SetItems(nil), cmd)
}

func (t tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.list.SetSize(msg.Width, msg.Height)
		t.devices.SetSize(msg.Width, msg.Height)
		t.view.Width, t.view.Height = msg.Width, msg.Height
	case tickMsg:
		if t.picking || t.loading {
			return t, tick()
		}
		cmd := t.reload()
		return t, tea.Batch(tick(), cmd)
	case loadedMsg:
		if msg.seq != t.loadSeq {
			return t, nil
		}
		t.loading = false
		t.list.StopSpinner()
		if msg.err != nil {
			return t, t.list.NewStatusMessage("error: " + msg.err.Error())
		}
		sel, hadSel := t.list.SelectedItem().(item)
		items := make([]list.Item, len(msg.msgs))
		for i, m := range msg.msgs {
			items[i] = item{m}
		}
		cmd := t.list.SetItems(items)
		// keep the cursor on the same message when new ones arrive on top.
		// While filtering, SetItems refilters asynchronously, so skip it.
		if hadSel && t.list.FilterState() == list.Unfiltered {
			for i, li := range items {
				if li.(item).m.ID == sel.m.ID {
					t.list.Select(i)
					break
				}
			}
		}
		return t, cmd
	case markedMsg:
		if msg.serial != t.dev.Serial {
			return t, nil
		}
		if msg.err != nil {
			return t, t.list.NewStatusMessage("error: " + msg.err.Error())
		}
		for i, li := range t.list.Items() {
			if it, ok := li.(item); ok && it.m.ID == msg.id {
				if t.unread {
					// SetItems refilters, RemoveItem leaves stale filter indexes
					items := slices.Delete(t.list.Items(), i, i+1)
					return t, t.list.SetItems(items)
				}
				it.m.Read = true
				return t, t.list.SetItem(i, it)
			}
		}
		return t, nil
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
			case "R":
				t.detail = false
				return t, t.markRead(t.opened, true)
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
					t.opened = it
					t.view.SetContent(lipgloss.NewStyle().Width(t.view.Width).Render(
						it.Title() + "\n\n" + Printable(it.m.Body) + "\n\n(esc: back  R: mark read)"))
					t.view.GotoTop()
				}
				return t, nil
			case "r":
				cmd := t.reload()
				return t, cmd
			case "R":
				it, ok := t.list.SelectedItem().(item)
				return t, t.markRead(it, ok)
			case "u":
				t.unread = !t.unread
				status := "all messages"
				if t.unread {
					status = "unread only"
				}
				cmd := t.reload()
				return t, tea.Batch(t.list.NewStatusMessage(status), cmd)
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
