package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestSpaceAccountRefreshesCredentialAndSurvivesAKRotation(t *testing.T) {
	ctx := context.Background()
	cfg := config.Load()
	cfg.AccessKey = ""
	store := &memoryCredentialStore{credential: &Credential{UID: "user-a", AccessKey: "key-a", ExpiredAt: time.Now().Add(time.Hour).Unix()}}
	manager := NewManager(cfg, WithCredentialStore(store))
	a, err := manager.SpaceAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a, "user-a") || strings.Contains(a, "key-a") {
		t.Fatal("raw identity leaked")
	}
	if err := store.Save(ctx, &Credential{UID: "user-a", AccessKey: "key-rotated", ExpiredAt: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	after, err := manager.SpaceAccount(ctx)
	if err != nil || after != a {
		t.Fatalf("rotation changed identity: %s, %v", after, err)
	}
	if key, err := manager.ResolveAccessKey(ctx); err != nil || key != "key-rotated" {
		t.Fatalf("request used stale credential: %v", err)
	}
	if err := store.Save(ctx, &Credential{UID: "user-b", AccessKey: "key-b", ExpiredAt: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	b, err := manager.SpaceAccount(ctx)
	if err != nil || b == a {
		t.Fatal("new account inherited old identity", err)
	}
}
