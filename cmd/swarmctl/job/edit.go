package job

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/spec"
	"github.com/spf13/cobra"
)

var (
	editCmd = &cobra.Command{
		Use:   "edit <job ID>",
		Short: "Edit a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("job ID missing")
			}

			editorPath := os.Getenv("EDITOR")
			if editorPath == "" {
				editorPath = "vi"
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			id := common.LookupID(common.Context(cmd), c, api.Job{}, args[0])
			r, err := c.GetJob(common.Context(cmd), &api.GetJobRequest{JobID: id})
			if err != nil {
				return err
			}

			service := &spec.ServiceConfig{}
			service.FromJobSpec(r.Job.Spec)

			file, err := ioutil.TempFile(os.TempDir(), "swarm-job-edit")
			if err != nil {
				return err
			}
			defer file.Close()

			if err := yaml.NewEncoder(file).Encode(service); err != nil {
				return err
			}

			editor := exec.Command(editorPath, file.Name())
			editor.Stdin = os.Stdin
			editor.Stdout = os.Stdout
			editor.Stderr = os.Stderr
			if err := editor.Run(); err != nil {
				return err
			}

			file.Seek(0, 0)

			newService := &spec.ServiceConfig{}
			if err := newService.Parse(file); err != nil {
				return err
			}

			diff, err := newService.Diff("old", "new", service)
			if err != nil {
				return err
			}
			fmt.Print(diff)
			if !confirm() {
				return nil
			}

			ru, err := c.UpdateJob(common.Context(cmd), &api.UpdateJobRequest{JobID: id, Spec: newService.JobSpec()})
			if err != nil {
				return err
			}
			fmt.Println(ru.Job.ID)
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
