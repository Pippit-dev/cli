package space

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// Session is one command's immutable account/origin/team choice. It is not an
// authorization grant: every request still uses the current credential.
// Store is optional when a command explicitly selects its space.
type Session struct {
	Store    *Store
	Explicit *string
	Expected *Binding
	Identity func(context.Context) (string, error)
	Stderr   io.Writer
	selected *Selection
}

func DecodeBinding(raw string) (*Binding, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	var binding Binding
	if err != nil || json.Unmarshal(data, &binding) != nil {
		return nil, fmt.Errorf("space-context 无效")
	}
	if _, err := ParseTeamID(binding.TeamID); err != nil {
		return nil, err
	}
	return &binding, nil
}

func (s *Session) CheckIdentity(ctx context.Context) error {
	if s.Store == nil || s.Identity == nil {
		return nil
	}
	account, err := s.Identity(ctx)
	if err != nil {
		return err
	}
	if account != s.Store.Account {
		return fmt.Errorf("当前账号已改变，已停止原空间的操作；请切回原账号后查询原任务")
	}
	return nil
}

// Resolve selects and freezes the command's space, checking each request's
// records even after freezing. CheckIdentity belongs at the request boundary.
func (s *Session) Resolve(ref References) (Selection, error) {
	explicit := s.Explicit
	fixed := s.selected
	if s.Expected != nil {
		if s.Store == nil {
			return Selection{}, fmt.Errorf("无法获取当前账号的空间记录，不能校验 --space-context")
		}
		if s.Expected.Account != s.Store.Account || s.Expected.Origin != s.Store.Origin {
			return Selection{}, fmt.Errorf("账号或 API origin 与原任务不一致，已停止操作")
		}
		if explicit != nil && *explicit != s.Expected.TeamID {
			return Selection{}, fmt.Errorf("--team-id 与固定任务空间冲突")
		}
		if fixed == nil {
			fixed = &s.Expected.Selection
		}
	}
	var records []record
	var unreadable error
	if s.Store != nil {
		records, unreadable = s.Store.loadReferences(ref)
	}
	var task *Selection
	for i := range records {
		r := &records[i]
		if r.Kind == "asset" {
			continue
		}
		if task != nil && task.TeamID != r.TeamID {
			return Selection{}, fmt.Errorf("thread/run 空间记录冲突；请先核实并修复本地记录")
		}
		task = &r.Selection
	}
	// Only explicit input permits recovery; a frozen choice does not.
	if unreadable != nil && explicit == nil {
		return Selection{}, fmt.Errorf("%w；核实原空间后用 --team-id <ID> 查询恢复，不要重新提交生成", unreadable)
	}

	// Keep selection priority in one place, whether or not a Store is available.
	selected := Personal()
	if fixed != nil {
		selected = *fixed
	} else if explicit != nil {
		teamID, err := ParseTeamID(*explicit)
		if err != nil {
			return Selection{}, err
		}
		if teamID != "0" {
			selected = Selection{TeamID: teamID, Name: "团队空间"}
		}
	} else if task != nil {
		selected = *task
	} else if s.Store != nil {
		var err error
		selected, err = s.Store.Current()
		if err != nil {
			return Selection{}, err
		}
	}
	if task != nil && task.TeamID != selected.TeamID {
		return Selection{}, fmt.Errorf("任务属于空间 %s，与本次空间 %s 冲突；请使用 --team-id %s 查询", task.TeamID, selected.TeamID, task.TeamID)
	}
	for _, r := range records {
		if r.Kind == "asset" && r.TeamID != selected.TeamID {
			return Selection{}, fmt.Errorf("素材属于空间 %s，与本次空间 %s 冲突；请使用相同空间的素材", r.TeamID, selected.TeamID)
		}
	}
	if s.selected == nil {
		s.selected = &selected
		if s.Stderr != nil {
			account := "个人"
			if selected.TeamID != "0" {
				account = "团队"
			}
			fmt.Fprintf(s.Stderr, "本次空间：%s（team_id=%s）；积分账户：%s\n", selected.Name, selected.TeamID, account)
		}
	}
	return *s.selected, nil
}

func (s *Session) TeamID() string {
	if s == nil || s.selected == nil {
		return "0"
	}
	return s.selected.TeamID
}

func (s *Session) Token() string {
	if s == nil || s.selected == nil || s.Store == nil {
		return ""
	}
	data, _ := json.Marshal(Binding{Selection: Selection{TeamID: s.TeamID()}, Account: s.Store.Account, Origin: s.Store.Origin})
	return base64.RawURLEncoding.EncodeToString(data)
}

func (s *Session) Remember(ref References) {
	if s == nil || s.selected == nil || s.Store == nil || s.Store.Account == "" {
		return
	}
	if err := s.Store.Remember(ref, *s.selected); err != nil && s.Stderr != nil {
		fmt.Fprintf(s.Stderr, "请求已成功，但空间记录保存失败：%v；保留 thread_id=%s run_id=%s asset_ids=%v，后续使用 --team-id %s 查询，不要重复提交。\n", err, ref.ThreadID, ref.RunID, ref.AssetIDs, s.TeamID())
	}
}
