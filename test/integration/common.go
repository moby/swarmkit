package integration

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func getPort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func killProcesses(processes []*os.Process) {
	for _, process := range processes {
		process.Kill()
		process.Wait()
	}
}

// Test represents an integration Test
type Test struct {
	managerProcesses []*os.Process
	managerPorts     []int
	agentProcesses   []*os.Process
}

// StartManagers starts `instances` managers binding on a random port each.
// Fills ManagerProcesses and ManagerPorts.
func (t *Test) StartManagers(instances int) {
	for i := 0; i < instances; i++ {
		port := getPort()
		tmp, err := ioutil.TempDir("", "swarm_integration")
		if err != nil {
			panic(err)
		}
		cmd := exec.Command("swarmd", "--log-level=debug", "manager", fmt.Sprintf("--listen-addr=127.0.0.1:%d", port), "--state-dir="+tmp)
		go func() {
			if err := cmd.Start(); err != nil {
				panic(err)
			}
		}()
		time.Sleep(100 * time.Millisecond)
		t.managerProcesses = append(t.managerProcesses, cmd.Process)
		t.managerPorts = append(t.managerPorts, port)
	}
}

// StopManagers stop all the managers listed in ManagerProcesses.
func (t *Test) StopManagers() {
	killProcesses(t.managerProcesses)
}

// StartAgents starts `instances` agents.
func (t *Test) StartAgents(instances int) {
	for i := 0; i < instances; i++ {
		cmd := exec.Command("swarmd", "--log-level=debug", "agent", fmt.Sprintf("--manager=127.0.0.1:%d", t.managerPorts[rand.Intn(len(t.managerPorts))]), fmt.Sprintf("--id=agent-%d", i), fmt.Sprintf("--hostname=agent-%d", i))
		go func() {
			if err := cmd.Start(); err != nil {
				panic(err)
			}
		}()
		time.Sleep(500 * time.Millisecond)
		t.agentProcesses = append(t.agentProcesses, cmd.Process)
	}
}

// StopAgents stop all the managers listed in AgentProcesses.
func (t *Test) StopAgents() {
	killProcesses(t.agentProcesses)
}

// SwarmCtl invokes the swarmclt binary connected to the 1st manager started.
func (t *Test) SwarmCtl(args ...string) ([]string, int, error) {
	cmd := exec.Command("swarmctl", append([]string{fmt.Sprintf("--addr=127.0.0.1:%d", t.managerPorts[rand.Intn(len(t.managerPorts))])}, args...)...)
	output, err := cmd.Output()
	exitCode := 0
	if msg, ok := err.(*exec.ExitError); ok { // TODO(vieux): doesn't work on windows.
		exitCode = msg.Sys().(syscall.WaitStatus).ExitStatus()
	}
	return splitLines(output), exitCode, err

}

// Cleanup stops agents and managers.
func (t *Test) Cleanup() {
	t.StopAgents()
	t.StopManagers()
}

func splitLines(b []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(b))
	var lines []string
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
