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

		fmt.Printf("Next on-call for %s:\n", userName)
		for _, r := range runs {
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
		user, _ := cmd.Flags().GetString("user")
		fmt.Printf("schedule last (user=%q) - not yet implemented\n", user)
		return nil
	},
}

var scheduleNowCmd = &cobra.Command{
	Use:   "now",
	Short: "Show who is currently on call",
	RunE: func(cmd *cobra.Command, args []string) error {
		service, _ := cmd.Flags().GetString("service")
		fmt.Printf("schedule now (service=%q) - not yet implemented\n", service)
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

	// Keep only the first run per schedule.
	seen := map[string]bool{}
	filtered := runs[:0]
	for _, r := range runs {
		if !seen[r.Schedule] {
			seen[r.Schedule] = true
			filtered = append(filtered, r)
		}
	}
	return filtered
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
