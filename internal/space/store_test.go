package space

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: t.TempDir(), Account: "account-a", Origin: "https://example.test"}
}

func TestTaskScopeSurvivesDefaultSwitchAndRejectsConflicts(t *testing.T) {
	s := testStore(t)
	a := Selection{TeamID: "12", Name: "A"}
	b := Selection{TeamID: "34", Name: "B"}
	ref := References{ThreadID: "thread", RunID: "run", AssetIDs: []string{"asset"}}
	if err := s.Remember(ref, a); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDefault(b); err != nil {
		t.Fatal(err)
	}
	got, err := (&Session{Store: s}).Resolve(ref)
	if err != nil || got.TeamID != "12" {
		t.Fatalf("old task = %+v, %v", got, err)
	}
	got, err = (&Session{Store: s}).Resolve(References{})
	if err != nil || got.TeamID != "34" {
		t.Fatalf("new task = %+v, %v", got, err)
	}
	if _, err := (&Session{Store: s, Explicit: &b.TeamID}).Resolve(ref); err == nil {
		t.Fatal("explicit conflicting team accepted")
	}
	if _, err := (&Session{Store: s}).Resolve(References{AssetIDs: []string{"asset"}}); err == nil {
		t.Fatal("cross-team asset accepted")
	}
	zero := "0"
	got, err = (&Session{Store: s, Explicit: &zero}).Resolve(References{})
	if err != nil || got.TeamID != "0" {
		t.Fatalf("personal override = %+v, %v", got, err)
	}
	current, _ := s.Current()
	if current.TeamID != "34" {
		t.Fatal("one-shot override changed saved default")
	}
	if err := os.Remove(s.file("thread", "thread")); err != nil {
		t.Fatal(err)
	}
	got, err = (&Session{Store: s}).Resolve(ref)
	if err != nil || got.TeamID != "12" {
		t.Fatalf("partial task record = %+v, %v", got, err)
	}
}

func TestCorruptRecordRequiresExplicitRecovery(t *testing.T) {
	s := testStore(t)
	ref := References{ThreadID: "thread", RunID: "run"}
	a := Selection{TeamID: "12", Name: "A"}
	if err := s.Remember(ref, a); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.file("run", "run"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Session{Store: s}).Resolve(ref); err == nil {
		t.Fatal("corrupt record treated as missing")
	}
	other := "34"
	if _, err := (&Session{Store: s, Explicit: &other}).Resolve(ref); err == nil {
		t.Fatal("corrupt run overrode valid thread")
	}
	if _, err := (&Session{Store: s, Explicit: &a.TeamID}).Resolve(ref); err != nil {
		t.Fatal(err)
	}
	if err := s.Remember(ref, a); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Session{Store: s}).Resolve(ref); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.file("default", ""), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Session{Store: s}).Resolve(References{}); err == nil {
		t.Fatal("corrupt default fell back to personal")
	}
	zero := "0"
	if _, err := (&Session{Store: s, Explicit: &zero}).Resolve(References{}); err != nil {
		t.Fatal(err)
	}
}

func TestSpaceIdentityAndInt64Boundary(t *testing.T) {
	for _, raw := range []string{"", "-1", "1.5", " 12", "+12", "9223372036854775808"} {
		if _, err := ParseTeamID(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	s := testStore(t)
	for raw, want := range map[string]string{"0": "0", "000": "0", "00012": "12", "9223372036854775807": "9223372036854775807"} {
		if got, err := ParseTeamID(raw); err != nil || got != want {
			t.Errorf("%q = %q, %v", raw, got, err)
		}
		if err := s.SaveDefault(Selection{TeamID: raw}); err != nil {
			t.Fatalf("save %q: %v", raw, err)
		}
		if got, err := s.Current(); err != nil || got.TeamID != want {
			t.Errorf("read saved %q = %+v, %v; want team_id %q", raw, got, err, want)
		}
	}
	a, err := NewStore("https://EXAMPLE.test:443/path", "account-a")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewStore("https://example.test", "account-a")
	c, _ := NewStore("https://example.test", "account-b")
	d, _ := NewStore("https://other.test", "account-a")
	if a.Dir != b.Dir || a.Dir == c.Dir || a.Dir == d.Dir {
		t.Fatal("account/origin isolation failed")
	}
}

func TestFixedSessionRequiresExplicitRecordRecovery(t *testing.T) {
	for _, teamID := range []string{"0", "12"} {
		for _, bindingOnly := range []bool{false, true} {
			s := testStore(t)
			selected := Selection{TeamID: teamID}
			if err := s.SaveDefault(selected); err != nil {
				t.Fatal(err)
			}
			session := &Session{Store: s}
			if _, err := session.Resolve(References{}); err != nil {
				t.Fatal(err)
			}
			if bindingOnly {
				binding, err := DecodeBinding(session.Token())
				if err != nil {
					t.Fatal(err)
				}
				session = &Session{Store: s, Expected: binding}
			}
			if err := s.SaveDefault(Selection{TeamID: "34"}); err != nil {
				t.Fatal(err)
			}
			ref := References{ThreadID: "thread", RunID: "run"}
			if err := s.Remember(ref, selected); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.file("run", "run"), []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := session.Resolve(ref); err == nil {
				t.Fatalf("team %s, binding=%v: fixed space bypassed corrupt record", teamID, bindingOnly)
			}
			session.Explicit = &teamID
			got, err := session.Resolve(ref)
			if err != nil || got.TeamID != teamID {
				t.Fatalf("explicit recovery = %+v, %v", got, err)
			}
			if err := s.Remember(References{ThreadID: "other"}, Selection{TeamID: "34"}); err != nil {
				t.Fatal(err)
			}
			if _, err := session.Resolve(References{ThreadID: "other"}); err == nil {
				t.Fatal("fixed space accepted a conflicting task")
			}
		}
	}
}

func TestSessionWithoutStoreKeepsExplicitSpace(t *testing.T) {
	for _, teamID := range []string{"0", "12", "34"} {
		explicit := teamID
		session := &Session{Explicit: &explicit}
		selected, err := session.Resolve(References{})
		if err != nil || selected.TeamID != teamID {
			t.Fatalf("space = %+v, %v", selected, err)
		}
		explicit = "56"
		selected, err = session.Resolve(References{ThreadID: "thread", RunID: "run"})
		if err != nil || selected.TeamID != teamID {
			t.Fatalf("command changed its frozen space: %+v, %v", selected, err)
		}
		session.Remember(References{ThreadID: "thread", RunID: "run"})
		if session.Token() != "" {
			t.Fatal("unknown account must not produce an identity binding")
		}
	}
	session := &Session{Expected: &Binding{Selection: Personal(), Account: "account", Origin: "https://example.test"}}
	if _, err := session.Resolve(References{}); err == nil {
		t.Fatal("identity binding accepted without an identifiable account")
	}
}

func TestCommandSessionFreezesScopeAndChecksIdentity(t *testing.T) {
	s := testStore(t)
	if err := s.SaveDefault(Selection{TeamID: "12", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	account := s.Account
	var warning bytes.Buffer
	session := &Session{Store: s, Stderr: &warning, Identity: func(context.Context) (string, error) { return account, nil }}
	ctx := context.Background()
	if _, err := session.Resolve(References{}); err != nil {
		t.Fatal(err)
	}
	token := session.Token()
	if err := s.SaveDefault(Selection{TeamID: "34", Name: "B"}); err != nil {
		t.Fatal(err)
	}
	got, err := session.Resolve(References{})
	if err != nil || got.TeamID != "12" {
		t.Fatalf("session changed = %+v, %v", got, err)
	}
	binding, err := DecodeBinding(token)
	if err != nil {
		t.Fatal(err)
	}
	next := &Session{Store: s, Expected: binding}
	got, err = next.Resolve(References{})
	if err != nil || got.TeamID != "12" || next.Token() != token {
		t.Fatalf("cross-process binding = %+v, %v", got, err)
	}
	account = "account-b"
	if err := session.CheckIdentity(ctx); err == nil {
		t.Fatal("account switch accepted")
	}
	next.Store = &Store{Dir: s.Dir, Account: account, Origin: s.Origin}
	if _, err := next.Resolve(References{}); err == nil {
		t.Fatal("binding allowed different account")
	}
	// A successful request stays successful even if the record directory fails.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Dir = blocked
	session.Remember(References{ThreadID: "created-thread", RunID: "created-run"})
	if !strings.Contains(warning.String(), "created-run") || !strings.Contains(warning.String(), "不要重复提交") {
		t.Fatal(warning.String())
	}
}
