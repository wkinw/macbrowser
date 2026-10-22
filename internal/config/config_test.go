//go:build windows

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"macbrowser/internal/secret"
	"macbrowser/internal/vfs"
)

func TestSMBPasswordEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	conn := vfs.Connection{Name: "nas", Host: "nas.local", User: "u", Password: "s3cret"}
	if err := cfg.UpsertConnection(conn); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("password stored in plain text: %s", raw)
	}
	if !strings.Contains(string(raw), secret.Prefix) {
		t.Fatalf("missing encrypted password: %s", raw)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Connections) != 1 || loaded.Connections[0].Password != "s3cret" {
		t.Fatalf("roundtrip connections = %+v", loaded.Connections)
	}
}

func TestLegacyPlaintextPasswordMigrated(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"connections":[{"name":"nas","host":"nas.local","port":445,"share":"","base":"","user":"u","domain":"","password":"legacy-pass"}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Connections) != 1 || cfg.Connections[0].Password != "legacy-pass" {
		t.Fatalf("loaded connections = %+v", cfg.Connections)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "legacy-pass") {
		t.Fatalf("legacy password still in plain text: %s", raw)
	}
	var disk struct {
		Connections []vfs.Connection `json:"connections"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.Connections) != 1 || !strings.HasPrefix(disk.Connections[0].Password, secret.Prefix) {
		t.Fatalf("disk password = %+v", disk.Connections)
	}
}
