package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Query on-call schedules",
}

var scheduleNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Show when you're next on call",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		client := pagerduty.NewClient(cfg.APIKey)
		ctx := context.Background()

		query, _ := cmd.Flags().GetString("user")
		userID, userName, err := resolveUser(ctx, client, query)
		if err != nil {
			return err
		}

		now := time.Now()
		resp, err := client.ListOnCallsWithContext(ctx, pagerduty.ListOnCallOptions{
			UserIDs: []string{userID},
			Since:   now.Format(time.RFC3339),
			Until:   now.AddDate(0, 3, 0).Format(time.RFC3339),
			Limit:   100,
		})
		if err != nil {
			return fmt.Errorf("listing on-calls: %w", err)
		}

		runs := mergeOnCallRuns(resp.OnCalls)
		if len(runs) == 0 {
			fmt.Printf("No upcoming on-call shifts for %s in the next 3 months.\n", userName)
			return nil
		}

		// Keep only the first run per schedule.
		seen := map[string]bool{}
		var first []oncallRun
		for _, r := range runs {
			if !seen[r.Schedule] {
				seen[r.Schedule] = true
				first = append(first, r)
			}
		}

		fmt.Printf("Next on-call for %s:\n", userName)
		for _, r := range first {
			fmt.Println()
			fmt.Printf("  Schedule:  %s\n", r.Schedule)
			fmt.Printf("  Start:     %s\n", r.Start.Local().Format("Mon Jan 2  3:04 PM MST"))
			fmt.Printf("  End:       %s\n", r.End.Local().Format("Mon Jan 2  3:04 PM MST"))
		}

		return nil
	},
}

var scheduleLastCmd = &cobra.Command{
	Use:   "last",
	Short: "Show when you were last on call",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		client := pagerduty.NewClient(cfg.APIKey)
		ctx := context.Background()

		query, _ := cmd.Flags().GetString("user")
		userID, userName, err := resolveUser(ctx, client, query)
		if err != nil {
			return err
		}

		now := time.Now()
		resp, err := client.ListOnCallsWithContext(ctx, pagerduty.ListOnCallOptions{
			UserIDs: []string{userID},
			Since:   now.AddDate(0, -3, 0).Format(time.RFC3339),
			Until:   now.Format(time.RFC3339),
			Limit:   100,
		})
		if err != nil {
			return fmt.Errorf("listing on-calls: %w", err)
		}

		runs := mergeOnCallRuns(resp.OnCalls)
		if len(runs) == 0 {
			fmt.Printf("No on-call shifts for %s in the last 3 months.\n", userName)
			return nil
		}

		// Keep only the last run per schedule.
		last := map[string]oncallRun{}
		for _, r := range runs {
			last[r.Schedule] = r
		}

		// Sort by start time for consistent output.
		var result []oncallRun
		for _, r := range last {
			result = append(result, r)
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].Start.Before(result[j].Start)
		})

		fmt.Printf("Last on-call for %s:\n", userName)
		for _, r := range result {
			fmt.Println()
			fmt.Printf("  Schedule:  %s\n", r.Schedule)
			fmt.Printf("  Start:     %s\n", r.Start.Local().Format("Mon Jan 2  3:04 PM MST"))
			fmt.Printf("  End:       %s\n", r.End.Local().Format("Mon Jan 2  3:04 PM MST"))
		}

		return nil
	},
}

var scheduleNowCmd = &cobra.Command{
	Use:   "now",
	Short: "Show who is currently on call",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		client := pagerduty.NewClient(cfg.APIKey)
		ctx := context.Background()

		serviceQuery, _ := cmd.Flags().GetString("service")

		var policyIDs []string
		if serviceQuery != "" {
			policyIDs, err = resolveServicePolicies(ctx, client, serviceQuery)
		} else {
			policyIDs, err = currentUserPolicies(ctx, client)
		}
		if err != nil {
			return err
		}

		resp, err := client.ListOnCallsWithContext(ctx, pagerduty.ListOnCallOptions{
			EscalationPolicyIDs: policyIDs,
			Earliest:            true,
			Includes:            []string{"users"},
			Limit:               100,
		})
		if err != nil {
			return fmt.Errorf("listing on-calls: %w", err)
		}

		if len(resp.OnCalls) == 0 {
			fmt.Println("No one is currently on call.")
			return nil
		}

		// Group by schedule.
		type scheduleEntry struct {
			name  string
			users []string
		}
		schedules := map[string]*scheduleEntry{}
		var order []string
		for _, oc := range resp.OnCalls {
			key := oc.Schedule.ID
			if key == "" {
				key = oc.EscalationPolicy.ID
			}
			if _, ok := schedules[key]; !ok {
				name := oc.Schedule.Summary
				if name == "" {
					name = oc.EscalationPolicy.Summary
				}
				schedules[key] = &scheduleEntry{name: name}
				order = append(order, key)
			}
			end := ""
			if t, err := time.Parse(time.RFC3339, oc.End); err == nil {
				end = fmt.Sprintf(" (until %s)", t.Local().Format("Mon Jan 2  3:04 PM MST"))
			}
			schedules[key].users = append(schedules[key].users, oc.User.Name+end)
		}

		fmt.Println("Currently on-call:")
		for _, key := range order {
			s := schedules[key]
			fmt.Println()
			fmt.Printf("  %s:\n", s.name)
			for _, u := range s.users {
				fmt.Printf("    - %s\n", u)
			}
		}

		return nil
	},
}

func resolveUser(ctx context.Context, client *pagerduty.Client, query string) (string, string, error) {
	if query == "" {
		user, err := client.GetCurrentUserWithContext(ctx, pagerduty.GetCurrentUserOptions{})
		if err != nil {
			return "", "", fmt.Errorf("getting current user: %w", err)
		}
		return user.ID, user.Name, nil
	}

	resp, err := client.ListUsersWithContext(ctx, pagerduty.ListUsersOptions{Query: query})
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

func currentUserPolicies(ctx context.Context, client *pagerduty.Client) ([]string, error) {
	user, err := client.GetCurrentUserWithContext(ctx, pagerduty.GetCurrentUserOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting current user: %w", err)
	}

	// Look 3 months ahead to find all schedules the user is a member of.
	now := time.Now()
	resp, err := client.ListOnCallsWithContext(ctx, pagerduty.ListOnCallOptions{
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

func resolveServicePolicies(ctx context.Context, client *pagerduty.Client, query string) ([]string, error) {
	resp, err := client.ListServicesWithContext(ctx, pagerduty.ListServiceOptions{Query: query})
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

type oncallRun struct {
	Schedule string
	Start    time.Time
	End      time.Time
}

// mergeOnCallRuns groups on-call entries by schedule, merges entries that share
// a calendar day (local time) into continuous runs, and returns the first run
// per schedule sorted by start time.
func mergeOnCallRuns(oncalls []pagerduty.OnCall) []oncallRun {
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
		name := oc.Schedule.Summary
		if name == "" {
			name = "(direct assignment)"
		}
		key := oc.Schedule.ID
		bySchedule[key] = append(bySchedule[key], entry{schedule: name, start: start, end: end})
	}

	var runs []oncallRun
	for _, entries := range bySchedule {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].start.Before(entries[j].start)
		})

		run := oncallRun{
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
				run = oncallRun{Schedule: e.schedule, Start: e.start, End: e.end}
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

func init() {
	scheduleNextCmd.Flags().String("user", "", "search by user name or email")
	scheduleLastCmd.Flags().String("user", "", "search by user name or email")
	scheduleNowCmd.Flags().String("service", "", "search by service name")

	scheduleCmd.AddCommand(scheduleNextCmd, scheduleLastCmd, scheduleNowCmd)
	rootCmd.AddCommand(scheduleCmd)
}
