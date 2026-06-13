package parameters

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/clawscli/claws/internal/action"
	appaws "github.com/clawscli/claws/internal/aws"
	"github.com/clawscli/claws/internal/dao"
	"github.com/clawscli/claws/internal/sanitize"
)

const (
	operationViewParameterValue   = "ViewParameterValue"
	operationViewParameterHistory = "ViewParameterHistory"
)

func init() {
	action.Global.Register("ssm", "parameters", []action.Action{
		{
			Name:      "View Value",
			Shortcut:  "v",
			Type:      action.ActionTypeAPI,
			Operation: operationViewParameterValue,
		},
		{
			Name:      "View History",
			Shortcut:  "h",
			Type:      action.ActionTypeAPI,
			Operation: operationViewParameterHistory,
		},
		{
			Name:      "Delete",
			Shortcut:  "D",
			Type:      action.ActionTypeAPI,
			Operation: "DeleteParameter",
			Confirm:   action.ConfirmDangerous,
		},
	})

	action.RegisterExecutor("ssm", "parameters", executeParameterAction)
}

func executeParameterAction(ctx context.Context, act action.Action, resource dao.Resource) action.ActionResult {
	switch act.Operation {
	case operationViewParameterValue:
		return executeViewParameterValue(ctx, resource)
	case operationViewParameterHistory:
		return executeViewParameterHistory(ctx, resource)
	case "DeleteParameter":
		return executeDeleteParameter(ctx, resource)
	default:
		return action.UnknownOperationResult(act.Operation)
	}
}

func executeViewParameterValue(ctx context.Context, resource dao.Resource) action.ActionResult {
	client, err := getParameterClient(ctx)
	if err != nil {
		return action.ActionResult{Success: false, Error: err}
	}

	name := resource.GetID()
	output, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &name,
		WithDecryption: appaws.BoolPtr(true),
	})
	if err != nil {
		return action.ActionResult{Success: false, Error: fmt.Errorf("get parameter value: %w", err)}
	}
	if output.Parameter == nil {
		return action.ActionResult{Success: false, Error: fmt.Errorf("parameter value not found")}
	}

	return action.ActionResult{
		Success: true,
		Message: formatParameterValue(name, appaws.Str(output.Parameter.Value)),
	}
}

func executeViewParameterHistory(ctx context.Context, resource dao.Resource) action.ActionResult {
	client, err := getParameterClient(ctx)
	if err != nil {
		return action.ActionResult{Success: false, Error: err}
	}

	name := resource.GetID()
	paginator := ssm.NewGetParameterHistoryPaginator(client, &ssm.GetParameterHistoryInput{
		Name:           &name,
		WithDecryption: appaws.BoolPtr(true),
	})

	var history []types.ParameterHistory
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return action.ActionResult{Success: false, Error: fmt.Errorf("get parameter history: %w", err)}
		}
		history = append(history, page.Parameters...)
	}

	return action.ActionResult{
		Success: true,
		Message: formatParameterHistory(name, history),
	}
}

func getParameterClient(ctx context.Context) (*ssm.Client, error) {
	cfg, err := appaws.NewConfig(ctx)
	if err != nil {
		return nil, err
	}
	return ssm.NewFromConfig(cfg), nil
}

func formatParameterValue(name, value string) string {
	return fmt.Sprintf("Parameter: %s\n\n%s", sanitize.TerminalText(name), sanitize.MultilineTerminalText(value))
}

func formatParameterHistory(name string, history []types.ParameterHistory) string {
	var b strings.Builder
	b.WriteString("Parameter history: ")
	b.WriteString(sanitize.TerminalText(name))
	if len(history) == 0 {
		b.WriteString("\n\nNo history found")
		return b.String()
	}

	for _, item := range history {
		b.WriteString("\n\nVersion ")
		b.WriteString(strconv.FormatInt(item.Version, 10))
		if item.LastModifiedDate != nil {
			b.WriteString(" (")
			b.WriteString(item.LastModifiedDate.Format("2006-01-02 15:04:05"))
			b.WriteString(")")
		}
		b.WriteString("\n")
		b.WriteString(sanitize.MultilineTerminalText(appaws.Str(item.Value)))
	}
	return b.String()
}

func executeDeleteParameter(ctx context.Context, resource dao.Resource) action.ActionResult {
	client, err := getParameterClient(ctx)
	if err != nil {
		return action.ActionResult{Success: false, Error: err}
	}

	paramName := resource.GetID()
	input := &ssm.DeleteParameterInput{
		Name: &paramName,
	}

	_, err = client.DeleteParameter(ctx, input)
	if err != nil {
		return action.ActionResult{Success: false, Error: fmt.Errorf("delete parameter: %w", err)}
	}

	return action.ActionResult{
		Success: true,
		Message: fmt.Sprintf("Deleted parameter %s", paramName),
	}
}
