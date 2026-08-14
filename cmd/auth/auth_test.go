package authcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type fakeAuthManager struct {
	credential    *internal_auth.Credential
	status        *internal_auth.Status
	loginProgress string
	loginCalls    int
	loginOptions  []internal_auth.LoginOptions
	logoutCalls   int
}

func (manager *fakeAuthManager) ResolveAccessKey(context.Context) (string, error) {
	if manager.credential == nil {
		return "", internal_auth.ErrCredentialNotFound
	}
	return manager.credential.AccessKey, nil
}

func (manager *fakeAuthManager) Login(_ context.Context, options internal_auth.LoginOptions) (*internal_auth.Credential, error) {
	manager.loginCalls++
	manager.loginOptions = append(manager.loginOptions, options)
	if options.Progress != nil {
		message := manager.loginProgress
		if message == "" {
			message = "正在完成浏览器授权…\n"
		}
		_, _ = options.Progress.Write([]byte(message))
	}
	return manager.credential, nil
}

func TestLoginCommandCanRequestSafeCredentialRotation(t *testing.T) {
	manager := &fakeAuthManager{credential: &internal_auth.Credential{
		UID: "123", CredentialScope: "account-device-scope", ExpiredAt: time.Now().Add(time.Hour).Unix(),
	}}
	command := NewLoginCommand(io.Discard, io.Discard, &common.Runner{Config: &config.Config{}, Auth: manager})
	command.SetArgs([]string{"--force"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(manager.loginOptions) != 1 || !manager.loginOptions[0].ForceRefresh {
		t.Fatalf("login options = %#v, want forced refresh", manager.loginOptions)
	}
}

func (manager *fakeAuthManager) Status(context.Context) (*internal_auth.Status, error) {
	return manager.status, nil
}

func (manager *fakeAuthManager) Logout(context.Context, bool) error {
	manager.logoutCalls++
	return nil
}

func (manager *fakeAuthManager) CredentialScope(context.Context) (string, error) {
	return "device-scope", nil
}

func TestLoginCommandPrintsMetadataWithoutAccessKey(t *testing.T) {
	const accessKey = "must-never-be-printed"
	const loginURL = "https://xyq.jianying.com/cli/pippit-tool-login?callback=http%3A%2F%2F127.0.0.1%3A1234&random_secret_key=one-time-secret"
	expiresAt := time.Now().Add(time.Hour).Unix()
	manager := &fakeAuthManager{credential: &internal_auth.Credential{
		AccessKey: accessKey, UID: "123", CredentialScope: "device-scope", ExpiredAt: expiresAt,
	}, loginProgress: "正在完成浏览器授权…\n授权地址：\n" + loginURL + "\n"}
	var stdout, stderr bytes.Buffer
	command := NewLoginCommand(&stdout, &stderr, &common.Runner{Config: &config.Config{}, Auth: manager})
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if manager.loginCalls != 1 || !strings.Contains(stderr.String(), "浏览器授权") {
		t.Fatalf("login calls/stderr = %d/%q", manager.loginCalls, stderr.String())
	}
	if strings.Contains(stdout.String(), accessKey) || strings.Contains(stderr.String(), accessKey) {
		t.Fatalf("command output leaked Access Key: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), loginURL) || strings.Contains(stdout.String(), loginURL) {
		t.Fatalf("login URL stream contract violated: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	var result loginResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.LoggedIn || result.Source != "browser" || result.UID != "123" || result.CredentialScope != "device-scope" {
		t.Fatalf("login result = %#v", result)
	}
}

func TestStatusAndLogoutDoNotModifyExplicitEnvironmentCredential(t *testing.T) {
	manager := &fakeAuthManager{status: &internal_auth.Status{LoggedIn: true, Source: "environment"}}
	runner := &common.Runner{Config: &config.Config{AccessKey: "explicit-ci-key"}, Auth: manager}
	var statusOut, statusErr bytes.Buffer
	status := NewStatusCommand(&statusOut, &statusErr, runner)
	if err := status.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statusOut.String(), runner.Config.AccessKey) {
		t.Fatal("status output leaked explicit Access Key")
	}

	var logoutOut, logoutErr bytes.Buffer
	logout := NewLogoutCommand(&logoutOut, &logoutErr, runner)
	if err := logout.Execute(); err != nil {
		t.Fatal(err)
	}
	if manager.logoutCalls != 1 || runner.Config.AccessKey != "explicit-ci-key" {
		t.Fatalf("logout calls/key = %d/%q", manager.logoutCalls, runner.Config.AccessKey)
	}
	if !strings.Contains(logoutErr.String(), "无法替你清除") || strings.Contains(logoutErr.String(), runner.Config.AccessKey) {
		t.Fatalf("logout stderr = %q", logoutErr.String())
	}
	var result logoutResult
	if err := json.Unmarshal(logoutOut.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.LoggedOut || !result.RemoteCredentialPreserved || !strings.Contains(logoutErr.String(), "远程 Access Key 未撤销") {
		t.Fatalf("logout result/stderr = %#v/%q", result, logoutErr.String())
	}
}
