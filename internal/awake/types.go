package awake

import (
	"context"
	"time"
)

type ProcessInfo struct {
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	StartedAt time.Time `json:"started_at"`
}

type SleepStatus struct {
	PID                int  `json:"caffeinate_pid"`
	AssertionActive    bool `json:"sleep_assertion"`
	ExternalCaffeinate bool `json:"external_caffeinate"`
}

type PowerManager interface {
	Start(context.Context) (ProcessInfo, error)
	Stop(context.Context) error
	Status(context.Context) (SleepStatus, error)
}

type Metrics struct {
	CPUPercent       float64    `json:"cpu_percent"`
	MemoryUsedBytes  uint64     `json:"memory_used_bytes"`
	MemoryTotalBytes uint64     `json:"memory_total_bytes"`
	MemoryPercent    float64    `json:"memory_percent"`
	SwapUsedBytes    uint64     `json:"swap_used_bytes"`
	LoadAverage      [3]float64 `json:"load_average"`
	UptimeSeconds    uint64     `json:"uptime_seconds"`
	Temperature      *float64   `json:"temperature"`
	ThermalState     string     `json:"thermal_state"`
}

type MetricsProvider interface {
	Snapshot(context.Context) (Metrics, error)
}

type TailscaleStatus string

const (
	TailscaleOnline       TailscaleStatus = "online"
	TailscaleOffline      TailscaleStatus = "offline"
	TailscaleNotInstalled TailscaleStatus = "not installed"
	TailscaleUnknown      TailscaleStatus = "unknown"
)

type SSHStatus string

const (
	SSHListening    SSHStatus = "listening"
	SSHNotListening SSHStatus = "not listening"
	SSHUnknown      SSHStatus = "unknown"
)

type NetworkStatusProvider interface {
	Tailscale(context.Context) (TailscaleStatus, error)
	SSH(context.Context) (SSHStatus, error)
}

type Snapshot struct {
	Awake              bool            `json:"awake"`
	CaffeinatePID      int             `json:"caffeinate_pid"`
	SleepAssertion     bool            `json:"sleep_assertion"`
	ExternalCaffeinate bool            `json:"external_caffeinate,omitempty"`
	PowerSource        string          `json:"power_source"`
	CPUPercent         float64         `json:"cpu_percent"`
	MemoryUsedBytes    uint64          `json:"memory_used_bytes"`
	MemoryTotalBytes   uint64          `json:"memory_total_bytes"`
	MemoryPercent      float64         `json:"memory_percent"`
	SwapUsedBytes      uint64          `json:"swap_used_bytes"`
	LoadAverage        [3]float64      `json:"load_average"`
	Tailscale          TailscaleStatus `json:"tailscale"`
	SSH                SSHStatus       `json:"ssh"`
	Temperature        *float64        `json:"temperature"`
	ThermalState       string          `json:"thermal_state"`
	UptimeSeconds      uint64          `json:"uptime_seconds"`
	UpdatedAt          time.Time       `json:"updated_at"`
}
