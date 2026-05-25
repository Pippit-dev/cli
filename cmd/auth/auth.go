package authcmd

//import (
//	"fmt"
//	"io"
//	"strings"
//	"time"
//
//	"github.com/Pippit-dev/pippit-cli/internal/auth"
//	"github.com/Pippit-dev/pippit-cli/internal/common"
//	"github.com/bytedance/sonic"
//	"github.com/spf13/cobra"
//)
//
//type checkResult struct {
//	Pending bool `json:"pending"`
//	State   any  `json:"state,omitempty"`
//}
//
//func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
//	cmd := &cobra.Command{
//		Use:   "auth",
//		Short: "Manage Pippit OAuth login state",
//	}
//	cmd.SetOut(stdout)
//	cmd.SetErr(stderr)
//	cmd.AddCommand(newLoginCommand(stdout, stderr, runner))
//	cmd.AddCommand(newCheckCommand(stdout, stderr, runner))
//	cmd.AddCommand(newStatusCommand(stdout, stderr, runner))
//	cmd.AddCommand(newLogoutCommand(stdout, stderr, runner))
//	return cmd
//}
//
//func newLoginCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
//	cmd := &cobra.Command{
//		Use:   "login",
//		Short: "Start an OAuth device login flow",
//		Args:  cobra.NoArgs,
//		RunE: func(cmd *cobra.Command, _ []string) error {
//			if runner == nil || runner.AuthAuthorizer == nil {
//				return fmt.Errorf("auth manager is required")
//			}
//			flow, err := runner.AuthAuthorizer.NewLoginFlow(cmd.Context())
//			if err != nil {
//				return err
//			}
//			return writeJSON(stdout, flow)
//		},
//	}
//	cmd.SetOut(stdout)
//	cmd.SetErr(stderr)
//	return cmd
//}
//
//func newCheckCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
//	var deviceCode string
//	cmd := &cobra.Command{
//		Use:   "check",
//		Short: "Check whether an OAuth device login has completed",
//		Args:  cobra.NoArgs,
//		RunE: func(cmd *cobra.Command, _ []string) error {
//			deviceCode = strings.TrimSpace(deviceCode)
//			if deviceCode == "" {
//				return fmt.Errorf("--device-code is required")
//			}
//			if runner == nil || runner.AuthAuthorizer == nil {
//				return fmt.Errorf("auth manager is required")
//			}
//			state, err := runner.AuthAuthorizer.CheckLogin(cmd.Context(), deviceCode)
//			if auth.IsLoginPending(err) {
//				return writeJSON(stdout, checkResult{Pending: true})
//			}
//			if err != nil {
//				return err
//			}
//			v := map[string]any{
//				"logged_in":  state.LoggedIn,
//				"expires_at": state.ExpiresAt.Format(time.RFC3339),
//			}
//			return writeJSON(stdout, checkResult{State: v})
//		},
//	}
//	cmd.SetOut(stdout)
//	cmd.SetErr(stderr)
//	cmd.Flags().StringVar(&deviceCode, "device-code", "", "device code returned by auth login")
//	return cmd
//}
//
//func newStatusCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
//	cmd := &cobra.Command{
//		Use:   "status",
//		Short: "Show current OAuth login state",
//		Args:  cobra.NoArgs,
//		RunE: func(cmd *cobra.Command, _ []string) error {
//			if runner == nil || runner.AuthAuthorizer == nil {
//				return fmt.Errorf("auth manager is required")
//			}
//			state, err := runner.AuthAuthorizer.State(cmd.Context())
//			if err != nil {
//				return err
//			}
//			if !state.LoggedIn {
//				return fmt.Errorf("not logged in")
//			}
//			v := map[string]any{
//				"logged_in":  state.LoggedIn,
//				"expires_at": state.ExpiresAt.Format(time.RFC3339),
//			}
//			return writeJSON(stdout, v)
//		},
//	}
//	cmd.SetOut(stdout)
//	cmd.SetErr(stderr)
//	return cmd
//}
//
//func newLogoutCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
//	cmd := &cobra.Command{
//		Use:   "logout",
//		Short: "Clear local OAuth login state",
//		Args:  cobra.NoArgs,
//		RunE: func(cmd *cobra.Command, _ []string) error {
//			if runner == nil || runner.AuthAuthorizer == nil {
//				return fmt.Errorf("auth manager is required")
//			}
//			if err := runner.AuthAuthorizer.Logout(cmd.Context()); err != nil {
//				return err
//			}
//			return writeJSON(stdout, map[string]bool{"logged_out": true})
//		},
//	}
//	cmd.SetOut(stdout)
//	cmd.SetErr(stderr)
//	return cmd
//}
//
//func writeJSON(w io.Writer, v any) error {
//	data, err := sonic.Marshal(v)
//	if err != nil {
//		return err
//	}
//	_, err = fmt.Fprintln(w, string(data))
//	return err
//}
