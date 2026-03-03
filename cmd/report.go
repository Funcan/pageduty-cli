package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	pd "pagerduty/pkg/pagerduty"

	"github.com/spf13/cobra"
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate a markdown incident report for your team(s)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		client := pd.NewClient(cfg.APIKey, verbose)
		ctx := cmd.Context()

		user, err := client.GetCurrentUser(ctx)
		if err != nil {
			return err
		}

		if len(user.Teams) == 0 {
			return fmt.Errorf("no teams found for current user")
		}

		teams := user.Teams
		if teamsFlag, _ := cmd.Flags().GetString("teams"); teamsFlag != "" {
			filters := strings.Split(teamsFlag, ",")
			var filtered []pd.Team
			for _, t := range teams {
				for _, f := range filters {
					if strings.Contains(strings.ToLower(t.Name), strings.ToLower(strings.TrimSpace(f))) {
						filtered = append(filtered, t)
						break
					}
				}
			}
			if len(filtered) == 0 {
				return fmt.Errorf("no teams matching %q", teamsFlag)
			}
			teams = filtered
		}

		teamIDs := make([]string, len(teams))
		teamNames := make([]string, len(teams))
		for i, t := range teams {
			teamIDs[i] = t.ID
			teamNames[i] = t.Name
		}

		fromFlag, _ := cmd.Flags().GetString("from")
		toFlag, _ := cmd.Flags().GetString("to")

		var since, until time.Time
		if fromFlag != "" || toFlag != "" {
			if fromFlag == "" || toFlag == "" {
				return fmt.Errorf("both --from and --to are required")
			}
			since, err = time.ParseInLocation("2006-01-02", fromFlag, time.Local)
			if err != nil {
				return fmt.Errorf("invalid --from date: %w", err)
			}
			until, err = time.ParseInLocation("2006-01-02", toFlag, time.Local)
			if err != nil {
				return fmt.Errorf("invalid --to date: %w", err)
			}
			// Include the entire "to" day.
			until = until.AddDate(0, 0, 1)
		} else {
			// Default to last on-call shift, using the same logic as
			// `schedule last`: keep the last run per schedule, then use
			// the overall start/end as the reporting window.
			now := time.Now()
			oncalls, err := client.ListUserOnCalls(ctx, user.ID, now.AddDate(0, -3, 0).Format(time.RFC3339), now.Format(time.RFC3339))
			if err != nil {
				return err
			}
			runs := pd.MergeOnCallRuns(oncalls)
			if len(runs) == 0 {
				return fmt.Errorf("no on-call shifts found in the last 3 months; use --from and --to to specify a range")
			}
			// Keep only the last run per schedule.
			last := map[string]pd.OnCallRun{}
			for _, r := range runs {
				last[r.Schedule] = r
			}
			for _, r := range last {
				if since.IsZero() || r.Start.Before(since) {
					since = r.Start
				}
				if r.End.After(until) {
					until = r.End
				}
			}
		}

		incidents, err := client.ListTeamIncidents(ctx, teamIDs, since.Format(time.RFC3339), until.Format(time.RFC3339))
		if err != nil {
			return err
		}

		// Also fetch incidents that were already open at the start of the period.
		noPrevious, _ := cmd.Flags().GetBool("no-previous")
		carryoverCount := 0
		if !noPrevious {
			carryover, err := client.ListTeamIncidents(ctx, teamIDs, since.AddDate(0, -3, 0).Format(time.RFC3339), since.Format(time.RFC3339))
			if err != nil {
				return err
			}
			seen := map[string]bool{}
			for _, inc := range incidents {
				seen[inc.ID] = true
			}
			for _, inc := range carryover {
				if seen[inc.ID] {
					continue
				}
				keep := false
				reason := ""
				if inc.Status != "resolved" {
					keep = true
					reason = fmt.Sprintf("status=%s", inc.Status)
				} else if resolved, err := time.Parse(time.RFC3339, inc.ResolvedAt); err == nil && !resolved.Before(since) {
					keep = true
					reason = fmt.Sprintf("resolved=%s (after period start)", inc.ResolvedAt)
				}
				if keep {
					incidents = append(incidents, inc)
					carryoverCount++
					if verbose {
						fmt.Fprintf(os.Stderr, "[carryover] INC-%d %s (%s)\n", inc.Number, inc.Title, reason)
					}
				}
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "[carryover] %d incidents carried over from before period\n", carryoverCount)
			}
		}

		sort.Slice(incidents, func(i, j int) bool {
			return incidents[i].CreatedAt < incidents[j].CreatedAt
		})

		if serviceFlag, _ := cmd.Flags().GetString("service"); serviceFlag != "" {
			filters := strings.Split(serviceFlag, ",")
			var filtered []pd.Incident
			for _, inc := range incidents {
				for _, f := range filters {
					if strings.Contains(strings.ToLower(inc.Service), strings.ToLower(strings.TrimSpace(f))) {
						filtered = append(filtered, inc)
						break
					}
				}
			}
			incidents = filtered
		}

		// Look up who was on-call for each incident's escalation policy.
		policySet := map[string]bool{}
		for _, inc := range incidents {
			if inc.EscalationPolicyID != "" {
				policySet[inc.EscalationPolicyID] = true
			}
		}
		var policyIDs []string
		for id := range policySet {
			policyIDs = append(policyIDs, id)
		}
		var oncalls []pd.OnCall
		if len(policyIDs) > 0 {
			oncalls, err = client.ListPolicyOnCallsInRange(ctx, policyIDs, since.Format(time.RFC3339), until.Format(time.RFC3339))
			if err != nil {
				return err
			}
		}

		// Print report header.
		dateFmt := "Mon Jan 2, 2006"
		fmt.Println("# On-Call Incident Report")
		fmt.Println()
		fmt.Printf("**Period:** %s - %s\n", since.Local().Format(dateFmt), until.Local().Format(dateFmt))
		fmt.Printf("**Teams:** %s\n", strings.Join(teamNames, ", "))
		fmt.Println()

		if len(incidents) == 0 {
			fmt.Printf("No incidents for %s in the given period.\n", strings.Join(teamNames, ", "))
			return nil
		}

		timeFmt := "Mon Jan 2 3:04 PM MST"
		for i, inc := range incidents {
			fmt.Printf("## INC-%d: %s\n", inc.Number, inc.Title)
			fmt.Println()

			fmt.Printf("- **Service:** %s\n", inc.Service)
			created, createdErr := time.Parse(time.RFC3339, inc.CreatedAt)
			if createdErr == nil {
				fmt.Printf("- **Created:** %s\n", created.Local().Format(timeFmt))
			}
			fmt.Printf("- **Status:** %s\n", inc.Status)
			if inc.Status == "resolved" {
				if resolved, err := time.Parse(time.RFC3339, inc.ResolvedAt); err == nil {
					fmt.Printf("- **Resolved:** %s\n", resolved.Local().Format(timeFmt))
					if createdErr == nil {
						fmt.Printf("- **Duration:** %s\n", formatDuration(resolved.Sub(created)))
					}
				}
			}
			if len(inc.Teams) > 0 {
				fmt.Printf("- **Teams:** %s\n", strings.Join(inc.Teams, ", "))
			}

			// Find who was on-call when this incident was created.
			if createdErr == nil && inc.EscalationPolicyID != "" {
				oncallName := ""
				for _, oc := range oncalls {
					if oc.PolicyID != inc.EscalationPolicyID {
						continue
					}
					ocStart, err1 := time.Parse(time.RFC3339, oc.Start)
					ocEnd, err2 := time.Parse(time.RFC3339, oc.End)
					if err1 != nil || err2 != nil {
						continue
					}
					if !created.Before(ocStart) && created.Before(ocEnd) {
						oncallName = oc.UserName
						break
					}
				}
				// If no match from bulk data, query for this specific time.
				if oncallName == "" {
					t := created.Format(time.RFC3339)
					spot, err := client.ListPolicyOnCallsInRange(ctx, []string{inc.EscalationPolicyID}, t, t)
					if err == nil && len(spot) > 0 {
						oncallName = spot[0].UserName
					}
				}
				if oncallName != "" {
					fmt.Printf("- **On-call:** %s\n", oncallName)
				}
			}

			if len(inc.Acks) > 0 {
				var ackParts []string
				for _, a := range inc.Acks {
					if t, err := time.Parse(time.RFC3339, a.At); err == nil {
						ackParts = append(ackParts, fmt.Sprintf("%s (%s)", a.Name, t.Local().Format(timeFmt)))
					} else {
						ackParts = append(ackParts, a.Name)
					}
				}
				fmt.Printf("- **Acknowledged by:** %s\n", strings.Join(ackParts, ", "))
			} else {
				fmt.Println("- **Acknowledged by:** unacknowledged")
			}

			notes, err := client.ListIncidentNotes(ctx, inc.ID)
			if err != nil {
				return err
			}
			if len(notes) > 0 {
				fmt.Println()
				fmt.Println("### Notes")
				fmt.Println()
				for _, n := range notes {
					if t, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil {
						fmt.Printf("- **%s** (%s): %s\n", n.User, t.Local().Format(timeFmt), n.Content)
					} else {
						fmt.Printf("- **%s**: %s\n", n.User, n.Content)
					}
				}
			}

			if i < len(incidents)-1 {
				fmt.Println()
				fmt.Println("---")
				fmt.Println()
			}
		}

		// Statistics.
		openAtStart := 0
		newCount := 0
		closed := 0
		leftOpen := 0
		for _, inc := range incidents {
			created, err := time.Parse(time.RFC3339, inc.CreatedAt)
			if err == nil && created.Before(since) {
				openAtStart++
			} else {
				newCount++
			}
			if inc.Status == "resolved" {
				closed++
			} else {
				leftOpen++
			}
		}
		fmt.Println()
		fmt.Println("---")
		fmt.Println()
		fmt.Println("## Statistics")
		fmt.Println()
		rows := [][]string{{"Metric", "Count"}}
		if !noPrevious {
			rows = append(rows, []string{"Open at start of period", fmt.Sprintf("%d", openAtStart)})
		}
		rows = append(rows,
			[]string{"New incidents", fmt.Sprintf("%d", newCount)},
			[]string{"Closed", fmt.Sprintf("%d", closed)},
			[]string{"Left open", fmt.Sprintf("%d", leftOpen)},
		)
		printTable(rows)

		return nil
	},
}

func init() {
	reportCmd.Flags().String("from", "", "start date (YYYY-MM-DD)")
	reportCmd.Flags().String("to", "", "end date (YYYY-MM-DD)")
	reportCmd.Flags().String("teams", "", "comma-separated team names to filter by")
	reportCmd.Flags().String("service", "", "comma-separated service names to filter by")
	reportCmd.Flags().Bool("no-previous", false, "exclude incidents that were open before the period")
	rootCmd.AddCommand(reportCmd)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	return strings.Join(parts, " ")
}

// printTable prints a markdown table with space-padded columns so it reads
// well in plain text. The first row is treated as the header.
func printTable(rows [][]string) {
	if len(rows) == 0 {
		return
	}
	cols := len(rows[0])
	widths := make([]int, cols)
	for _, row := range rows {
		for c := 0; c < cols && c < len(row); c++ {
			if len(row[c]) > widths[c] {
				widths[c] = len(row[c])
			}
		}
	}
	for i, row := range rows {
		fmt.Print("|")
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			fmt.Printf(" %-*s |", widths[c], cell)
		}
		fmt.Println()
		if i == 0 {
			fmt.Print("|")
			for c := 0; c < cols; c++ {
				fmt.Printf(" %s |", strings.Repeat("-", widths[c]))
			}
			fmt.Println()
		}
	}
}
