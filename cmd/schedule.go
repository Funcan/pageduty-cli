package cmd

import (
	"fmt"

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
		user, _ := cmd.Flags().GetString("user")
		fmt.Printf("schedule next (user=%q) - not yet implemented\n", user)
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

func init() {
	scheduleNextCmd.Flags().String("user", "", "search by user name or email")
	scheduleLastCmd.Flags().String("user", "", "search by user name or email")
	scheduleNowCmd.Flags().String("service", "", "search by service name")

	scheduleCmd.AddCommand(scheduleNextCmd, scheduleLastCmd, scheduleNowCmd)
	rootCmd.AddCommand(scheduleCmd)
}
