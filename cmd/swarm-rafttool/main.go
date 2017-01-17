package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	mainCmd = &cobra.Command{
		Use:   os.Args[0],
		Short: "Tool to translate and decrypt the raft logs of a swarm manager",
	}

	decryptCmd = &cobra.Command{
		Use:   "decrypt <output directory>",
		Short: "Decrypt a swarm manager's raft logs to an optional directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%s command does not take any arguments", os.Args[0])
			}

			outDir, err := cmd.Flags().GetString("output-dir")
			if err != nil {
				return err
			}

			stateDir, err := cmd.Flags().GetString("state-dir")
			if err != nil {
				return err
			}

			unlockKey, err := cmd.Flags().GetString("unlock-key")
			if err != nil {
				return err
			}

			return decryptRaftData(stateDir, outDir, unlockKey)
		},
	}
)

func init() {
	mainCmd.PersistentFlags().StringP("state-dir", "d", "./swarmkitstate", "State directory")
	mainCmd.PersistentFlags().String("unlock-key", "", "Unlock key, if raft logs are encrypted")
	decryptCmd.Flags().StringP("output-dir", "o", "plaintext_raft", "Output directory for decrypted raft logs")
	mainCmd.AddCommand(
		decryptCmd,
	)
}

func main() {
	if _, err := mainCmd.ExecuteC(); err != nil {
		os.Exit(-1)
	}
}
