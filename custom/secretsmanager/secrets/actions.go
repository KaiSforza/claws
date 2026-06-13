package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	smClient "github.com/clawscli/claws/custom/secretsmanager"
	"github.com/clawscli/claws/internal/action"
	appaws "github.com/clawscli/claws/internal/aws"
	"github.com/clawscli/claws/internal/dao"
	"github.com/clawscli/claws/internal/sanitize"
)

const (
	operationViewSecretValue = "ViewSecretValue"
	operationDescribeSecret  = "DescribeSecret"
)

func init() {
	action.Global.Register("secretsmanager", "secrets", []action.Action{
		{
			Name:      "View Value",
			Shortcut:  "v",
			Type:      action.ActionTypeAPI,
			Operation: operationViewSecretValue,
			Confirm:   action.ConfirmSimple,
		},
		{
			Name:      "Describe (JSON)",
			Shortcut:  "j",
			Type:      action.ActionTypeAPI,
			Operation: operationDescribeSecret,
		},
		{
			Name:      "Delete",
			Shortcut:  "D",
			Type:      action.ActionTypeAPI,
			Operation: "DeleteSecret",
			Confirm:   action.ConfirmDangerous,
		},
	})

	// Register executor
	action.RegisterExecutor("secretsmanager", "secrets", executeSecretAction)
}

// executeSecretAction executes an action on a secret
func executeSecretAction(ctx context.Context, act action.Action, resource dao.Resource) action.ActionResult {
	switch act.Operation {
	case operationViewSecretValue:
		return executeViewSecretValue(ctx, resource)
	case operationDescribeSecret:
		return executeDescribeSecret(ctx, resource)
	case "DeleteSecret":
		return executeDeleteSecret(ctx, resource)
	default:
		return action.UnknownOperationResult(act.Operation)
	}
}

func getSecretsManagerClient(ctx context.Context) (*secretsmanager.Client, error) {
	return smClient.GetClient(ctx)
}

func executeViewSecretValue(ctx context.Context, resource dao.Resource) action.ActionResult {
	client, err := getSecretsManagerClient(ctx)
	if err != nil {
		return action.ActionResult{Success: false, Error: err}
	}

	secretID := resource.GetID()
	output, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &secretID})
	if err != nil {
		return action.ActionResult{Success: false, Error: fmt.Errorf("get secret value: %w", err)}
	}

	return action.ActionResult{
		Success: true,
		Message: formatSecretValue(secretID, output.SecretString, output.SecretBinary),
	}
}

func executeDescribeSecret(ctx context.Context, resource dao.Resource) action.ActionResult {
	client, err := getSecretsManagerClient(ctx)
	if err != nil {
		return action.ActionResult{Success: false, Error: err}
	}

	secretID := resource.GetID()
	output, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: &secretID})
	if err != nil {
		return action.ActionResult{Success: false, Error: fmt.Errorf("describe secret: %w", err)}
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return action.ActionResult{Success: false, Error: fmt.Errorf("format secret description: %w", err)}
	}

	return action.ActionResult{
		Success: true,
		Message: sanitize.MultilineTerminalText(string(data)),
	}
}

func formatSecretValue(secretID string, secretString *string, secretBinary []byte) string {
	value := ""
	if secretString != nil {
		value = *secretString
	} else if len(secretBinary) > 0 {
		value = base64.StdEncoding.EncodeToString(secretBinary)
	}
	return fmt.Sprintf("Secret: %s\n\n%s", sanitize.TerminalText(secretID), sanitize.MultilineTerminalText(value))
}

func executeDeleteSecret(ctx context.Context, resource dao.Resource) action.ActionResult {
	secret, ok := resource.(*SecretResource)
	if !ok {
		return action.InvalidResourceResult()
	}

	client, err := getSecretsManagerClient(ctx)
	if err != nil {
		return action.ActionResult{Success: false, Error: err}
	}

	secretId := secret.GetID()
	input := &secretsmanager.DeleteSecretInput{
		SecretId:                   &secretId,
		ForceDeleteWithoutRecovery: appaws.BoolPtr(false), // Safe delete with 30-day recovery window
	}

	_, err = client.DeleteSecret(ctx, input)
	if err != nil {
		return action.ActionResult{Success: false, Error: fmt.Errorf("delete secret: %w", err)}
	}

	return action.ActionResult{
		Success: true,
		Message: fmt.Sprintf("Secret %s scheduled for deletion (30-day recovery window)", secretId),
	}
}
