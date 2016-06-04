package root

import (
	"fmt"
	"reflect"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/cmd/swarmctl/network"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/metadata"
)

var (
	deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an app",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := readSpec(cmd.Flags())
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			// retrieve base64 auth token
			encodedAuth, err := cmd.Flags().GetString("registry-auth")
			if err != nil {
				return err
			}

			md := metadata.Pairs(
				"x-registry-auth", encodedAuth,
			)
			ctx := metadata.NewContext(common.Context(cmd), md)

			r, err := c.ListServices(ctx, &api.ListServicesRequest{})
			if err != nil {
				return err
			}

			services := map[string]*api.Service{}

			for _, j := range r.Services {
				if j.Spec.Annotations.Labels["namespace"] == s.Namespace {
					services[j.Spec.Annotations.Name] = j
				}
			}

			for _, serviceSpec := range s.ServiceSpecs() {
				if err := network.ResolveServiceNetworks(ctx, c, serviceSpec); err != nil {
					return err
				}

				if service, ok := services[serviceSpec.Annotations.Name]; ok && !reflect.DeepEqual(service.Spec, serviceSpec) {
					r, err := c.UpdateService(ctx, &api.UpdateServiceRequest{
						ServiceID:      service.ID,
						ServiceVersion: &service.Meta.Version,
						Spec:           serviceSpec,
					})
					if err != nil {
						fmt.Printf("%s: %v\n", serviceSpec.Annotations.Name, err)
						continue
					}
					fmt.Printf("%s: %s - UPDATED\n", serviceSpec.Annotations.Name, r.Service.ID)
					delete(services, serviceSpec.Annotations.Name)
				} else if !ok {
					r, err := c.CreateService(ctx, &api.CreateServiceRequest{Spec: serviceSpec})
					if err != nil {
						fmt.Printf("%s: %v\n", serviceSpec.Annotations.Name, err)
						continue
					}
					fmt.Printf("%s: %s - CREATED\n", serviceSpec.Annotations.Name, r.Service.ID)
				} else {
					// nothing to update
					delete(services, serviceSpec.Annotations.Name)
				}
			}

			for _, service := range services {
				_, err := c.RemoveService(ctx, &api.RemoveServiceRequest{ServiceID: service.ID})
				if err != nil {

					return err
				}
				fmt.Printf("%s: %s - REMOVED\n", service.Spec.Annotations.Name, service.ID)
			}

			return nil
		},
	}
)

func init() {
	deployCmd.Flags().StringP("file", "f", "docker.yml", "Spec file to deploy")
	deployCmd.Flags().String("registry-auth", "", "Auth token to use for registry login")
}
