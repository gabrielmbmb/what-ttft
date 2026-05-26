package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Quit            key.Binding
	Confirm         key.Binding
	Cancel          key.Binding
	Help            key.Binding
	FocusNext       key.Binding
	FocusPrev       key.Binding
	TargetUp        key.Binding
	TargetDown      key.Binding
	ToggleTarget    key.Binding
	ShowAllTargets  key.Binding
	RequestExplorer key.Binding
	ExplorerBack    key.Binding
	FilterRequests  key.Binding
	ClearFilter     key.Binding
	PageUp          key.Binding
	PageDown        key.Binding
	Home            key.Binding
	End             key.Binding
	Enter           key.Binding
	Summary         key.Binding
	TTFT            key.Binding
	E2E             key.Binding
	Waterfall       key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit/cancel"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "confirm"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("n", "esc"),
			key.WithHelp("esc", "back"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		FocusNext: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next pane"),
		),
		FocusPrev: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev pane"),
		),
		TargetUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "target up"),
		),
		TargetDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "target down"),
		),
		ToggleTarget: key.NewBinding(
			key.WithKeys("space"),
			key.WithHelp("space", "toggle model"),
		),
		ShowAllTargets: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "show all"),
		),
		RequestExplorer: key.NewBinding(
			key.WithKeys("5", "r"),
			key.WithHelp("5/r", "requests"),
		),
		ExplorerBack: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		FilterRequests: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter requests"),
		),
		ClearFilter: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("ctrl+u", "clear filter"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
		Home: key.NewBinding(
			key.WithKeys("home"),
			key.WithHelp("home", "first"),
		),
		End: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("end", "last"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "target detail"),
		),
		Summary: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "summary"),
		),
		TTFT: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "ttft"),
		),
		E2E: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "e2e/tps"),
		),
		Waterfall: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "waterfall"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.FocusNext, k.Summary, k.RequestExplorer, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Summary, k.TTFT, k.E2E, k.Waterfall, k.RequestExplorer},
		{k.FocusNext, k.FocusPrev, k.TargetUp, k.TargetDown},
		{k.PageUp, k.PageDown, k.Home, k.End},
		{k.FilterRequests, k.ClearFilter, k.ToggleTarget, k.ShowAllTargets, k.Enter, k.ExplorerBack, k.Help, k.Quit},
		{k.Confirm, k.Cancel},
	}
}
