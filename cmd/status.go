package cmd

import (
	"fmt"
	"strings"
	"time"

	pd "pagerduty/pkg/pagerduty"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show currently unresolved incidents",
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

		incidents, err := client.ListTeamIncidents(ctx, teamIDs, []string{"triggered", "acknowledged"}, "", "")
		if err != nil {
			return err
		}

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

		if len(incidents) == 0 {
			fmt.Printf("No open incidents for %s.\n", strings.Join(teamNames, ", "))
			return nil
		}

		timeFmt := "Mon Jan 2 3:04 PM MST"
		fmt.Printf("Open incidents for %s:\n", strings.Join(teamNames, ", "))
		for _, inc := range incidents {
			fmt.Println()
			fmt.Printf("  INC-%d: %s\n", inc.Number, inc.Title)
			fmt.Printf("    Service:  %s\n", inc.Service)
			fmt.Printf("    Status:   %s\n", inc.Status)
			if created, err := time.Parse(time.RFC3339, inc.CreatedAt); err == nil {
				fmt.Printf("    Created:  %s\n", created.Local().Format(timeFmt))
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
				fmt.Printf("    Acked by: %s\n", strings.Join(ackParts, ", "))
			} else {
				fmt.Printf("    Acked by: unacknowledged\n")
			}

			notes, err := client.ListIncidentNotes(ctx, inc.ID)
			if err != nil {
				return err
			}
			if len(notes) > 0 {
				fmt.Printf("    Notes:\n")
				for _, n := range notes {
					if t, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil {
						fmt.Printf("      - %s (%s): %s\n", n.User, t.Local().Format(timeFmt), n.Content)
					} else {
						fmt.Printf("      - %s: %s\n", n.User, n.Content)
					}
				}
			}
		}

		unacked := 0
		for _, inc := range incidents {
			if len(inc.Acks) == 0 {
				unacked++
			}
		}
		fmt.Println()
		fmt.Printf("%d incidents open / %d incidents unacknowledged\n", len(incidents), unacked)

		return nil
	},
}

func init() {
	statusCmd.Flags().String("teams", "", "comma-separated team names to filter by")
	statusCmd.Flags().String("service", "", "comma-separated service names to filter by")
	rootCmd.AddCommand(statusCmd)
}
