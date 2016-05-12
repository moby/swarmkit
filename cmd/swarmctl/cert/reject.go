package cert

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	rejectCmd = &cobra.Command{
		Use:   "reject",
		Short: "Reject a pending certificate request",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("certificate ID missing")
			}

			certID := args[0]

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			resp, err := c.GetRegisteredCertificate(common.Context(cmd),
				&api.GetRegisteredCertificateRequest{RegisteredCertificateID: certID})
			if err != nil {
				return fmt.Errorf("registered certificate %s not found", certID)
			}

			rCert := resp.RegisteredCertificate

			if rCert.Status.State != api.IssuanceStatePending {
				return fmt.Errorf("can only reject pending certificate requests")
			}

			rCert.Spec.DesiredState = api.IssuanceStateRejected

			_, err = c.UpdateRegisteredCertificate(common.Context(cmd), &api.UpdateRegisteredCertificateRequest{
				RegisteredCertificateID:      certID,
				RegisteredCertificateVersion: &rCert.Meta.Version,
				Spec: &rCert.Spec,
			})
			if err != nil {
				return err
			}

			fmt.Printf("Certificate request %s rejected\n", certID)
			return nil
		},
	}
)
