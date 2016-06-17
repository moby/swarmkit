package cluster

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/spf13/cobra"
)

var (
	updateCmd = &cobra.Command{
		Use:   "update <cluster name>",
		Short: "Update a cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("cluster name missing")
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			cluster, err := getCluster(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}

			flags := cmd.Flags()
			spec := &cluster.Spec

			if flags.Changed("autoaccept") {
				autoaccept, err := flags.GetStringSlice("autoaccept")
				if err != nil {
					return err
				}

				// We are getting a whitelist, so make all of the autoaccepts false
				for _, policy := range spec.AcceptancePolicy.Policies {
					policy.Autoaccept = false

				}

				// For each of the roles handed to us by the client, make them true
				for _, role := range autoaccept {
					// Convert the role into a proto role
					apiRole, err := ca.FormatRole("swarm-" + role)
					if err != nil {
						return fmt.Errorf("unrecognized role %s", role)
					}
					// Attempt to find this role inside of the current policies
					found := false
					for _, policy := range spec.AcceptancePolicy.Policies {
						if policy.Role == apiRole {
							// We found a matching policy, let's update it
							policy.Autoaccept = true
							found = true
						}

					}
					// We didn't find this policy, create it
					if !found {
						newPolicy := &api.AcceptancePolicy_RoleAdmissionPolicy{
							Role:       apiRole,
							Autoaccept: true,
						}
						spec.AcceptancePolicy.Policies = append(spec.AcceptancePolicy.Policies, newPolicy)
					}
				}

			}

			if flags.Changed("secret") {
				secret, err := flags.GetStringSlice("secret")
				if err != nil || secret == nil || len(secret) < 1 {
					return err
				}
				// Using the defaut bcrypt cost
				hashedSecret, err := bcrypt.GenerateFromPassword([]byte(secret[0]), 0)
				if err != nil {
					return err
				}
				for _, policy := range spec.AcceptancePolicy.Policies {
					policy.Secret = &api.AcceptancePolicy_RoleAdmissionPolicy_HashedSecret{
						Data: hashedSecret,
						Alg:  "bcrypt",
					}
				}
			}
			if flags.Changed("certexpiry") {
				cePeriod, err := flags.GetDuration("certexpiry")
				if err != nil {
					return err
				}
				ceProtoPeriod := ptypes.DurationProto(cePeriod)
				spec.CAConfig.NodeCertExpiry = ceProtoPeriod
			}
			if flags.Changed("taskhistory") {
				taskHistory, err := flags.GetInt64("taskhistory")
				if err != nil {
					return err
				}
				spec.Orchestration.TaskHistoryRetentionLimit = taskHistory
			}
			if flags.Changed("heartbeatperiod") {
				hbPeriod, err := flags.GetDuration("heartbeatperiod")
				if err != nil {
					return err
				}
				spec.Dispatcher.HeartbeatPeriod = ptypes.DurationProto(hbPeriod)
			}

			r, err := c.UpdateCluster(common.Context(cmd), &api.UpdateClusterRequest{
				ClusterID:      cluster.ID,
				ClusterVersion: &cluster.Meta.Version,
				Spec:           spec,
			})
			if err != nil {
				return err
			}
			fmt.Println(r.Cluster.ID)
			return nil
		},
	}
)

func init() {
	updateCmd.Flags().StringSlice("autoaccept", nil, "Roles to automatically issue certificates for")
	updateCmd.Flags().StringSlice("secret", nil, "Secret required to join the cluster")
	updateCmd.Flags().Int64("taskhistory", 0, "Number of historic task entries to retain per slot or node")
	updateCmd.Flags().Duration("certexpiry", 24*30*3*time.Hour, "Duration node certificates will be valid for")
	updateCmd.Flags().Duration("heartbeatperiod", 0, "Period when heartbeat is expected to receive from agent")
}
