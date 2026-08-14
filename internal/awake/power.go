package awake

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type SystemPower struct {
	Store  *StateStore
	Runner Runner
	mu     sync.Mutex
	cmd    *exec.Cmd
}

func (p *SystemPower) Start(ctx context.Context) (ProcessInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := exec.LookPath("caffeinate"); err != nil {
		return ProcessInfo{}, fmt.Errorf("caffeinate is required but was not found in PATH: %w", err)
	}
	cmd := exec.Command("caffeinate", "-is")
	if err := cmd.Start(); err != nil {
		return ProcessInfo{}, fmt.Errorf("start caffeinate -is: %w", err)
	}
	p.cmd = cmd
	info := ProcessInfo{PID: cmd.Process.Pid, Command: "caffeinate", StartedAt: time.Now().UTC()}
	if err := p.Store.SaveProcess(info); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return ProcessInfo{}, err
	}
	go func() { _ = cmd.Wait() }()
	return info, nil
}
func (p *SystemPower) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	info, err := p.Store.LoadProcess()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read awake state: %w", err)
	}
	if !processAlive(info.PID) {
		_ = os.Remove(p.Store.PIDPath())
		return nil
	}
	owned, err := p.isCaffeinate(ctx, info.PID)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("refusing to stop PID %d: it is not caffeinate", info.PID)
	}
	if same, err := p.sameProcess(ctx, info); err != nil {
		return err
	} else if !same {
		return fmt.Errorf("refusing to stop PID %d: process identity does not match saved state", info.PID)
	}
	proc, err := os.FindProcess(info.PID)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop caffeinate PID %d: %w", info.PID, err)
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(75 * time.Millisecond)
	defer tick.Stop()
	for {
		if !processAlive(info.PID) {
			_ = os.Remove(p.Store.PIDPath())
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			_ = proc.Kill()
			_ = os.Remove(p.Store.PIDPath())
			return nil
		case <-tick.C:
		}
	}
}
func (p *SystemPower) Status(ctx context.Context) (SleepStatus, error) {
	status := SleepStatus{}
	info, err := p.Store.LoadProcess()
	if err == nil {
		if !processAlive(info.PID) {
			_ = os.Remove(p.Store.PIDPath())
		} else if owned, ownErr := p.isCaffeinate(ctx, info.PID); ownErr != nil {
			return status, ownErr
		} else if owned {
			same, sameErr := p.sameProcess(ctx, info)
			if sameErr != nil {
				return status, sameErr
			}
			if !same {
				_ = os.Remove(p.Store.PIDPath())
				return status, nil
			}
			status.PID = info.PID
		}
	}
	if status.PID == 0 {
		external, extErr := p.externalCaffeinate(ctx)
		if extErr == nil {
			status.ExternalCaffeinate = external
		}
	}
	b, assertionErr := p.Runner.Output(ctx, "pmset", "-g", "assertions")
	if assertionErr != nil {
		return status, fmt.Errorf("inspect sleep assertions: %w", assertionErr)
	}
	if status.PID > 0 {
		status.AssertionActive = HasAssertionForPID(string(b), status.PID)
	}
	return status, nil
}
func (p *SystemPower) isCaffeinate(ctx context.Context, pid int) (bool, error) {
	b, err := p.Runner.Output(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	if err != nil {
		return false, err
	}
	return filepath.Base(strings.TrimSpace(string(b))) == "caffeinate", nil
}
func (p *SystemPower) sameProcess(ctx context.Context, info ProcessInfo) (bool, error) {
	b, err := p.Runner.Output(ctx, "ps", "-p", strconv.Itoa(info.PID), "-o", "lstart=")
	if err != nil {
		return false, err
	}
	return ProcessStartMatches(info.StartedAt, string(b)), nil
}
func (p *SystemPower) externalCaffeinate(ctx context.Context) (bool, error) {
	b, err := p.Runner.Output(ctx, "pgrep", "-x", "caffeinate")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(b)) != "", nil
}

var pidPattern = regexp.MustCompile(`(?i)\bpid\s+(\d+)\s*\(caffeinate\)`)

func HasAssertionForPID(output string, pid int) bool {
	for _, m := range pidPattern.FindAllStringSubmatch(output, -1) {
		n, _ := strconv.Atoi(m[1])
		if n == pid {
			return strings.Contains(output, m[0])
		}
	}
	return false
}
func ProcessStartMatches(saved time.Time, output string) bool {
	start, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.TrimSpace(output), time.Local)
	if err != nil {
		return false
	}
	delta := start.Sub(saved)
	if delta < 0 {
		delta = -delta
	}
	// ps reports whole seconds, so allow normal scheduler and clock rounding jitter only.
	return delta <= 2*time.Second
}
func ParsePowerSource(output string) string {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "ac power") || strings.Contains(lower, "charging") || strings.Contains(lower, "charged"):
		return "AC"
	case strings.Contains(lower, "battery power") || strings.Contains(lower, "discharging"):
		return "Battery"
	default:
		return "Unknown"
	}
}
