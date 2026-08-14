package awake

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Dashboard struct {
	Collector func(context.Context) (Snapshot, error)
	Interval  time.Duration
	Width     int
	Snapshot  Snapshot
	Err       error
	QuitStops bool
}
type tickMsg time.Time

func (m Dashboard) Init() tea.Cmd {
	return tea.Batch(m.refresh(), tea.Tick(m.Interval, func(t time.Time) tea.Msg { return tickMsg(t) }))
}
func (m Dashboard) refresh() tea.Cmd {
	return func() tea.Msg { s, err := m.Collector(context.Background()); return snapshotMsg{Snapshot: s, Err: err} }
}

type snapshotMsg struct {
	Snapshot Snapshot
	Err      error
}

func (m Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = v.Width
	case snapshotMsg:
		m.Snapshot, m.Err = v.Snapshot, v.Err
	case tickMsg:
		return m, tea.Batch(m.refresh(), tea.Tick(m.Interval, func(t time.Time) tea.Msg { return tickMsg(t) }))
	case tea.KeyMsg:
		if v.String() == "q" || v.String() == "esc" || v.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}
func (m Dashboard) View() string {
	if m.Err != nil {
		return "awake: " + m.Err.Error() + "\n"
	}
	s := m.Snapshot
	active := "INACTIVE"
	marker := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("○")
	if s.Awake {
		active = "ACTIVE"
		marker = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("●")
	}
	if s.ExternalCaffeinate && !s.Awake {
		active = "EXTERNAL CAFFEINATE"
		marker = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("●")
	}
	title := lipgloss.NewStyle().Bold(true).Render("AWAKE " + marker + " " + active)
	lines := []string{title, fmt.Sprintf("Sleep prevention: %s", boolText(s.SleepAssertion, "active", "inactive")), fmt.Sprintf("Caffeinate PID: %s", pidText(s.CaffeinatePID)), fmt.Sprintf("Power: %s  CPU: %.1f%%", s.PowerSource, s.CPUPercent), fmt.Sprintf("RAM: %s / %s (%.0f%%)", bytes(s.MemoryUsedBytes), bytes(s.MemoryTotalBytes), s.MemoryPercent), fmt.Sprintf("Swap: %s  Load: %.2f %.2f %.2f", bytes(s.SwapUsedBytes), s.LoadAverage[0], s.LoadAverage[1], s.LoadAverage[2]), fmt.Sprintf("Tailscale: %s  SSH :22: %s", s.Tailscale, s.SSH), fmt.Sprintf("Temperature: unavailable  Thermal: %s", s.ThermalState), "Uptime: " + uptime(s.UptimeSeconds), "", "q / Esc / Ctrl+C to exit"}
	if m.Width > 0 && m.Width < 44 {
		lines = []string{title, fmt.Sprintf("Sleep: %s", boolText(s.SleepAssertion, "active", "inactive")), fmt.Sprintf("CPU %.1f%%  RAM %.0f%%", s.CPUPercent, s.MemoryPercent), fmt.Sprintf("Tail: %s  SSH: %s", s.Tailscale, s.SSH), "q to exit"}
	}
	return strings.Join(lines, "\n") + "\n"
}
func boolText(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}
func pidText(pid int) string {
	if pid == 0 {
		return "-"
	}
	return fmt.Sprint(pid)
}
func bytes(v uint64) string {
	const g = 1024 * 1024 * 1024
	return fmt.Sprintf("%.1f GB", float64(v)/g)
}
func uptime(sec uint64) string {
	d := time.Duration(sec) * time.Second
	return fmt.Sprintf("%02dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}
