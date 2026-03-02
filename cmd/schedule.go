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
			UserIDs:  []string{userID},
			Since:    now.Format(time.RFC3339),
			Until:    now.AddDate(0, 3, 0).Format(time.RFC3339),
			Earliest: true,
		})
		if err != nil {
			return fmt.Errorf("listing on-calls: %w", err)
		}

		if len(resp.OnCalls) == 0 {
			fmt.Printf("No upcoming on-call shifts for %s in the next 3 months.\n", userName)
			return nil
		}

		sort.Slice(resp.OnCalls, func(i, j int) bool {
			return resp.OnCalls[i].Start < resp.OnCalls[j].Start
		})

		fmt.Printf("Next on-call for %s:\n", userName)
		for _, oc := range resp.OnCalls {
			scheduleName := oc.Schedule.Summary
			if scheduleName == "" {
				scheduleName = "(direct assignment)"
			}

			fmt.Println()
			fmt.Printf("  Schedule:  %s\n", scheduleName)
			if start, err := time.Parse(time.RFC3339, oc.Start); err == nil {
				fmt.Printf("  Start:     %s\n", start.Local().Format("Mon Jan 2  3:04 PM MST"))
			}
			if end, err := time.Parse(time.RFC3339, oc.End); err == nil {
				fmt.Printf("  End:       %s\n", end.Local().Format("Mon Jan 2  3:04 PM MST"))
			}
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

func init() {
	scheduleNextCmd.Flags().String("user", "", "search by user name or email")
	scheduleLastCmd.Flags().String("user", "", "search by user name or email")
	scheduleNowCmd.Flags().String("service", "", "search by service name")

	scheduleCmd.AddCommand(scheduleNextCmd, scheduleLastCmd, scheduleNowCmd)
	rootCmd.AddCommand(scheduleCmd)
}
