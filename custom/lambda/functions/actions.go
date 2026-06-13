package functions

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	lambdaClient "github.com/clawscli/claws/custom/lambda"
	"github.com/clawscli/claws/internal/action"
	"github.com/clawscli/claws/internal/dao"
	"github.com/clawscli/claws/internal/sanitize"
)

func init() {
	// Register actions for Lambda functions
	action.Global.Register("lambda", "functions", []action.Action{
		{
			Name:      "Invoke",
			Shortcut:  "i",
			Type:      action.ActionTypeAPI,
			Operation: "InvokeFunction",
			Confirm:   action.ConfirmSimple,
		},
		{
			Name:      "Invoke (Dry Run)",
			Shortcut:  "I",
			Type:      action.ActionTypeAPI,
			Operation: "InvokeFunctionDryRun",
		},
		{
			Name:      "Delete",
			Shortcut:  "D",
			Type:      action.ActionTypeAPI,
			Operation: "DeleteFunction",
			Confirm:   action.ConfirmDangerous,
		},
	})

	// Register executor
	action.RegisterExecutor("lambda", "functions", executeFunctionAction)
}

// executeFunctionAction executes an action on a Lambda function
func executeFunctionAction(ctx context.Context, act action.Action, resource dao.Resource) action.ActionResult {
	switch act.Operation {
	case "InvokeFunction":
		return executeInvoke(ctx, resource, false)
	case "InvokeFunctionDryRun":
		return executeInvoke(ctx, resource, true)
	case "DeleteFunction":
		return executeDeleteFunction(ctx, resource)
	default:
		return action.UnknownOperationResult(act.Operation)
	}
}

func getLambdaClient(ctx context.Context) (*lambda.Client, error) {
	return lambdaClient.GetClient(ctx)
}

func executeInvoke(ctx context.Context, resource dao.Resource, dryRun bool) action.ActionResult {
	fn, ok := dao.UnwrapResource(resource).(*FunctionResource)
	if !ok {
		return action.InvalidResourceResult()
	}

	client, err := getLambdaClient(ctx)
	if err != nil {
		return action.FailResult(err)
	}

	functionName := fn.GetName()

	// Use empty JSON object as test payload
	payload := []byte("{}")

	input := &lambda.InvokeInput{
		FunctionName: &functionName,
		Payload:      payload,
	}

	if dryRun {
		input.InvocationType = lambdatypes.InvocationTypeDryRun
	} else {
		input.InvocationType = lambdatypes.InvocationTypeRequestResponse
	}

	output, err := client.Invoke(ctx, input)
	if err != nil {
		return action.FailResultf(err, "invoke function %s", functionName)
	}

	if dryRun {
		return action.SuccessResult(fmt.Sprintf("Dry run successful for %s (Status: %d)", functionName, output.StatusCode))
	}

	statusCode := output.StatusCode
	responsePreview := lambdaPayloadPreview(output.Payload)

	// Check for function error
	if output.FunctionError != nil && *output.FunctionError != "" {
		return action.FailResult(fmt.Errorf("function error: %s - %s", *output.FunctionError, responsePreview))
	}

	return action.SuccessResult(fmt.Sprintf("Invoked %s (Status: %d) Response: %s", functionName, statusCode, responsePreview))
}

func lambdaPayloadPreview(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	preview := string(payload)
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	return sanitize.SensitiveText(preview)
}

func executeDeleteFunction(ctx context.Context, resource dao.Resource) action.ActionResult {
	fn, ok := dao.UnwrapResource(resource).(*FunctionResource)
	if !ok {
		return action.InvalidResourceResult()
	}

	client, err := getLambdaClient(ctx)
	if err != nil {
		return action.FailResult(err)
	}

	functionName := fn.GetName()

	input := &lambda.DeleteFunctionInput{
		FunctionName: &functionName,
	}

	_, err = client.DeleteFunction(ctx, input)
	if err != nil {
		return action.FailResultf(err, "delete function %s", functionName)
	}

	return action.SuccessResult(fmt.Sprintf("Deleted function %s", functionName))
}
