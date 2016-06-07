package cluster

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/docker/libswarm/api"
	"github.com/docker/libswarm/cmd/swarmctl/common"
	"github.com/docker/libswarm/spec"
	"github.com/spf13/cobra"
)

var (
	editCmd = &cobra.Command{
		Use:   "edit <cluster name>",
		Short: "Edit a cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("cluster name missing")
			}

			editorPath := os.Getenv("EDITOR")
			if editorPath == "" {
				editorPath = "vi"
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			clusterPB, err := getCluster(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}
			specPB := &clusterPB.Spec

			cluster := &spec.ClusterConfig{}
			cluster.FromProto(specPB)

			original, err := ioutil.TempFile(os.TempDir(), "swarm-cluster-edit")
			if err != nil {
				return err
			}
			defer os.Remove(original.Name())

			if err := cluster.Write(original); err != nil {
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

			newCluster := &spec.ClusterConfig{}
			if err := newCluster.Read(updated); err != nil {
				return err
			}

			diff, err := newCluster.Diff(3, "old", "new", cluster)
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

			newClusterPB := newCluster.ToProto()
			newClusterPB.Annotations = specPB.Annotations
			r, err := c.UpdateCluster(common.Context(cmd), &api.UpdateClusterRequest{
				ClusterID:      clusterPB.ID,
				ClusterVersion: &clusterPB.Meta.Version,
				Spec:           newClusterPB,
			})
			if err != nil {
				return err
			}
			fmt.Println(r.Cluster.ID)
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
