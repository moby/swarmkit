package service

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/cmd/swarmctl/network"
	"github.com/docker/swarm-v2/spec"
	"github.com/spf13/cobra"
)

var (
	editCmd = &cobra.Command{
		Use:   "edit <service ID>",
		Short: "Edit a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("service ID missing")
			}

			editorPath := os.Getenv("EDITOR")
			if editorPath == "" {
				editorPath = "vi"
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			servicePB, err := getService(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}
			specPB := &servicePB.Spec
			if err := network.ResolveServiceNetworks(common.Context(cmd), c, specPB); err != nil {
				return err
			}

			service := &spec.ServiceConfig{}
			service.FromProto(specPB)

			original, err := ioutil.TempFile(os.TempDir(), "swarm-service-edit")
			if err != nil {
				return err
			}
			defer os.Remove(original.Name())

			if err := service.Write(original); err != nil {
				original.Close()
				return err
			}
			original.Close()

			editor := exec.Command(editorPath, original.Name())
			editor.Stdin = os.Stdin
			editor.Stdout = os.Stdout
			editor.Stderr = os.Stderr
			if err := editor.Run(); err != nil {
				return fmt.Errorf("there was a problem with the editor '%s': %v", editorPath, err)
			}

			updated, err := os.Open(original.Name())
			if err != nil {
				return err
			}
			defer updated.Close()

			newService := &spec.ServiceConfig{}
			if err := newService.Read(updated); err != nil {
				return err
			}

			diff, err := newService.Diff(3, "old", "new", service)
			if err != nil {
				return err
			}
			if diff == "" {
				fmt.Println("no changes detected")
				return nil
			}
			fmt.Print(diff)
			if !confirm() {
				return nil
			}

			r, err := c.UpdateService(common.Context(cmd), &api.UpdateServiceRequest{
				ServiceID:      servicePB.ID,
				ServiceVersion: &servicePB.Version,
				Spec:           newService.ToProto(),
			})
			if err != nil {
				return err
			}
			fmt.Println(r.Service.ID)
			return nil
		},
	}
)

func confirm() bool {
	fmt.Printf("Apply changes? [N/y] ")
	os.Stdout.Sync()

	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		return false
	}

	response = strings.ToLower(response)

	return response == "y" || response == "yes"
}
