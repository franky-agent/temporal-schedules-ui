package temporal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/client"
)

// ScheduleService wraps ScheduleClient with typed helpers used by the UI.
// It reads from a ReconnectingClient so calls fail gracefully when Temporal
// is not yet reachable.
type ScheduleService struct {
	rc *ReconnectingClient
}

func NewScheduleService(rc *ReconnectingClient) *ScheduleService {
	return &ScheduleService{rc: rc}
}

// client returns the current Temporal client or an error if not connected.
func (s *ScheduleService) client() (client.Client, error) {
	return s.rc.Client()
}

// ScheduleSummary is the flat view-model the templates render.
type ScheduleSummary struct {
	ID              string
	WorkflowType    string
	Spec            string
	Paused          bool
	Note            string
	NextActionTimes []time.Time
	RecentActions   []time.Time
}

// List returns all schedules as a flat summary list.
func (s *ScheduleService) List(ctx context.Context) ([]ScheduleSummary, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	view, err := c.ScheduleClient().List(ctx, client.ScheduleListOptions{PageSize: 100})
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}

	var out []ScheduleSummary
	for view.HasNext() {
		e, err := view.Next()
		if err != nil {
			return nil, fmt.Errorf("iterate schedules: %w", err)
		}
		out = append(out, summarize(e.ID, e.Spec, e.Paused, e.Note, e.WorkflowType.Name, e.NextActionTimes, e.RecentActions))
	}
	return out, nil
}

// Detail is the rich view-model returned by Describe.
type Detail struct {
	ScheduleSummary
	CreatedAt       time.Time
	LastUpdateAt    time.Time
	NumActions      int
	NumSkipped      int
	NumMissed       int
	PausedOnFailure bool
	Overlap         string
	WorkflowID      string
	TaskQueue       string
}

// Describe fetches full details for one schedule.
func (s *ScheduleService) Describe(ctx context.Context, id string) (*Detail, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	desc, err := c.ScheduleClient().GetHandle(ctx, id).Describe(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe %s: %w", id, err)
	}

	d := &Detail{
		ScheduleSummary: ScheduleSummary{
			ID:   id,
			Spec: specString(desc.Schedule.Spec),
		},
		NumActions: desc.Info.NumActions,
		NumSkipped: desc.Info.NumActionsSkippedOverlap,
		NumMissed:  desc.Info.NumActionsMissedCatchupWindow,
		CreatedAt:  desc.Info.CreatedAt,
		LastUpdateAt: desc.Info.LastUpdateAt,
	}

	if desc.Schedule.State != nil {
		d.Paused = desc.Schedule.State.Paused
		d.Note = desc.Schedule.State.Note
	}
	if desc.Schedule.Policy != nil {
		d.Overlap = strings.TrimPrefix(desc.Schedule.Policy.Overlap.String(), "SCHEDULE_OVERLAP_POLICY_")
		d.PausedOnFailure = desc.Schedule.Policy.PauseOnFailure
	}
	if desc.Schedule.Spec != nil {
		d.NextActionTimes = desc.Info.NextActionTimes
		for _, a := range desc.Info.RecentActions {
			d.RecentActions = append(d.RecentActions, a.ActualTime)
		}
	}
	if act, ok := desc.Schedule.Action.(*client.ScheduleWorkflowAction); ok {
		d.WorkflowType = workflowName(act.Workflow)
		d.WorkflowID = act.ID
		d.TaskQueue = act.TaskQueue
	}
	return d, nil
}

// CreateInput is the form payload for creating a schedule.
type CreateInput struct {
	ID           string
	Workflow     string
	WorkflowID   string
	TaskQueue    string
	Cron         string
	Interval     string
	Note         string
	Paused       bool
	Args         string
}

// Create registers a new schedule.
func (s *ScheduleService) Create(ctx context.Context, in CreateInput) error {
	if in.ID == "" {
		return errors.New("schedule id is required")
	}
	if in.Workflow == "" {
		return errors.New("workflow is required")
	}
	if in.TaskQueue == "" {
		in.TaskQueue = TaskQueue
	}
	if in.Cron == "" && in.Interval == "" {
		return errors.New("either cron or interval must be set")
	}

	spec := client.ScheduleSpec{}
	if in.Cron != "" {
		spec.CronExpressions = []string{in.Cron}
	}
	if in.Interval != "" {
		d, err := time.ParseDuration(in.Interval)
		if err != nil {
			return fmt.Errorf("invalid interval %q: %w", in.Interval, err)
		}
		spec.Intervals = []client.ScheduleIntervalSpec{{Every: d}}
	}

	args := []any{}
	if in.Args != "" {
		args = append(args, in.Args)
	}

	c, err := s.client()
	if err != nil {
		return err
	}
	_, err = c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:     in.ID,
		Spec:   spec,
		Paused: in.Paused,
		Note:   in.Note,
		Action: &client.ScheduleWorkflowAction{
			ID:        in.WorkflowID,
			Workflow:  in.Workflow,
			Args:      args,
			TaskQueue: in.TaskQueue,
		},
	})
	if err != nil {
		return fmt.Errorf("create schedule: %w", err)
	}
	return nil
}

// Pause pauses a schedule.
func (s *ScheduleService) Pause(ctx context.Context, id, note string) error {
	opts := client.SchedulePauseOptions{}
	if note != "" {
		opts.Note = note
	}
	c, err := s.client()
	if err != nil {
		return err
	}
	return c.ScheduleClient().GetHandle(ctx, id).Pause(ctx, opts)
}

// Unpause resumes a schedule.
func (s *ScheduleService) Unpause(ctx context.Context, id, note string) error {
	opts := client.ScheduleUnpauseOptions{}
	if note != "" {
		opts.Note = note
	}
	c, err := s.client()
	if err != nil {
		return err
	}
	return c.ScheduleClient().GetHandle(ctx, id).Unpause(ctx, opts)
}

// Trigger fires one action immediately.
func (s *ScheduleService) Trigger(ctx context.Context, id string) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	return c.ScheduleClient().GetHandle(ctx, id).Trigger(ctx, client.ScheduleTriggerOptions{})
}

// Delete removes a schedule.
func (s *ScheduleService) Delete(ctx context.Context, id string) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	return c.ScheduleClient().GetHandle(ctx, id).Delete(ctx)
}

// --- helpers ---

func summarize(id string, spec *client.ScheduleSpec, paused bool, note string,
	workflowName string, next []time.Time, recent []client.ScheduleActionResult) ScheduleSummary {

	sum := ScheduleSummary{
		ID:              id,
		WorkflowType:    workflowName,
		Paused:          paused,
		Note:            note,
		Spec:            specString(spec),
		NextActionTimes: next,
	}
	for _, a := range recent {
		sum.RecentActions = append(sum.RecentActions, a.ActualTime)
	}
	return sum
}

func specString(spec *client.ScheduleSpec) string {
	if spec == nil {
		return ""
	}
	var parts []string
	if len(spec.CronExpressions) > 0 {
		parts = append(parts, "cron:"+strings.Join(spec.CronExpressions, ", "))
	}
	for _, iv := range spec.Intervals {
		parts = append(parts, "every "+iv.Every.String())
	}
	if len(spec.Calendars) > 0 {
		parts = append(parts, fmt.Sprintf("%d calendar spec(s)", len(spec.Calendars)))
	}
	return strings.Join(parts, " · ")
}

// workflowName converts a Workflow field (function or string) to a type name.
func workflowName(w any) string {
	switch v := w.(type) {
	case string:
		return v
	case func():
		return ""
	default:
		return fmt.Sprintf("%v", w)
	}
}
