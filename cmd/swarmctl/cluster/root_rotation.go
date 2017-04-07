package cluster

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/docker/docker/cli/command"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/progress"
	"github.com/docker/docker/pkg/streamformatter"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	digest "github.com/opencontainers/go-digest"
	"golang.org/x/net/context"
)

const (
	certsRotatedStr = "  nodes rotated TLS certificates"
	rootsRotatedStr = "   nodes rotated CA certificates"
)

// rootRotationProgress outputs progress information for convergence of a root rotation.
func rootRotationProgress(ctx context.Context, client api.ControlClient, clusterID string, progressWriter io.WriteCloser) error {
	defer progressWriter.Close()

	progressOut := streamformatter.NewJSONStreamFormatter().NewProgressOutput(progressWriter, false)

	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt)
	defer signal.Stop(sigint)

	var (
		updater     *rootRotationProgressUpdater
		converged   bool
		convergedAt time.Time
		monitor     = 3 * time.Second
	)

	for {
		clusterResp, err := client.GetCluster(ctx, &api.GetClusterRequest{ClusterID: clusterID})
		if err != nil {
			return err
		}

		issuerInfo, err := ca.IssuerFromAPIRootCA(&clusterResp.Cluster.RootCA)
		if err != nil {
			return err
		}
		desiredTLSInfo := api.NodeTLSInfo{
			TrustRoot:           clusterResp.Cluster.RootCA.CACert,
			CertIssuerPublicKey: issuerInfo.PublicKey,
			CertIssuerSubject:   issuerInfo.Subject,
		}

		if updater == nil {
			updater = &rootRotationProgressUpdater{
				progressOut: progressOut,
			}
		}

		if converged && time.Since(convergedAt) >= monitor {
			return nil
		}

		nodesListResp, err := client.ListNodes(ctx, &api.ListNodesRequest{})
		if err != nil {
			return err
		}

		updater.update(&desiredTLSInfo, nodesListResp.Nodes, clusterResp.Cluster.RootCA.RootRotation == nil)
		converged = updater.done
		if converged {
			if convergedAt.IsZero() {
				convergedAt = time.Now()
			}
			wait := monitor - time.Since(convergedAt)
			if wait >= 0 {
				progressOut.WriteProgress(progress.Progress{
					// Ideally this would have no ID, but
					// the progress rendering code behaves
					// poorly on an "action" with no ID. It
					// returns the cursor to the beginning
					// of the line, so the first character
					// may be difficult to read. Then the
					// output is overwritten by the shell
					// prompt when the command finishes.
					ID:     "verify",
					Action: fmt.Sprintf("Waiting %d seconds to verify that the roots are stable...", wait/time.Second+1),
				})
			}
		} else {
			if !convergedAt.IsZero() {
				progressOut.WriteProgress(progress.Progress{
					ID:     "verify",
					Action: "detected another root rotation change",
				})
			}
			convergedAt = time.Time{}
		}

		select {
		case <-time.After(200 * time.Millisecond):
		case <-sigint:
			if !converged {
				progress.Message(progressOut, "", "Operation continuing in background.")
				progress.Message(progressOut, "", "Use `swarmctl cluster inspect default` to check progress.")
			}
			return nil
		}
	}
}

type rootRotationProgressUpdater struct {
	progressOut progress.Output
	initialized bool
	done        bool
}

func (r *rootRotationProgressUpdater) update(desiredTLSInfo *api.NodeTLSInfo, nodes []*api.Node, rootRotationDone bool) {
	// write the current desired root cert
	r.progressOut.WriteProgress(progress.Progress{
		ID:     "desired root digest",
		Action: digest.FromBytes(desiredTLSInfo.TrustRoot).String(),
	})

	if !r.initialized {
		// draw 2 progress bars, 1 for nodes with the correct cert, 1 for nodes with the correct trust root
		progress.Update(r.progressOut, certsRotatedStr, " ")
		progress.Update(r.progressOut, rootsRotatedStr, " ")
		r.initialized = true
	}

	// If we had reached a converged state, check if we are still converged.
	var certsRight, trustRootsRight int64
	for _, n := range nodes {
		if n.Description == nil || n.Description.TLSInfo == nil {
			continue
		}

		if bytes.Equal(n.Description.TLSInfo.CertIssuerPublicKey, desiredTLSInfo.CertIssuerPublicKey) &&
			bytes.Equal(n.Description.TLSInfo.CertIssuerSubject, desiredTLSInfo.CertIssuerSubject) {
			certsRight++
		}

		if bytes.Equal(n.Description.TLSInfo.TrustRoot, desiredTLSInfo.TrustRoot) {
			trustRootsRight++
		}
	}

	total := int64(len(nodes))
	certsAction := fmt.Sprintf("%d/%d done", certsRight, total)
	r.progressOut.WriteProgress(progress.Progress{
		ID:         certsRotatedStr,
		Action:     certsAction,
		Current:    certsRight,
		Total:      total,
		HideCounts: true,
	})

	if certsRight == total && rootRotationDone {
		rootsAction := fmt.Sprintf("%d/%d done", trustRootsRight, total)
		r.progressOut.WriteProgress(progress.Progress{
			ID:         rootsRotatedStr,
			Action:     rootsAction,
			Current:    trustRootsRight,
			Total:      total,
			HideCounts: true,
		})
		r.done = certsRight == total && trustRootsRight == total
	} else {
		rootsAction := fmt.Sprintf("%d/%d done", 0, total)
		r.progressOut.WriteProgress(progress.Progress{
			ID:         rootsRotatedStr,
			Action:     rootsAction,
			Current:    0,
			Total:      total,
			HideCounts: true,
		})
		r.done = false
	}
}

// WaitOnRootRotation waits for the root rotation to converge. It outputs a progress bar.
func WaitOnRootRotation(ctx context.Context, client api.ControlClient, clusterID string) error {
	errChan := make(chan error, 1)
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		errChan <- rootRotationProgress(ctx, client, clusterID, pipeWriter)
	}()

	err := jsonmessage.DisplayJSONMessagesToStream(pipeReader, command.NewOutStream(os.Stdout), nil)
	if err == nil {
		err = <-errChan
	}
	return err
}
