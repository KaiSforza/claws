package aws

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

func TestRunSSOLoginReturnsThrottlingBeforeDeviceFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetSSOLoginSeams(t)

	cachedErr := errors.New("ThrottlingException: rate exceeded")
	loadSSOConfig = func(context.Context, ...func(*config.LoadOptions) error) (awssdk.Config, error) {
		return awssdk.Config{Region: "us-east-1"}, nil
	}
	retrieveSSORoleCredentialsForLogin = func(context.Context, awssdk.Config, ProfileInfo, string) (time.Time, error) {
		return time.Time{}, cachedErr
	}
	startSSODeviceLoginForLogin = func(context.Context, awssdk.Config, ProfileInfo, string, io.Writer) error {
		t.Fatal("startSSODeviceLoginForLogin should not be called for throttling cached credentials")
		return nil
	}

	_, err := RunSSOLogin(context.Background(), completeSSOProfile(), io.Discard)
	if !errors.Is(err, cachedErr) {
		t.Fatalf("RunSSOLogin() error = %v, want cached throttling error", err)
	}
}

func TestRunSSOLoginFallsThroughToDeviceFlowForNonThrottlingCachedError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetSSOLoginSeams(t)

	deviceStarted := false
	retrieveCalls := 0
	expiresAt := time.Now().Add(time.Hour).UTC()
	loadSSOConfig = func(context.Context, ...func(*config.LoadOptions) error) (awssdk.Config, error) {
		return awssdk.Config{Region: "us-east-1"}, nil
	}
	retrieveSSORoleCredentialsForLogin = func(context.Context, awssdk.Config, ProfileInfo, string) (time.Time, error) {
		retrieveCalls++
		if retrieveCalls == 1 {
			return time.Time{}, errors.New("cached token expired")
		}
		return expiresAt, nil
	}
	startSSODeviceLoginForLogin = func(context.Context, awssdk.Config, ProfileInfo, string, io.Writer) error {
		deviceStarted = true
		return nil
	}

	result, err := RunSSOLogin(context.Background(), completeSSOProfile(), io.Discard)
	if err != nil {
		t.Fatalf("RunSSOLogin() error = %v", err)
	}
	if !deviceStarted {
		t.Fatal("RunSSOLogin() did not start device flow after non-throttling cached credential error")
	}
	if retrieveCalls != 2 {
		t.Fatalf("retrieve calls = %d, want 2", retrieveCalls)
	}
	if result.Kind != SSOLoginNew {
		t.Fatalf("result.Kind = %v, want SSOLoginNew", result.Kind)
	}
	if result.Message != "SSO login successful" {
		t.Fatalf("result.Message = %q, want SSO login successful", result.Message)
	}
	if !result.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("result.ExpiresAt = %v, want %v", result.ExpiresAt, expiresAt)
	}
}

func TestRunSSOLoginReturnsRefreshedKindForUsableCachedCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetSSOLoginSeams(t)

	expiresAt := time.Now().Add(time.Hour).UTC()
	loadSSOConfig = func(context.Context, ...func(*config.LoadOptions) error) (awssdk.Config, error) {
		return awssdk.Config{Region: "us-east-1"}, nil
	}
	retrieveSSORoleCredentialsForLogin = func(context.Context, awssdk.Config, ProfileInfo, string) (time.Time, error) {
		return expiresAt, nil
	}
	startSSODeviceLoginForLogin = func(context.Context, awssdk.Config, ProfileInfo, string, io.Writer) error {
		t.Fatal("startSSODeviceLoginForLogin should not be called for usable cached credentials")
		return nil
	}

	result, err := RunSSOLogin(context.Background(), completeSSOProfile(), io.Discard)
	if err != nil {
		t.Fatalf("RunSSOLogin() error = %v", err)
	}
	if result.Kind != SSOLoginRefreshed {
		t.Fatalf("result.Kind = %v, want SSOLoginRefreshed", result.Kind)
	}
	if result.Message != "SSO session ready" {
		t.Fatalf("result.Message = %q, want SSO session ready", result.Message)
	}
}

func TestSSOScopesDefaultsToAccountAccess(t *testing.T) {
	scopes := ssoScopes(ProfileInfo{})
	if len(scopes) != 1 || scopes[0] != ssoDefaultScope {
		t.Fatalf("ssoScopes() = %v, want [%s]", scopes, ssoDefaultScope)
	}
}

func TestSSOScopesSplitsConfiguredScopes(t *testing.T) {
	scopes := ssoScopes(ProfileInfo{SSOScopes: "sso:account:access, custom:scope other:scope"})
	want := []string{"sso:account:access", "custom:scope", "other:scope"}
	if len(scopes) != len(want) {
		t.Fatalf("ssoScopes length = %d, want %d: %v", len(scopes), len(want), scopes)
	}
	for i := range want {
		if scopes[i] != want[i] {
			t.Fatalf("ssoScopes[%d] = %q, want %q", i, scopes[i], want[i])
		}
	}
}

func TestValidateSSOProfileRequiresCompleteSettings(t *testing.T) {
	err := validateSSOProfile(ProfileInfo{Name: "dev", SSOStartURL: "https://example.awsapps.com/start"})
	if err == nil {
		t.Fatal("validateSSOProfile() error = nil, want missing settings error")
	}
}

func TestSSOTokenCachePathUsesSessionNameWhenPresent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withSession, err := ssoTokenCachePath(ProfileInfo{SSOSession: "dev-session", SSOStartURL: "https://example.awsapps.com/start"})
	if err != nil {
		t.Fatalf("ssoTokenCachePath() with session error: %v", err)
	}
	withoutSession, err := ssoTokenCachePath(ProfileInfo{SSOStartURL: "https://example.awsapps.com/start"})
	if err != nil {
		t.Fatalf("ssoTokenCachePath() without session error: %v", err)
	}
	if withSession == withoutSession {
		t.Fatalf("cache path with session should differ from legacy start URL path: %q", withSession)
	}
}

func TestWriteFileAtomicRemovesTempFileOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "token.json")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	err := writeFileAtomic(targetDir, []byte("secret-token"), 0o600)
	if err == nil {
		t.Fatal("writeFileAtomic() error = nil, want rename failure")
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("ReadDir failed: %v", readErr)
	}
	for _, entry := range entries {
		if entry.Name() != "token.json" {
			t.Fatalf("unexpected temp file left behind: %s", entry.Name())
		}
	}
}

func resetSSOLoginSeams(t *testing.T) {
	t.Helper()
	loadSSOConfig = func(ctx context.Context, optFns ...func(*config.LoadOptions) error) (awssdk.Config, error) {
		return config.LoadDefaultConfig(ctx, optFns...)
	}
	retrieveSSORoleCredentialsForLogin = retrieveSSORoleCredentials
	startSSODeviceLoginForLogin = startSSODeviceLogin
	t.Cleanup(func() {
		loadSSOConfig = func(ctx context.Context, optFns ...func(*config.LoadOptions) error) (awssdk.Config, error) {
			return config.LoadDefaultConfig(ctx, optFns...)
		}
		retrieveSSORoleCredentialsForLogin = retrieveSSORoleCredentials
		startSSODeviceLoginForLogin = startSSODeviceLogin
	})
}

func completeSSOProfile() ProfileInfo {
	return ProfileInfo{
		Name:         "dev",
		SSOSession:   "dev-session",
		SSOStartURL:  "https://example.awsapps.com/start",
		SSORegion:    "us-east-1",
		SSOAccountID: "123456789012",
		SSORoleName:  "AdministratorAccess",
	}
}
