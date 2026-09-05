package awake

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	outputs map[string][]byte
	errs    map[string]error
}

func (f fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	k := name + " " + strings.Join(args, " ")
	return f.outputs[k], f.errs[k]
}

func testStore(t *testing.T) *StateStore { t.Helper(); return NewStateStore(t.TempDir()) }
func TestProcessStateRoundTrip(t *testing.T) {
	s := testStore(t)
	want := ProcessInfo{PID: 42, Command: "caffeinate", StartedAt: time.Now().UTC().Round(0)}
	if err := s.SaveProcess(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadProcess()
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != want.PID || got.Command != want.Command {
		t.Fatalf("got %#v", got)
	}
	info, err := os.Stat(s.PIDPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
}
func TestAcquirePreventsDuplicateRun(t *testing.T) {
	s := testStore(t)
	release, err := s.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := s.Acquire(); err == nil {
		t.Fatal("second acquire succeeded")
	}
}
func TestAcquireRemovesStaleLock(t *testing.T) {
	s := testStore(t)
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.LockPath(), []byte("999999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	release, err := s.Acquire()
	if err != nil {
		t.Fatalf("acquire after stale lock: %v", err)
	}
	release()
}
func TestLoadMissingPID(t *testing.T) {
	_, err := testStore(t).LoadProcess()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}
func TestSupervisorStateRoundTrip(t *testing.T) {
	s := testStore(t)
	if pid, err := s.LoadSupervisor(); err != nil || pid != 0 {
		t.Fatalf("missing supervisor: %d %v", pid, err)
	}
	if err := s.SaveSupervisor(42); err != nil {
		t.Fatal(err)
	}
	if pid, err := s.LoadSupervisor(); err != nil || pid != 42 {
		t.Fatalf("got %d %v", pid, err)
	}
	if err := s.ClearSupervisor(); err != nil {
		t.Fatal(err)
	}
	if pid, err := s.LoadSupervisor(); err != nil || pid != 0 {
		t.Fatalf("after clear: %d %v", pid, err)
	}
}
func TestRunningProbe(t *testing.T) {
	s := testStore(t)
	ok, err := s.Running()
	if err != nil || ok {
		t.Fatalf("fresh store reports running: %v %v", ok, err)
	}
	release, err := s.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	ok, err = s.Running()
	if err != nil || !ok {
		t.Fatalf("held lock not detected: %v %v", ok, err)
	}
	release()
	ok, err = s.Running()
	if err != nil || ok {
		t.Fatalf("released lock still running: %v %v", ok, err)
	}
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.LockPath(), []byte("999999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ok, err = s.Running()
	if err != nil || ok {
		t.Fatalf("stale lock reports running: %v %v", ok, err)
	}
}
func TestHasAssertionForPID(t *testing.T) {
	text := `pid 123(caffeinate): [0x000001] 00:02:00 PreventUserIdleSystemSleep named: "caffeinate"`
	if !HasAssertionForPID(text, 123) {
		t.Fatal("expected assertion")
	}
	if HasAssertionForPID(text, 124) {
		t.Fatal("wrong pid accepted")
	}
}
func TestParsePowerSource(t *testing.T) {
	cases := map[string]string{"Now drawing from 'AC Power'\n": "AC", "Now drawing from 'Battery Power'\n": "Battery", "unrecognized": "Unknown"}
	for in, want := range cases {
		if got := ParsePowerSource(in); got != want {
			t.Errorf("%q: %s", in, got)
		}
	}
}
func TestOwnerVerification(t *testing.T) {
	s := testStore(t)
	p := SystemPower{Store: s, Runner: fakeRunner{outputs: map[string][]byte{"ps -p 12 -o comm=": []byte("/usr/bin/caffeinate\n")}}}
	ok, err := p.isCaffeinate(context.Background(), 12)
	if err != nil || !ok {
		t.Fatalf("got %v,%v", ok, err)
	}
	p.Runner = fakeRunner{outputs: map[string][]byte{"ps -p 12 -o comm=": []byte("/bin/sh\n")}}
	ok, err = p.isCaffeinate(context.Background(), 12)
	if err != nil || ok {
		t.Fatalf("got %v,%v", ok, err)
	}
}
func TestProcessStartMatches(t *testing.T) {
	saved := time.Date(2026, time.August, 14, 12, 8, 1, 500000000, time.Local)
	if !ProcessStartMatches(saved, "Fri Aug 14 12:08:01 2026") {
		t.Fatal("same process start was rejected")
	}
	if ProcessStartMatches(saved, "Fri Aug 14 12:08:10 2026") {
		t.Fatal("reused PID start time was accepted")
	}
}
func TestParseTailscaleJSON(t *testing.T) {
	got, err := ParseTailscaleJSON([]byte(`{"BackendState":"Running","Self":{"Online":true}}`))
	if err != nil || got != TailscaleOnline {
		t.Fatalf("%s %v", got, err)
	}
	got, err = ParseTailscaleJSON([]byte(`{"BackendState":"Stopped"}`))
	if err != nil || got != TailscaleOffline {
		t.Fatalf("%s %v", got, err)
	}
	if _, err = ParseTailscaleJSON([]byte("bad")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
func TestSSHListener(t *testing.T) {
	n := SystemNetwork{Runner: fakeRunner{outputs: map[string][]byte{"lsof -nP -iTCP:22 -sTCP:LISTEN": []byte("sshd 1 root 3u IPv4 TCP *:22 (LISTEN)\n")}}}
	got, err := n.SSH(context.Background())
	if err != nil || got != SSHListening {
		t.Fatalf("%s %v", got, err)
	}
	n.Runner = fakeRunner{errs: map[string]error{"lsof -nP -iTCP:22 -sTCP:LISTEN": errors.New("exit status 1")}}
	got, err = n.SSH(context.Background())
	if err != nil || got != SSHNotListening {
		t.Fatalf("%s %v", got, err)
	}
}
func TestSnapshotJSON(t *testing.T) {
	b, err := json.Marshal(Snapshot{Awake: true, CaffeinatePID: 42, Temperature: nil, Tailscale: TailscaleOnline, SSH: SSHListening})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["awake"] != true || got["caffeinate_pid"].(float64) != 42 || got["temperature"] != nil {
		t.Fatalf("unexpected JSON %s", b)
	}
}
func TestLaunchAgentPlist(t *testing.T) {
	s := testStore(t)
	p := LaunchAgentPlist("/Applications/Awake & Co/awake", s)
	for _, want := range []string{"<string>dev.awake</string>", "<string>run</string>", "<string>--no-tui</string>", "<key>KeepAlive</key><true/>", "&amp;"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q", want)
		}
	}
	path, err := WriteLaunchAgent(filepath.Dir(filepath.Dir(filepath.Dir(s.Dir))), "/tmp/awake", s)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if !strings.HasSuffix(path, "dev.awake.plist") {
		t.Fatal(path)
	}
}
