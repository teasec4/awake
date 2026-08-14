package awake

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

type GopsutilMetrics struct{}

func (GopsutilMetrics) Snapshot(ctx context.Context) (Metrics, error) {
	c, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return Metrics{}, err
	}
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return Metrics{}, err
	}
	s, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		return Metrics{}, err
	}
	l, err := load.AvgWithContext(ctx)
	if err != nil {
		return Metrics{}, err
	}
	u, err := host.UptimeWithContext(ctx)
	if err != nil {
		return Metrics{}, err
	}
	percent := 0.0
	if len(c) > 0 {
		percent = c[0]
	}
	return Metrics{CPUPercent: percent, MemoryUsedBytes: v.Used, MemoryTotalBytes: v.Total, MemoryPercent: v.UsedPercent, SwapUsedBytes: s.Used, LoadAverage: [3]float64{l.Load1, l.Load5, l.Load15}, UptimeSeconds: u, ThermalState: "unknown"}, nil
}

type SystemNetwork struct{ Runner Runner }

func (n SystemNetwork) Tailscale(ctx context.Context) (TailscaleStatus, error) {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return TailscaleNotInstalled, nil
	}
	b, err := n.Runner.Output(ctx, "tailscale", "status", "--json")
	if err != nil {
		return TailscaleUnknown, err
	}
	return ParseTailscaleJSON(b)
}
func (n SystemNetwork) SSH(ctx context.Context) (SSHStatus, error) {
	b, err := n.Runner.Output(ctx, "lsof", "-nP", "-iTCP:22", "-sTCP:LISTEN")
	if err != nil {
		text := strings.ToLower(err.Error())
		if strings.Contains(text, "not found") {
			return SSHUnknown, nil
		}
		return SSHNotListening, nil
	}
	if strings.Contains(string(b), "LISTEN") {
		return SSHListening, nil
	}
	return SSHNotListening, nil
}
func ParseTailscaleJSON(b []byte) (TailscaleStatus, error) {
	var v struct {
		BackendState string `json:"BackendState"`
		Self         *struct {
			Online bool `json:"Online"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return TailscaleUnknown, fmt.Errorf("parse tailscale JSON: %w", err)
	}
	if v.Self != nil && v.Self.Online || strings.EqualFold(v.BackendState, "Running") {
		return TailscaleOnline, nil
	}
	return TailscaleOffline, nil
}
