package root

import (
	"fmt"
	"reflect"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
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

			r, err := c.ListServices(common.Context(cmd), &api.ListServicesRequest{})
			if err != nil {
				return err
			}

			services := map[string]*api.Service{}

			for _, j := range r.Services {
				if j.Spec.Meta.Labels["namespace"] == s.Namespace {
					services[j.Spec.Meta.Name] = j
				}
			}

			for _, serviceSpec := range s.ServiceSpecs() {
				if service, ok := services[serviceSpec.Meta.Name]; ok && !reflect.DeepEqual(service.Spec, serviceSpec) {
					r, err := c.UpdateService(common.Context(cmd), &api.UpdateServiceRequest{ServiceID: service.ID, Spec: serviceSpec})
					if err != nil {
						fmt.Printf("%s: %v", serviceSpec.Meta.Name, err)
						continue
					}
					fmt.Printf("%s: %s - UPDATED\n", serviceSpec.Meta.Name, r.Service.ID)
					delete(services, serviceSpec.Meta.Name)
				} else if !ok {
					r, err := c.CreateService(common.Context(cmd), &api.CreateServiceRequest{Spec: serviceSpec})
					if err != nil {
						fmt.Printf("%s: %v", serviceSpec.Meta.Name, err)
						continue
					}
					fmt.Printf("%s: %s - CREATED\n", serviceSpec.Meta.Name, r.Service.ID)
				} else {
					// nothing to update
					delete(services, serviceSpec.Meta.Name)
				}
			}

			for _, service := range services {
				_, err := c.RemoveService(common.Context(cmd), &api.RemoveServiceRequest{ServiceID: service.ID})
				if err != nil {

					return err
				}
				fmt.Printf("%s: %s - REMOVED\n", service.Spec.Meta.Name, service.ID)
			}
			return nil
		},
	}
)

func init() {
	deployCmd.Flags().StringP("file", "f", "docker.yml", "Spec file to deploy")
}
