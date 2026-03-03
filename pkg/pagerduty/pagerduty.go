package pagerduty

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	pd "github.com/PagerDuty/go-pagerduty"
)

type Client struct {
	client  *pd.Client
	verbose bool
}

func NewClient(apiKey string, verbose bool) *Client {
	return &Client{client: pd.NewClient(apiKey), verbose: verbose}
}

func (c *Client) logf(format string, args ...interface{}) {
	if c.verbose {
		fmt.Fprintf(os.Stderr, "[api] "+format+"\n", args...)
	}
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

type Team struct {
	ID   string
	Name string
}

type User struct {
	ID    string
	Name  string
	Email string
	Teams []Team
}

// GetCurrentUser returns details about the authenticated user.
func (c *Client) GetCurrentUser(ctx context.Context) (*User, error) {
	c.logf("GET /users/me")
	user, err := c.client.GetCurrentUserWithContext(ctx, pd.GetCurrentUserOptions{
		Includes: []string{"teams"},
	})
	if err != nil {
		return nil, fmt.Errorf("getting current user: %w", err)
	}
	c.logf("  => %s (%s), %d teams", user.Name, user.Email, len(user.Teams))

	teams := make([]Team, len(user.Teams))
	for i, t := range user.Teams {
		teams[i] = Team{ID: t.ID, Name: t.Name}
	}
	return &User{ID: user.ID, Name: user.Name, Email: user.Email, Teams: teams}, nil
}

// ResolveUser looks up a user by query string (name or email). If query is
// empty, it returns the currently authenticated user.
func (c *Client) ResolveUser(ctx context.Context, query string) (id, name string, err error) {
	if query == "" {
		c.logf("GET /users/me")
		user, err := c.client.GetCurrentUserWithContext(ctx, pd.GetCurrentUserOptions{})
		if err != nil {
			return "", "", fmt.Errorf("getting current user: %w", err)
		}
		c.logf("  => %s", user.Name)
		return user.ID, user.Name, nil
	}

	c.logf("GET /users?query=%s", query)
	resp, err := c.client.ListUsersWithContext(ctx, pd.ListUsersOptions{Query: query})
	if err != nil {
		return "", "", fmt.Errorf("searching users: %w", err)
	}
	c.logf("  => %d users", len(resp.Users))

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
	c.logf("GET /users/me")
	user, err := c.client.GetCurrentUserWithContext(ctx, pd.GetCurrentUserOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting current user: %w", err)
	}

	// Look 3 months ahead to find all schedules the user is a member of.
	now := time.Now()
	c.logf("GET /oncalls user=%s since=%s until=%s", user.ID, now.Format(time.RFC3339), now.AddDate(0, 3, 0).Format(time.RFC3339))
	resp, err := c.client.ListOnCallsWithContext(ctx, pd.ListOnCallOptions{
		UserIDs: []string{user.ID},
		Since:   now.Format(time.RFC3339),
		Until:   now.AddDate(0, 3, 0).Format(time.RFC3339),
		Limit:   100,
	})
	if err != nil {
		return nil, fmt.Errorf("listing on-calls: %w", err)
	}
	c.logf("  => %d on-call entries", len(resp.OnCalls))

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
	c.logf("GET /services?query=%s", query)
	resp, err := c.client.ListServicesWithContext(ctx, pd.ListServiceOptions{Query: query})
	if err != nil {
		return nil, fmt.Errorf("searching services: %w", err)
	}
	c.logf("  => %d services", len(resp.Services))

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
	c.logf("GET /oncalls user=%s since=%s until=%s", userID, since, until)
	resp, err := c.client.ListOnCallsWithContext(ctx, pd.ListOnCallOptions{
		UserIDs: []string{userID},
		Since:   since,
		Until:   until,
		Limit:   100,
	})
	if err != nil {
		return nil, fmt.Errorf("listing on-calls: %w", err)
	}
	c.logf("  => %d on-call entries", len(resp.OnCalls))
	return convertOnCalls(resp.OnCalls), nil
}

// ListPolicyOnCalls returns current on-call entries for the given escalation
// policy IDs.
func (c *Client) ListPolicyOnCalls(ctx context.Context, policyIDs []string) ([]OnCall, error) {
	c.logf("GET /oncalls policies=%v earliest=true", policyIDs)
	resp, err := c.client.ListOnCallsWithContext(ctx, pd.ListOnCallOptions{
		EscalationPolicyIDs: policyIDs,
		Earliest:            true,
		Includes:            []string{"users"},
		Limit:               100,
	})
	if err != nil {
		return nil, fmt.Errorf("listing on-calls: %w", err)
	}
	c.logf("  => %d on-call entries", len(resp.OnCalls))
	return convertOnCalls(resp.OnCalls), nil
}

// ListPolicyOnCallsInRange returns on-call entries for the given escalation
// policy IDs within a time range.
func (c *Client) ListPolicyOnCallsInRange(ctx context.Context, policyIDs []string, since, until string) ([]OnCall, error) {
	c.logf("GET /oncalls policies=%v since=%s until=%s earliest=true", policyIDs, since, until)
	resp, err := c.client.ListOnCallsWithContext(ctx, pd.ListOnCallOptions{
		EscalationPolicyIDs: policyIDs,
		Earliest:            true,
		Includes:            []string{"users"},
		Since:               since,
		Until:               until,
		Limit:               100,
	})
	if err != nil {
		return nil, fmt.Errorf("listing on-calls: %w", err)
	}
	c.logf("  => %d on-call entries", len(resp.OnCalls))
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
			if sameDay(run.End, e.start, time.Local) || !e.start.After(run.End) {
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

func sameDay(a, b time.Time, loc *time.Location) bool {
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
}

type Incident struct {
	ID                 string
	Number             uint
	Title              string
	Status             string
	Service            string
	CreatedAt          string
	ResolvedAt         string
	EscalationPolicyID string
	Acks               []Ack
	Teams              []string
}

type Ack struct {
	Name string
	At   string
}

type IncidentNote struct {
	User      string
	Content   string
	CreatedAt string
}

// ListTeamIncidents returns all incidents for the given team IDs within a time range.
// The API's team_ids filter matches on service association, so we also filter
// client-side to only include incidents explicitly tagged with the requested teams.
func (c *Client) ListTeamIncidents(ctx context.Context, teamIDs, statuses []string, since, until string) ([]Incident, error) {
	c.logf("GET /incidents teams=%v statuses=%v since=%s until=%s", teamIDs, statuses, since, until)
	wanted := map[string]bool{}
	for _, id := range teamIDs {
		wanted[id] = true
	}

	var all []Incident
	opts := pd.ListIncidentsOptions{
		TeamIDs:  teamIDs,
		Since:    since,
		Until:    until,
		Statuses: statuses,
		Limit:    100,
	}
	for {
		resp, err := c.client.ListIncidentsWithContext(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("listing incidents: %w", err)
		}
		for _, inc := range resp.Incidents {
			var incTeams []string
			match := false
			for _, t := range inc.Teams {
				incTeams = append(incTeams, t.Summary)
				if wanted[t.ID] {
					match = true
				}
			}
			if !match {
				continue
			}
			acks := make([]Ack, len(inc.Acknowledgements))
			for i, a := range inc.Acknowledgements {
				acks[i] = Ack{Name: a.Acknowledger.Summary, At: a.At}
			}
			all = append(all, Incident{
				ID:                 inc.ID,
				Number:             inc.IncidentNumber,
				Title:              inc.Title,
				Status:             inc.Status,
				Service:            inc.Service.Summary,
				CreatedAt:          inc.CreatedAt,
				ResolvedAt:         inc.ResolvedAt,
				EscalationPolicyID: inc.EscalationPolicy.ID,
				Acks:               acks,
				Teams:              incTeams,
			})
		}
		if !resp.More {
			break
		}
		opts.Offset += opts.Limit
	}
	c.logf("  => %d incidents (after team filter)", len(all))
	return all, nil
}

// ListIncidentNotes returns all notes for the given incident.
func (c *Client) ListIncidentNotes(ctx context.Context, incidentID string) ([]IncidentNote, error) {
	c.logf("GET /incidents/%s/notes", incidentID)
	notes, err := c.client.ListIncidentNotesWithContext(ctx, incidentID)
	if err != nil {
		return nil, fmt.Errorf("listing incident notes: %w", err)
	}
	c.logf("  => %d notes", len(notes))
	out := make([]IncidentNote, len(notes))
	for i, n := range notes {
		out[i] = IncidentNote{
			User:      n.User.Summary,
			Content:   n.Content,
			CreatedAt: n.CreatedAt,
		}
	}
	return out, nil
}
