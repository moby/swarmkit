package task

import "github.com/spf13/cobra"

var (
	// Cmd exposes the top-level task command.
	Cmd = &cobra.Command{
		Use:   "task",
		Short: "Task management",
	}
)
