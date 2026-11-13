package temporal

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

// SendEmailWorkflow is a trivial demo workflow that a Schedule can trigger.
// It runs the SendEmailActivity once. Keeping it minimal so the UI has
// something concrete to schedule out of the box.
func SendEmailWorkflow(ctx workflow.Context, to string) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
	})

	if err := workflow.ExecuteActivity(ctx, SendEmailActivity, to).Get(ctx, nil); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

// SendEmailActivity simulates sending an email. Registered as an activity
// so it shows up nicely in Temporal's workflow history.
func SendEmailActivity(ctx context.Context, to string) (string, error) {
	activity.GetLogger(ctx).Info("sending email", "to", to)
	time.Sleep(500 * time.Millisecond)
	return fmt.Sprintf("email sent to %s", to), nil
}
