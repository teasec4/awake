package awake

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const LaunchAgentLabel = "dev.awake"

func LaunchAgentPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist")
}
func LaunchAgentPlist(binary string, store *StateStore) string {
	esc := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>run</string><string>--no-tui</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, LaunchAgentLabel, esc(binary), esc(filepath.Join(store.LogsDir(), "awake.out.log")), esc(filepath.Join(store.LogsDir(), "awake.err.log")))
}
func WriteLaunchAgent(home, binary string, store *StateStore) (string, error) {
	if err := store.Ensure(); err != nil {
		return "", err
	}
	path := LaunchAgentPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(LaunchAgentPlist(binary, store)), 0644)
}
