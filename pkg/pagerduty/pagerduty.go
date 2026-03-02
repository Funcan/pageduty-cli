package pagerduty

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	pd "github.com/PagerDuty/go-pagerduty"
)

type Client struct {
	client *pd.Client
}

func NewClient(apiKey string) *Client {
	return &Client{client: pd.NewClient(apiKey)}
}

type OnCall struct {
	UserName     string
	ScheduleID   string
	ScheduleName string
	PolicyID     string
	PolicyName   string
	Start        string
	End          string
}

type OnCallRun struct {
	Schedule string
	Start    time.Time
	End      time.Time
}

type User struct {
	Name  string
	Email string
	Teams []string
}

// GetCurrentUser returns details about the authenticated user.
func (c *Client) GetCurrentUser(ctx context.Context) (*User, error) {
	user, err := c.client.GetCurrentUserWithContext(ctx, pd.GetCurrentUserOptions{
		Includes: []string{"teams"},
	})
	if err != nil {
		return nil, fmt.Errorf("getting current user: %w", err)
	}

	teams := make([]string, len(user.Teams))
	for i, t := range user.Teams {
		teams[i] = t.Name
	}
	return &User{Name: user.Name, Email: user.Email, Teams: teams}, nil
}

// ResolveUser looks up a user by query string (name or email). If query is
// empty, it returns the currently authenticated user.
func (c *Client) ResolveUser(ctx context.Context, query string) (id, name string, err error) {
	if query == "" {
		user, err := c.client.GetCurrentUserWithContext(ctx, pd.GetCurrentUserOptions{})
		if err != nil {
			return "", "", fmt.Errorf("getting current user: %w", err)
		}
		return user.ID, user.Name, nil
	}

	resp, err := c.client.ListUsersWithContext(ctx, pd.ListUsersOptions{Query: query})
	if err != nil {
		return "", "", fmt.Errorf("searching users: %w", err)
	}

	switch len(resp.Users) {
	case 0:
		return "", "", fmt.Errorf("no users matching %q", query)
	case 1:
		return resp.Users[0].ID, resp.Users[0].Name, nil
	default:
		var lines []string
		for _, u := range resp.Users {
			lines = append(lines, fmt.Sprintf("  - %s (%s)", u.Name, u.Email))
		}
		return "", "", fmt.Errorf("multiple users match %q:\n%s", query, strings.Join(lines, "\n"))
	}
}

// CurrentUserPolicies returns escalation policy IDs for all schedules the
// authenticated user is a member of.
func (c *Client) CurrentUserPolicies(ctx context.Context) ([]string, error) {
	user, err := c.client.GetCurrentUserWithContext(ctx, pd.GetCurrentUserOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting current user: %w", err)
	}

	// Look 3 months ahead to find all schedules the user is a member of.
	now := time.Now()
	resp, err := c.client.ListOnCallsWithContext(ctx, pd.ListOnCallOptions{
		UserIDs: []string{user.ID},
		Since:   now.Format(time.RFC3339),
		Until:   now.AddDate(0, 3, 0).Format(time.RFC3339),
		Limit:   100,
	})
	if err != nil {
		return nil, fmt.Errorf("listing on-calls: %w", err)
	}

	seen := map[string]bool{}
	var ids []string
	for _, oc := range resp.OnCalls {
		id := oc.EscalationPolicy.ID
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no schedules found for current user in the next 3 months")
	}
	return ids, nil
}

// ResolveServicePolicies looks up a service by name and returns its
// escalation policy ID.
func (c *Client) ResolveServicePolicies(ctx context.Context, query string) ([]string, error) {
	resp, err := c.client.ListServicesWithContext(ctx, pd.ListServiceOptions{Query: query})
	if err != nil {
		return nil, fmt.Errorf("searching services: %w", err)
	}

	switch len(resp.Services) {
	case 0:
		return nil, fmt.Errorf("no services matching %q", query)
	case 1:
		return []string{resp.Services[0].EscalationPolicy.ID}, nil
	default:
		var lines []string
		for _, s := range resp.Services {
			lines = append(lines, fmt.Sprintf("  - %s", s.Name))
		}
		return nil, fmt.Errorf("multiple services match %q:\n%s", query, strings.Join(lines, "\n"))
	}
}

// ListUserOnCalls returns on-call entries for a user within a time range.
func (c *Client) ListUserOnCalls(ctx context.Context, userID, since, until string) ([]OnCall, error) {
	resp, err := c.client.ListOnCallsWithContext(ctx, pd.ListOnCallOptions{
		UserIDs: []string{userID},
		Since:   since,
		Until:   until,
		Limit:   100,
	})
	if err != nil {
		return nil, fmt.Errorf("listing on-calls: %w", err)
	}
	return convertOnCalls(resp.OnCalls), nil
}

// ListPolicyOnCalls returns current on-call entries for the given escalation
// policy IDs.
func (c *Client) ListPolicyOnCalls(ctx context.Context, policyIDs []string) ([]OnCall, error) {
	resp, err := c.client.ListOnCallsWithContext(ctx, pd.ListOnCallOptions{
		EscalationPolicyIDs: policyIDs,
		Earliest:            true,
		Includes:            []string{"users"},
		Limit:               100,
	})
	if err != nil {
		return nil, fmt.Errorf("listing on-calls: %w", err)
	}
	return convertOnCalls(resp.OnCalls), nil
}

func convertOnCalls(oncalls []pd.OnCall) []OnCall {
	out := make([]OnCall, len(oncalls))
	for i, oc := range oncalls {
		out[i] = OnCall{
			UserName:     oc.User.Name,
			ScheduleID:   oc.Schedule.ID,
			ScheduleName: oc.Schedule.Summary,
			PolicyID:     oc.EscalationPolicy.ID,
			PolicyName:   oc.EscalationPolicy.Summary,
			Start:        oc.Start,
			End:          oc.End,
		}
	}
	return out
}

// MergeOnCallRuns groups on-call entries by schedule, merges entries that share
// a calendar day (local time) into continuous runs, and returns all runs sorted
// by start time.
func MergeOnCallRuns(oncalls []OnCall) []OnCallRun {
	type entry struct {
		schedule string
		start    time.Time
		end      time.Time
	}

	bySchedule := map[string][]entry{}
	for _, oc := range oncalls {
		start, err1 := time.Parse(time.RFC3339, oc.Start)
		end, err2 := time.Parse(time.RFC3339, oc.End)
		if err1 != nil || err2 != nil {
			continue
		}
		name := oc.ScheduleName
		if name == "" {
			name = "(direct assignment)"
		}
		bySchedule[oc.ScheduleID] = append(bySchedule[oc.ScheduleID], entry{schedule: name, start: start, end: end})
	}

	var runs []OnCallRun
	for _, entries := range bySchedule {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].start.Before(entries[j].start)
		})

		run := OnCallRun{
			Schedule: entries[0].schedule,
			Start:    entries[0].start,
			End:      entries[0].end,
		}
		for _, e := range entries[1:] {
			if sameLocalDay(run.End, e.start) || !e.start.After(run.End) {
				if e.end.After(run.End) {
					run.End = e.end
				}
			} else {
				runs = append(runs, run)
				run = OnCallRun{Schedule: e.schedule, Start: e.start, End: e.end}
			}
		}
		runs = append(runs, run)
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].Start.Before(runs[j].Start)
	})

	return runs
}

func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}
