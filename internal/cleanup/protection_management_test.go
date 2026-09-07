package cleanup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProtectionManagementPreservesPlanBoundary(t *testing.T) {
	home, data := cacheFixture(t)
	e := &Engine{Config: Config{Home: home, DataDir: data, HostID: "fixture-host", Now: func() time.Time { return fixedNow }, GoCleaner: verifiedFakeGoCleaner(home)}}
	items, err := e.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, c := range items {
		if c.Family == "go-build-cache" {
			id = c.ID
		}
	}
	if err := e.Protect("", id); err != nil {
		t.Fatal(err)
	}
	rows, err := e.Protections()
	if err != nil || len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("protections = %#v, %v", rows, err)
	}
	items, err = e.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Plan(items, []string{id}); err == nil {
		t.Fatal("protected candidate planned")
	}
	if err := e.Unprotect(id); err != nil {
		t.Fatal(err)
	}
	items, err = e.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Plan(items, []string{id}); err != nil {
		t.Fatalf("fresh plan after unprotect: %v", err)
	}
	if err := e.Unprotect(id); err == nil {
		t.Fatal("missing protection silently accepted")
	}
}

func TestProtectionManagementRejectsTampering(t *testing.T) {
	home, data := cacheFixture(t)
	e := &Engine{Config: Config{Home: home, DataDir: data}}
	if err := e.Unprotect("../outside"); err == nil {
		t.Fatal("traversal accepted")
	}
	rows, err := e.Protections()
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty = %#v, %v", rows, err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("listing created state")
	}
	if err := e.ensureState(); err != nil {
		t.Fatal(err)
	}
	id := stableID("go-build-cache", "Library/Caches/go-build")
	outside := filepath.Join(t.TempDir(), "fixture-private")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.cleanupDir(), "protected", id+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Protections(); err == nil {
		t.Fatal("symlink protection accepted")
	}
	if err := e.Unprotect(id); err == nil {
		t.Fatal("symlink protection removed")
	}
	if b, err := os.ReadFile(outside); err != nil || string(b) != "keep" {
		t.Fatal("outside changed")
	}
}
