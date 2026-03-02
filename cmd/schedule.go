package cmd

import (
	"fmt"
	"sort"
	"time"

	pd "pagerduty/pkg/pagerduty"

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

		client := pd.NewClient(cfg.APIKey)
		ctx := cmd.Context()

		query, _ := cmd.Flags().GetString("user")
		userID, userName, err := client.ResolveUser(ctx, query)
		if err != nil {
			return err
		}

		now := time.Now()
		oncalls, err := client.ListUserOnCalls(ctx, userID, now.Format(time.RFC3339), now.AddDate(0, 3, 0).Format(time.RFC3339))
		if err != nil {
			return err
		}

		runs := pd.MergeOnCallRuns(oncalls)
		if len(runs) == 0 {
			fmt.Printf("No upcoming on-call shifts for %s in the next 3 months.\n", userName)
			return nil
		}

		// Keep only the first run per schedule.
		seen := map[string]bool{}
		var first []pd.OnCallRun
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

		client := pd.NewClient(cfg.APIKey)
		ctx := cmd.Context()

		query, _ := cmd.Flags().GetString("user")
		userID, userName, err := client.ResolveUser(ctx, query)
		if err != nil {
			return err
		}

		now := time.Now()
		oncalls, err := client.ListUserOnCalls(ctx, userID, now.AddDate(0, -3, 0).Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			return err
		}

		runs := pd.MergeOnCallRuns(oncalls)
		if len(runs) == 0 {
			fmt.Printf("No on-call shifts for %s in the last 3 months.\n", userName)
			return nil
		}

		// Keep only the last run per schedule.
		last := map[string]pd.OnCallRun{}
		for _, r := range runs {
			last[r.Schedule] = r
		}

		// Sort by start time for consistent output.
		var result []pd.OnCallRun
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

		client := pd.NewClient(cfg.APIKey)
		ctx := cmd.Context()

		serviceQuery, _ := cmd.Flags().GetString("service")

		var policyIDs []string
		if serviceQuery != "" {
			policyIDs, err = client.ResolveServicePolicies(ctx, serviceQuery)
		} else {
			policyIDs, err = client.CurrentUserPolicies(ctx)
		}
		if err != nil {
			return err
		}

		oncalls, err := client.ListPolicyOnCalls(ctx, policyIDs)
		if err != nil {
			return err
		}

		if len(oncalls) == 0 {
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
		for _, oc := range oncalls {
			key := oc.ScheduleID
			if key == "" {
				key = oc.PolicyID
			}
			if _, ok := schedules[key]; !ok {
				name := oc.ScheduleName
				if name == "" {
					name = oc.PolicyName
				}
				schedules[key] = &scheduleEntry{name: name}
				order = append(order, key)
			}
			end := ""
			if t, err := time.Parse(time.RFC3339, oc.End); err == nil {
				end = fmt.Sprintf(" (until %s)", t.Local().Format("Mon Jan 2  3:04 PM MST"))
			}
			schedules[key].users = append(schedules[key].users, oc.UserName+end)
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

func init() {
	scheduleNextCmd.Flags().String("user", "", "search by user name or email")
	scheduleLastCmd.Flags().String("user", "", "search by user name or email")
	scheduleNowCmd.Flags().String("service", "", "search by service name")

	scheduleCmd.AddCommand(scheduleNextCmd, scheduleLastCmd, scheduleNowCmd)
	rootCmd.AddCommand(scheduleCmd)
}
