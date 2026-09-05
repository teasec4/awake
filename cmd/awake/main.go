package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/teasec4/awake/internal/awake"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

var version = "dev"
var commit = "none"
var buildTime = "unknown"

type app struct {
	home     string
	store    *awake.StateStore
	runner   awake.Runner
	power    awake.PowerManager
	metrics  awake.MetricsProvider
	network  awake.NetworkStatusProvider
	interval time.Duration
	verbose  bool
	noColor  bool
}

func newApp() *app {
	home, _ := os.UserHomeDir()
	store := awake.NewStateStore(home)
	r := awake.ExecRunner{}
	return &app{home: home, store: store, runner: r, power: &awake.SystemPower{Store: store, Runner: r}, metrics: awake.GopsutilMetrics{}, network: awake.SystemNetwork{Runner: r}, interval: time.Second}
}
func main() {
	a := newApp()
	root := rootCmd(a)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "awake:", err)
		os.Exit(1)
	}
}
func rootCmd(a *app) *cobra.Command {
	root := &cobra.Command{Use: "awake", Short: "Keep a macOS session awake and observable", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().BoolVar(&a.verbose, "verbose", false, "show diagnostic details")
	root.PersistentFlags().DurationVar(&a.interval, "interval", time.Second, "refresh interval")
	root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable colour output")
	var startFlag bool
	root.Flags().BoolVar(&startFlag, "start", false, "start awake in the background (same as 'awake start')")
	root.PersistentPreRun = func(*cobra.Command, []string) {
		if a.noColor {
			lipgloss.SetColorProfile(termenv.Ascii)
		}
	}
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if startFlag {
			return a.start(cmd.Context())
		}
		return cmd.Help()
	}
	root.AddCommand(startCmd(a), runCmd(a), statusCmd(a), statsCmd(a), stopCmd(a), installCmd(a), uninstallCmd(a), doctorCmd(a), versionCmd())
	return root
}
func startCmd(a *app) *cobra.Command {
	return &cobra.Command{Use: "start", Short: "Start awake in the background", RunE: func(cmd *cobra.Command, args []string) error { return a.start(cmd.Context()) }}
}
func (a *app) start(ctx context.Context) error {
	running, err := a.store.Running()
	if err != nil {
		return err
	}
	if running {
		return errors.New("awake is already running")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := a.store.Ensure(); err != nil {
		return err
	}
	out, err := os.OpenFile(filepath.Join(a.store.LogsDir(), "awake.start.out.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	errLog, err := os.OpenFile(filepath.Join(a.store.LogsDir(), "awake.start.err.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		_ = out.Close()
		return err
	}
	cmd := exec.Command(exe, "run", "--no-tui")
	cmd.Stdout = out
	cmd.Stderr = errLog
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		_ = out.Close()
		_ = errLog.Close()
		return fmt.Errorf("start background supervisor: %w", err)
	}
	_ = out.Close()
	_ = errLog.Close()
	if err = a.store.SaveSupervisor(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	fmt.Printf("awake: started (supervisor PID %d)\nlogs: %s\n", cmd.Process.Pid, a.store.LogsDir())
	return nil
}
func runCmd(a *app) *cobra.Command {
	var noTUI bool
	c := &cobra.Command{Use: "run", Short: "Run caffeinate -is in the foreground", RunE: func(cmd *cobra.Command, args []string) error { return a.run(cmd.Context(), noTUI) }}
	c.Flags().BoolVar(&noTUI, "no-tui", false, "run without the live terminal UI")
	return c
}
func (a *app) run(ctx context.Context, noTUI bool) error {
	release, err := a.store.Acquire()
	if err != nil {
		return err
	}
	defer release()
	info, err := a.power.Start(ctx)
	if err != nil {
		return err
	}
	defer a.power.Stop(context.Background())
	if noTUI {
		fmt.Printf("awake: caffeinate PID %d active\n", info.PID)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sig)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sig:
			return nil
		}
	}
	p := tea.NewProgram(awake.Dashboard{Collector: a.snapshot, Interval: a.interval, QuitStops: true})
	_, err = p.Run()
	return err
}
func (a *app) snapshot(ctx context.Context) (awake.Snapshot, error) {
	var out awake.Snapshot
	p, perr := a.power.Status(ctx)
	m, merr := a.metrics.Snapshot(ctx)
	tail, terr := a.network.Tailscale(ctx)
	ssh, serr := a.network.SSH(ctx)
	power := "Unknown"
	if b, e := a.runner.Output(ctx, "pmset", "-g", "batt"); e == nil {
		power = awake.ParsePowerSource(string(b))
	}
	out = awake.Snapshot{Awake: p.PID > 0 && p.AssertionActive, CaffeinatePID: p.PID, SleepAssertion: p.AssertionActive, ExternalCaffeinate: p.ExternalCaffeinate, PowerSource: power, CPUPercent: m.CPUPercent, MemoryUsedBytes: m.MemoryUsedBytes, MemoryTotalBytes: m.MemoryTotalBytes, MemoryPercent: m.MemoryPercent, SwapUsedBytes: m.SwapUsedBytes, LoadAverage: m.LoadAverage, Tailscale: tail, SSH: ssh, Temperature: m.Temperature, ThermalState: m.ThermalState, UptimeSeconds: m.UptimeSeconds, UpdatedAt: time.Now().UTC()}
	_ = a.store.SaveSnapshot(out)
	for _, e := range []error{perr, merr, terr, serr} {
		if e != nil {
			return out, e
		}
	}
	return out, nil
}
func statusCmd(a *app) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{Use: "status", Short: "Show sleep prevention and machine status", RunE: func(cmd *cobra.Command, args []string) error {
		s, e := a.snapshot(cmd.Context())
		if asJSON {
			b, _ := json.MarshalIndent(s, "", "  ")
			fmt.Println(string(b))
			return e
		}
		fmt.Print(statusText(s))
		return e
	}}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return c
}
func statsCmd(a *app) *cobra.Command {
	var watch bool
	c := &cobra.Command{Use: "stats", Short: "Show live machine statistics", RunE: func(cmd *cobra.Command, args []string) error {
		if !watch {
			s, e := a.snapshot(cmd.Context())
			fmt.Print(statusText(s))
			return e
		}
		_, e := tea.NewProgram(awake.Dashboard{Collector: a.snapshot, Interval: a.interval}).Run()
		return e
	}}
	c.Flags().BoolVar(&watch, "watch", false, "refresh continuously")
	return c
}
func stopCmd(a *app) *cobra.Command {
	return &cobra.Command{Use: "stop", Short: "Stop awake's background supervisor and caffeinate", RunE: func(c *cobra.Command, args []string) error {
		if _, err := a.stopSupervisor(c.Context()); err != nil {
			return err
		}
		if err := a.power.Stop(c.Context()); err != nil {
			return err
		}
		fmt.Println("awake: stopped")
		return nil
	}}
}
func (a *app) stopSupervisor(ctx context.Context) (bool, error) {
	pid, err := a.store.LoadSupervisor()
	if err != nil {
		return false, err
	}
	if pid <= 0 || !alive(pid) {
		_ = a.store.ClearSupervisor()
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	b, err := a.runner.Output(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	if err != nil {
		return false, fmt.Errorf("verify supervisor PID %d: %w", pid, err)
	}
	if filepath.Base(strings.TrimSpace(string(b))) != filepath.Base(exe) {
		return false, fmt.Errorf("refusing to stop PID %d: it is not the awake supervisor", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return false, fmt.Errorf("stop supervisor PID %d: %w", pid, err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if !alive(pid) {
			_ = a.store.ClearSupervisor()
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return false, fmt.Errorf("supervisor PID %d did not exit within 5s", pid)
		case <-tick.C:
		}
	}
}
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}
func installCmd(a *app) *cobra.Command {
	var dry bool
	c := &cobra.Command{Use: "install", Short: "Install the user LaunchAgent", RunE: func(cmd *cobra.Command, args []string) error {
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		bin, err = filepath.Abs(bin)
		if err != nil {
			return err
		}
		path := awake.LaunchAgentPath(a.home)
		fmt.Printf("LaunchAgent: %s\nProgram: %s run --no-tui\n", path, bin)
		if dry {
			return nil
		}
		if _, err = awake.WriteLaunchAgent(a.home, bin, a.store); err != nil {
			return err
		}
		uid := strconv.Itoa(os.Getuid())
		_, _ = a.runner.Output(cmd.Context(), "launchctl", "bootout", "gui/"+uid, path)
		if _, err = a.runner.Output(cmd.Context(), "launchctl", "bootstrap", "gui/"+uid, path); err != nil {
			return fmt.Errorf("install LaunchAgent: %w", err)
		}
		fmt.Println("awake: LaunchAgent installed. It runs after user login; FileVault still requires unlock after reboot.")
		return nil
	}}
	c.Flags().BoolVar(&dry, "dry-run", false, "show without writing or loading")
	return c
}
func uninstallCmd(a *app) *cobra.Command {
	return &cobra.Command{Use: "uninstall", Short: "Remove awake's user LaunchAgent", RunE: func(cmd *cobra.Command, args []string) error {
		path := awake.LaunchAgentPath(a.home)
		uid := strconv.Itoa(os.Getuid())
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			fmt.Println("awake: LaunchAgent is not installed")
			return nil
		}
		_, _ = a.runner.Output(cmd.Context(), "launchctl", "bootout", "gui/"+uid, path)
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Println("awake: LaunchAgent removed")
		return nil
	}}
}
func doctorCmd(a *app) *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Diagnose macOS readiness", RunE: func(c *cobra.Command, args []string) error {
		fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		if runtime.GOOS != "darwin" {
			fmt.Println("macOS: unsupported")
		} else {
			fmt.Println("macOS: ok")
		}
		for _, name := range []string{"caffeinate", "pmset"} {
			_, err := a.runner.Output(c.Context(), "which", name)
			fmt.Printf("%s: %s\n", name, check(err == nil))
		}
		s, err := a.snapshot(c.Context())
		fmt.Printf("Power source: %s\nSleep assertion: %s\nTailscale: %s\nSSH :22: %s\nState directory: %s\nPID: %s\nLaunchAgent: %s\n", s.PowerSource, check(s.SleepAssertion), s.Tailscale, s.SSH, check(a.store.Ensure() == nil), pidOrNone(s.CaffeinatePID), check(fileExists(awake.LaunchAgentPath(a.home))))
		return err
	}}
}
func versionCmd() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Show build version", Run: func(*cobra.Command, []string) {
		fmt.Printf("awake %s (commit %s, built %s)\n", version, commit, buildTime)
	}}
}
func statusText(s awake.Snapshot) string {
	state := "INACTIVE"
	if s.Awake {
		state = "ACTIVE"
	}
	if s.ExternalCaffeinate && !s.Awake {
		state = "EXTERNAL CAFFEINATE"
	}
	return fmt.Sprintf("AWAKE %s\n\nSleep prevention: %s\nCaffeinate PID: %s\nPower source: %s\nCPU: %.1f%%\nRAM: %.1f / %.1f GB (%.0f%%)\nSwap: %.1f GB\nLoad: %.2f %.2f %.2f\nTailscale: %s\nSSH :22: %s\nTemperature: unavailable\nThermal state: %s\nUptime: %s\n", state, boolString(s.SleepAssertion, "active", "inactive"), pidOrNone(s.CaffeinatePID), s.PowerSource, s.CPUPercent, float64(s.MemoryUsedBytes)/(1<<30), float64(s.MemoryTotalBytes)/(1<<30), s.MemoryPercent, float64(s.SwapUsedBytes)/(1<<30), s.LoadAverage[0], s.LoadAverage[1], s.LoadAverage[2], s.Tailscale, s.SSH, s.ThermalState, formatUptime(s.UptimeSeconds))
}
func boolString(v bool, a, b string) string {
	if v {
		return a
	}
	return b
}
func check(v bool) string {
	if v {
		return "ok"
	}
	return "missing/error"
}
func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }
func pidOrNone(p int) string {
	if p == 0 {
		return "none"
	}
	return strconv.Itoa(p)
}
func formatUptime(s uint64) string { return fmt.Sprintf("%02dh %02dm", s/3600, (s%3600)/60) }
