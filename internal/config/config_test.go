package config

import (
	"os"
	"strings"
	"testing"
)

func setFargateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BACKFLOW_MODE", "fargate")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("BACKFLOW_DATABASE_URL", "postgres://localhost/test")
	t.Setenv("BACKFLOW_ECS_CLUSTER", "test-cluster")
	t.Setenv("BACKFLOW_ECS_TASK_DEFINITION", "test-task")
	t.Setenv("BACKFLOW_ECS_SUBNETS", "subnet-abc")
	t.Setenv("BACKFLOW_CLOUDWATCH_LOG_GROUP", "/ecs/test")
}

func TestLoad_FargateMaxSubscriptionWithoutARN(t *testing.T) {
	setFargateEnv(t)
	t.Setenv("BACKFLOW_AUTH_MODE", "max_subscription")
	t.Setenv("ANTHROPIC_API_KEY", "") // not needed for max_subscription

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when max_subscription in fargate mode without secret ARN")
	}
	if !strings.Contains(err.Error(), "BACKFLOW_CLAUDE_CREDENTIALS_SECRET_ARN") {
		t.Errorf("error should mention BACKFLOW_CLAUDE_CREDENTIALS_SECRET_ARN, got: %s", err.Error())
	}
}

func TestLoad_FargateMaxSubscriptionWithARN(t *testing.T) {
	setFargateEnv(t)
	t.Setenv("BACKFLOW_AUTH_MODE", "max_subscription")
	t.Setenv("ANTHROPIC_API_KEY", "") // not needed for max_subscription
	t.Setenv("BACKFLOW_CLAUDE_CREDENTIALS_SECRET_ARN", "arn:aws:secretsmanager:us-east-1:123456789012:secret:test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClaudeCredentialsSecretARN != "arn:aws:secretsmanager:us-east-1:123456789012:secret:test" {
		t.Errorf("ClaudeCredentialsSecretARN = %q, want ARN", cfg.ClaudeCredentialsSecretARN)
	}
}

func TestMaxConcurrent_FargateMaxSubscription(t *testing.T) {
	cfg := &Config{
		Mode:               ModeFargate,
		AuthMode:           AuthModeMaxSubscription,
		MaxConcurrentTasks: 5,
	}
	if got := cfg.MaxConcurrent(); got != 1 {
		t.Errorf("MaxConcurrent() = %d, want 1 for fargate+max_subscription", got)
	}
}

func TestMaxConcurrent_FargateAPIKey(t *testing.T) {
	cfg := &Config{
		Mode:               ModeFargate,
		AuthMode:           AuthModeAPIKey,
		MaxConcurrentTasks: 5,
	}
	if got := cfg.MaxConcurrent(); got != 5 {
		t.Errorf("MaxConcurrent() = %d, want 5 for fargate+api_key", got)
	}
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	// Set minimum env vars to pass earlier validations
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	os.Setenv("BACKFLOW_DATABASE_URL", "")
	defer os.Unsetenv("ANTHROPIC_API_KEY")
	defer os.Unsetenv("BACKFLOW_DATABASE_URL")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when BACKFLOW_DATABASE_URL is empty, got nil")
	}

	want := "BACKFLOW_DATABASE_URL"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error should mention %q, got: %s", want, err.Error())
	}
}
