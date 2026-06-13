package view

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	awsui "github.com/clawscli/claws/internal/aws"
	"github.com/clawscli/claws/internal/config"
	navmsg "github.com/clawscli/claws/internal/msg"
)

func testProfiles() []profileItem {
	return []profileItem{
		{id: "default", display: "default", isSSO: false},
		{id: "dev", display: "dev", isSSO: false},
		{id: "prod-sso", display: "prod-sso", isSSO: true},
	}
}

func TestProfileSelectorMouseHover(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)

	selector.Update(profilesLoadedMsg{profiles: testProfiles()})

	initialCursor := selector.selector.Cursor()

	motionMsg := tea.MouseMotionMsg{X: 10, Y: 3}
	selector.Update(motionMsg)

	t.Logf("Cursor after hover: %d (was %d)", selector.selector.Cursor(), initialCursor)
}

func TestProfileSelectorMouseClick(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)

	selector.Update(profilesLoadedMsg{profiles: testProfiles()})

	clickMsg := tea.MouseClickMsg{X: 10, Y: 3, Button: tea.MouseLeft}
	_, cmd := selector.Update(clickMsg)

	t.Logf("Command after click: %v", cmd)
}

func TestProfileSelectorEmptyFilter(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)

	selector.Update(profilesLoadedMsg{profiles: testProfiles()})

	selector.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "zzz-nonexistent" {
		selector.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	selector.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if selector.selector.FilteredLen() != 0 {
		t.Errorf("Expected 0 filtered profiles, got %d", selector.selector.FilteredLen())
	}
	if selector.selector.Cursor() != -1 {
		t.Errorf("Expected cursor -1 for empty filter, got %d", selector.selector.Cursor())
	}

	selector.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})

	if selector.selector.FilteredLen() != 3 {
		t.Errorf("Expected 3 filtered profiles after clear, got %d", selector.selector.FilteredLen())
	}
	if selector.selector.Cursor() < 0 {
		t.Errorf("Expected cursor >= 0 after clear, got %d", selector.selector.Cursor())
	}
}

func TestProfileSelectorFilterMatching(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)

	profiles := []profileItem{
		{id: "default", display: "default", isSSO: false},
		{id: "dev", display: "dev", isSSO: false},
		{id: "dev-staging", display: "dev-staging", isSSO: false},
		{id: "prod-sso", display: "prod-sso", isSSO: true},
	}
	selector.Update(profilesLoadedMsg{profiles: profiles})

	selector.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "dev" {
		selector.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	selector.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if selector.selector.FilteredLen() != 2 {
		t.Errorf("Expected 2 profiles matching 'dev', got %d", selector.selector.FilteredLen())
	}

	selector.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})

	selector.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "sso" {
		selector.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	selector.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if selector.selector.FilteredLen() != 1 {
		t.Errorf("Expected 1 profile matching 'sso', got %d", selector.selector.FilteredLen())
	}
}

func TestProfileSelectorSSODetection(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)

	profiles := []profileItem{
		{id: "default", display: "default", isSSO: false},
		{id: "prod-sso", display: "prod-sso", isSSO: true},
	}
	selector.Update(profilesLoadedMsg{profiles: profiles})

	var ssoProfile profileItem
	foundSSOProfile := false
	for i := range selector.profiles {
		if selector.profiles[i].isSSO {
			ssoProfile = selector.profiles[i]
			foundSSOProfile = true
			break
		}
	}

	if !foundSSOProfile {
		t.Fatal("Expected to find SSO profile")
	}
	if ssoProfile.id != "prod-sso" {
		t.Errorf("Expected SSO profile 'prod-sso', got %q", ssoProfile.id)
	}

	var nonSSOProfile profileItem
	foundNonSSOProfile := false
	for i := range selector.profiles {
		if !selector.profiles[i].isSSO {
			nonSSOProfile = selector.profiles[i]
			foundNonSSOProfile = true
			break
		}
	}

	if !foundNonSSOProfile {
		t.Fatal("Expected to find non-SSO profile")
	}
	if nonSSOProfile.isSSO {
		t.Error("Expected non-SSO profile to have isSSO=false")
	}
}

func TestProfileSelectorToggle(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)

	profiles := []profileItem{
		{id: "default", display: "default", isSSO: false},
		{id: "dev", display: "dev", isSSO: false},
	}
	selector.Update(profilesLoadedMsg{profiles: profiles})

	selector.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if !selector.selector.Selected()["default"] {
		t.Error("Expected 'default' to be selected after toggle")
	}

	selector.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if selector.selector.Selected()["default"] {
		t.Error("Expected 'default' to be deselected after second toggle")
	}

	selector.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	selector.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	selector.Update(tea.KeyPressMsg{Code: tea.KeySpace})

	if !selector.selector.Selected()["default"] || !selector.selector.Selected()["dev"] {
		t.Error("Expected both profiles to be selected")
	}
}

func TestProfileSelectorConsoleLoginSuccessSwitchesAndEmitsProfileChange(t *testing.T) {
	config.Global().UseEnvOnly()
	t.Cleanup(func() { config.Global().UseSDKDefault() })

	selector := NewProfileSelector()
	selector.SetSize(100, 50)
	selector.Update(profilesLoadedMsg{profiles: []profileItem{
		{id: config.ProfileIDEnvOnly, display: "Env/IMDS Only", isSSO: false},
		{id: "dev", display: "dev", isSSO: false},
	}})

	_, cmd := selector.Update(newLoginResult("dev", loginKindConsole, awsui.SSOLoginResult{}, nil))
	if cmd == nil {
		t.Fatal("Expected console login success to emit profile change command")
	}

	msg := cmd()
	profileMsg, ok := msg.(navmsg.ProfilesChangedMsg)
	if !ok {
		t.Fatalf("command message = %T, want ProfilesChangedMsg", msg)
	}
	if len(profileMsg.Selections) != 1 || profileMsg.Selections[0].ID() != "dev" {
		t.Fatalf("ProfilesChangedMsg selections = %v, want [dev]", profileMsg.Selections)
	}

	if got := config.Global().Selection().ID(); got != "dev" {
		t.Errorf("global selection = %q, want dev", got)
	}
	if selector.selector.Selected()[config.ProfileIDEnvOnly] {
		t.Error("env-only selection should be cleared after console login switches profile")
	}
	if !selector.selector.Selected()["dev"] {
		t.Error("dev profile should be selected after console login")
	}
}

func TestProfileSelectorSSOLoginUsesSDKRunner(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)
	selector.Update(profilesLoadedMsg{
		profiles: []profileItem{{id: "prod-sso", display: "prod-sso", isSSO: true}},
		infoMap: map[string]awsui.ProfileInfo{
			"prod-sso": {
				Name:         "prod-sso",
				SSOSession:   "prod-session",
				SSOStartURL:  "https://example.awsapps.com/start",
				SSORegion:    "us-east-1",
				SSOAccountID: "123456789012",
				SSORoleName:  "ReadOnly",
			},
		},
	})

	called := false
	selector.ssoLogin = func(_ context.Context, profile awsui.ProfileInfo, _ io.Writer) (awsui.SSOLoginResult, error) {
		called = true
		if profile.Name != "prod-sso" {
			t.Fatalf("profile.Name = %q, want prod-sso", profile.Name)
		}
		return awsui.SSOLoginResult{Message: "SSO session ready", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}

	_, cmd := selector.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if cmd == nil {
		t.Fatal("expected SSO login command")
	}

	execCmd := &ssoLoginExec{profile: selector.profileInfo["prod-sso"], run: selector.ssoLogin}
	if err := execCmd.Run(); err != nil {
		t.Fatalf("ssoLoginExec.Run() error = %v", err)
	}
	if !called {
		t.Fatal("expected SSO login runner to be called")
	}
	if execCmd.result.Message != "SSO session ready" {
		t.Fatalf("result.Message = %q, want SSO session ready", execCmd.result.Message)
	}
}

func TestProfileSelectorSSOLoginTimeoutCancelsRunner(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)

	cancelled := make(chan struct{})
	runner := func(ctx context.Context, _ awsui.ProfileInfo, _ io.Writer) (awsui.SSOLoginResult, error) {
		<-ctx.Done()
		close(cancelled)
		return awsui.SSOLoginResult{}, ctx.Err()
	}
	execCmd := &ssoLoginExec{
		profile: awsui.ProfileInfo{Name: "prod-sso"},
		run:     runner,
		timeout: 10 * time.Millisecond,
	}

	start := time.Now()
	err := execCmd.Run()
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ssoLoginExec.Run() took %s, expected quick timeout", elapsed)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("runner did not observe context cancellation")
	}
	if !strings.Contains(err.Error(), "SSO login timed out after 10ms") {
		t.Fatalf("timeout error = %q, want clear timeout", err.Error())
	}

	selector.Update(newLoginResult("prod-sso", loginKindSSO, awsui.SSOLoginResult{}, err))
	view := selector.ViewString()
	if !strings.Contains(view, "SSO login failed: SSO login timed out after 10ms") {
		t.Fatalf("ViewString() = %q, want timeout failure message", view)
	}
}

func TestProfileSelectorLoginResultDisplayCompatibility(t *testing.T) {
	tests := []struct {
		name   string
		msg    loginResultMsg
		want   string
		danger bool
	}{
		{
			name: "sso success uses result kind default",
			msg:  newLoginResult("prod-sso", loginKindSSO, awsui.SSOLoginResult{Kind: awsui.SSOLoginRefreshed}, nil),
			want: "SSO session ready",
		},
		{
			name: "console success default message",
			msg:  newLoginResult("dev", loginKindConsole, awsui.SSOLoginResult{}, nil),
			want: "Console login successful",
		},
		{
			name:   "sso failure",
			msg:    newLoginResult("prod-sso", loginKindSSO, awsui.SSOLoginResult{}, errors.New("boom")),
			want:   "SSO login failed: boom",
			danger: true,
		},
		{
			name:   "console failure",
			msg:    newLoginResult("dev", loginKindConsole, awsui.SSOLoginResult{}, errors.New("boom")),
			want:   "Console login failed: boom",
			danger: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector := NewProfileSelector()
			selector.SetSize(100, 50)
			selector.Update(tt.msg)

			view := selector.ViewString()
			if !strings.Contains(view, tt.want) {
				t.Fatalf("ViewString() = %q, want %q", view, tt.want)
			}
			if tt.danger && tt.msg.err == nil {
				t.Fatal("danger case must carry an error")
			}
		})
	}
}

func TestProfileSelectorDisplaysLoadWarnings(t *testing.T) {
	selector := NewProfileSelector()
	selector.SetSize(100, 50)

	selector.Update(profilesLoadedMsg{
		profiles: []profileItem{{id: config.ProfileIDSDKDefault, display: "SDK Default"}},
		warnings: []string{"Failed to parse AWS config file /tmp/config: broken section"},
	})

	view := selector.ViewString()
	if !strings.Contains(view, "Failed to parse AWS config file /tmp/config") {
		t.Fatalf("ViewString() = %q, want parse warning", view)
	}
	if selector.selector.extraHeight != 1 {
		t.Fatalf("extraHeight = %d, want 1 for warning", selector.selector.extraHeight)
	}
}
