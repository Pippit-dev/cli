package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/Pippit-dev/pippit-cli/internal/commandnames"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/space"
	"github.com/spf13/cobra"
)

func prepareCommandSpace(cmd *cobra.Command, runner *common.Runner, stderr io.Writer) error {
	if runner == nil {
		return nil
	}
	teamFlag := cmd.Flags().Lookup("team-id")
	var explicit *string
	if teamFlag != nil && teamFlag.Changed {
		id, err := space.ParseTeamID(teamFlag.Value.String())
		if err != nil {
			return err
		}
		explicit = &id
	}
	top := cmd
	for top.Parent() != nil && top.Parent().Parent() != nil {
		top = top.Parent()
	}
	switch top.Name() {
	case commandnames.Space, commandnames.Login, commandnames.Logout, commandnames.Status, commandnames.Update, commandnames.Completion, commandnames.Help, commandnames.DownloadResult:
		return nil
	}
	raw, _ := cmd.Flags().GetString("space-context")
	store, err := common.SpaceStoreForRunner(cmd.Context(), runner)
	if err != nil {
		if explicit == nil || raw != "" {
			return err
		}
		if stderr != nil {
			fmt.Fprintln(stderr, "本地空间记录不可用，将使用显式 --team-id；本次不保存空间记录。")
		}
	}
	if store == nil && explicit == nil && raw == "" {
		return nil
	}
	session := &space.Session{Store: store, Explicit: explicit, Stderr: stderr, Identity: func(ctx context.Context) (string, error) { return common.SpaceAccount(ctx, runner) }}
	if raw != "" {
		session.Expected, err = space.DecodeBinding(raw)
		if err != nil {
			return err
		}
	}
	ctx := common.WithSpace(cmd.Context(), session)
	cmd.SetContext(ctx)
	// Business requests resolve their space from the final request body.
	// Canvas has no team scope, so check its personal-only boundary here.
	if top.Name() != "canvas" {
		return nil
	}
	selected, err := session.Resolve(space.References{})
	if err != nil {
		return err
	}
	if selected.TeamID != "0" {
		return fmt.Errorf("canvas 暂仅支持个人空间；请显式使用 --team-id 0")
	}
	return nil
}

type listedSpace struct {
	space.Selection
	CurrentRole *int32 `json:"current_role,omitempty"`
	Current     bool   `json:"current"`
}

func loadSpaces(ctx context.Context, runner *common.Runner, store *space.Store) ([]listedSpace, error) {
	if store != nil {
		ctx = common.WithSpace(ctx, &space.Session{Store: store, Identity: func(ctx context.Context) (string, error) { return common.SpaceAccount(ctx, runner) }})
	}
	result := []listedSpace{{Selection: space.Personal()}}
	seenTeams := map[string]bool{"0": true}
	seenCursors := map[string]bool{}
	cursor := "0"
	for {
		if seenCursors[cursor] {
			return nil, fmt.Errorf("团队列表分页游标重复，未保存空间选择")
		}
		seenCursors[cursor] = true
		var response struct {
			Ret    string `json:"ret"`
			Errmsg string `json:"errmsg"`
			LogID  string `json:"log_id"`
			Data   *struct {
				Teams []struct {
					TeamID string `json:"team_id"`
					Name   string `json:"team_name"`
					Role   *int32 `json:"current_role"`
				} `json:"team_info_list"`
				NextCursor string `json:"next_cursor"`
				HasMore    bool   `json:"has_more"`
			} `json:"data"`
		}
		if err := runner.Client.SendRequest(ctx, config.GetLiteTeamListPath, map[string]any{"cursor": cursor, "count": 100}, &response); err != nil {
			return nil, fmt.Errorf("获取团队列表失败，默认空间未改变: %w", err)
		}
		if response.Ret != "0" {
			return nil, common.NewLogIDError("获取团队列表失败: "+response.Errmsg, response.LogID)
		}
		if response.Data == nil {
			return nil, fmt.Errorf("团队列表响应缺少 data")
		}
		for _, team := range response.Data.Teams {
			id, err := space.ParseTeamID(team.TeamID)
			if err != nil || id == "0" {
				return nil, fmt.Errorf("团队列表返回无效 team_id")
			}
			if !seenTeams[id] {
				result = append(result, listedSpace{Selection: space.Selection{TeamID: id, Name: team.Name}, CurrentRole: team.Role})
				seenTeams[id] = true
			}
		}
		if !response.Data.HasMore {
			return result, nil
		}
		if response.Data.NextCursor == "" {
			return nil, fmt.Errorf("团队列表未返回下一页游标，未保存空间选择")
		}
		cursor = response.Data.NextCursor
	}
}

func newSpaceCommand(stdout io.Writer, runner *common.Runner) *cobra.Command {
	root := &cobra.Command{Use: commandnames.Space, Short: "查看和切换个人或团队默认空间"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "列出个人和团队空间，标记当前选择", RunE: func(cmd *cobra.Command, _ []string) error {
		store, err := common.SpaceStoreForRunner(cmd.Context(), runner)
		if err != nil {
			return err
		}
		var selected *space.Selection
		if store != nil {
			current, err := store.Current()
			if err != nil {
				return err
			}
			selected = &current
		}
		spaces, err := loadSpaces(cmd.Context(), runner, store)
		if err != nil {
			return err
		}
		for i := range spaces {
			spaces[i].Current = selected != nil && spaces[i].TeamID == selected.TeamID
		}
		return common.WriteJSON(stdout, map[string]any{"spaces": spaces, "current": selected})
	}}
	use := &cobra.Command{Use: "use <TEAM_ID>", Args: cobra.ExactArgs(1), Short: "保存默认空间；0 为个人，正整数为团队", RunE: func(cmd *cobra.Command, args []string) error {
		id, err := space.ParseTeamID(args[0])
		if err != nil {
			return err
		}
		store, err := common.SpaceStoreForRunner(cmd.Context(), runner)
		if err != nil {
			return err
		}
		if store == nil {
			return fmt.Errorf("无法识别当前账号，不能保存默认空间")
		}
		selected := space.Personal()
		if id != "0" {
			spaces, err := loadSpaces(cmd.Context(), runner, store)
			if err != nil {
				return err
			}
			found := false
			for _, item := range spaces {
				if item.TeamID == id {
					selected = item.Selection
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("当前账号的团队列表中没有 %s，默认空间未改变", id)
			}
		}
		account, err := common.SpaceAccount(cmd.Context(), runner)
		if err != nil {
			return err
		}
		if account != store.Account {
			return fmt.Errorf("账号已改变，默认空间未保存；请重新选择")
		}
		if err := store.SaveDefault(selected); err != nil {
			return err
		}
		return common.WriteJSON(stdout, selected)
	}}
	root.AddCommand(list, use)
	return root
}
