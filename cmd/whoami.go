package cmd

import (
	"fmt"
	"strings"

	pd "pagerduty/pkg/pagerduty"

	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show details about your PagerDuty account",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		client := pd.NewClient(cfg.APIKey)
		user, err := client.GetCurrentUser(cmd.Context())
		if err != nil {
			return err
		}

		fmt.Printf("Name:   %s\n", user.Name)
		fmt.Printf("Email:  %s\n", user.Email)
		if len(user.Teams) > 0 {
			names := make([]string, len(user.Teams))
			for i, t := range user.Teams {
				names[i] = t.Name
			}
			fmt.Printf("Teams:  %s\n", strings.Join(names, ", "))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
