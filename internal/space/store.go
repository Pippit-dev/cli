package space

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const Version = 1

type Selection struct {
	TeamID string `json:"team_id"`
	Name   string `json:"name"`
}

type Binding struct {
	Selection
	Account string `json:"account_scope"`
	Origin  string `json:"api_origin"`
}

type References struct {
	ThreadID string   `json:"thread_id"`
	RunID    string   `json:"run_id"`
	AssetIDs []string `json:"asset_ids"`
}

type record struct {
	Version int `json:"version"`
	Binding
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	ThreadID string `json:"thread_id,omitempty"`
}

type Store struct {
	Dir     string
	Account string
	Origin  string
}

func Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ParseTeamID(raw string) (string, error) {
	if raw == "" || strings.IndexFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return "", fmt.Errorf("team-id 必须为 0 或正整数团队 ID")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return "", fmt.Errorf("team-id 超出 int64 范围")
	}
	return strconv.FormatInt(n, 10), nil
}

func NormalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", fmt.Errorf("API origin 无效")
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	return u.Scheme + "://" + u.Host, nil
}

func NewStore(baseURL, account string) (*Store, error) {
	origin, err := NormalizeOrigin(baseURL)
	if err != nil {
		return nil, err
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{Dir: filepath.Join(dir, "pippit-cli", "spaces", Digest(origin+"\n"+account)), Account: account, Origin: origin}, nil
}

func Personal() Selection { return Selection{TeamID: "0", Name: "个人空间"} }

func (s *Store) file(kind, id string) string {
	return filepath.Join(s.Dir, kind+"-"+Digest(id)+".json")
}

func (s *Store) load(kind, id, thread string) (*record, error) {
	data, err := os.ReadFile(s.file(kind, id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取%s空间记录: %w", kind, err)
	}
	var r record
	if json.Unmarshal(data, &r) != nil || r.Version != Version || r.Account != s.Account || r.Origin != s.Origin || r.Kind != kind || r.ID != id || r.ThreadID != thread {
		return nil, fmt.Errorf("%s空间记录损坏、身份不符或版本不支持", kind)
	}
	teamID, err := ParseTeamID(r.TeamID)
	if err != nil || teamID != r.TeamID {
		return nil, fmt.Errorf("%s空间记录的 team_id 无效", kind)
	}
	return &r, nil
}

func (s *Store) Current() (Selection, error) {
	r, err := s.load("default", "", "")
	if err != nil {
		return Selection{}, fmt.Errorf("%w；可用 --team-id 指定本次空间", err)
	}
	if r == nil {
		return Personal(), nil
	}
	return r.Selection, nil
}

func (s *Store) SaveDefault(selection Selection) error {
	if s.Account == "" {
		return fmt.Errorf("请先登录，再保存默认空间")
	}
	return s.save("default", "", "", selection)
}

func (s *Store) save(kind, id, thread string, selection Selection) error {
	teamID, err := ParseTeamID(selection.TeamID)
	if err != nil {
		return err
	}
	selection.TeamID = teamID
	data, err := json.Marshal(record{Version: Version, Binding: Binding{Selection: selection, Account: s.Account, Origin: s.Origin}, Kind: kind, ID: id, ThreadID: thread})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".space-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.file(kind, id))
}

// loadReferences returns readable records together with any read error, so a
// caller recovering an unreadable record can still check the remaining records.
func (s *Store) loadReferences(ref References) ([]record, error) {
	var records []record
	var unreadable error
	for _, key := range referenceKeys(ref) {
		r, err := s.load(key.kind, key.id, key.thread)
		if err != nil {
			unreadable = err
		} else if r != nil {
			records = append(records, *r)
		}
	}
	return records, unreadable
}

type referenceKey struct{ kind, id, thread string }

func referenceKeys(ref References) []referenceKey {
	var keys []referenceKey
	if ref.ThreadID != "" {
		keys = append(keys, referenceKey{"thread", ref.ThreadID, ""})
	}
	if ref.RunID != "" {
		keys = append(keys, referenceKey{"run", ref.RunID, ref.ThreadID})
	}
	for _, id := range ref.AssetIDs {
		if id != "" {
			keys = append(keys, referenceKey{"asset", id, ""})
		}
	}
	return keys
}

func (s *Store) Remember(ref References, selected Selection) error {
	for _, key := range referenceKeys(ref) {
		old, err := s.load(key.kind, key.id, key.thread)
		if err == nil && old != nil && old.TeamID != selected.TeamID {
			return fmt.Errorf("拒绝覆盖归属不同的%s记录", key.kind)
		}
		if err := s.save(key.kind, key.id, key.thread, selected); err != nil {
			return err
		}
	}
	return nil
}
