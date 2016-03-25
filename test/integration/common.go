package integration

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
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

// TODO(vieux): this is ugly.
func getBinDir() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return strings.Split(wd, "/test/")[0]
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
		cmd := exec.Command(getBinDir()+"/bin/swarmd", "--log-level=debug", "manager", fmt.Sprintf("--listen-addr=127.0.0.1:%d", port), "--state-dir="+tmp)
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
		cmd := exec.Command(getBinDir()+"/bin/swarmd", "--log-level=debug", "agent", fmt.Sprintf("--manager=127.0.0.1:%d", t.managerPorts[0]), fmt.Sprintf("--id=agent-%d", i), fmt.Sprintf("--hostname=agent-%d", i))
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
func (t *Test) SwarmCtl(args ...string) (Output, int, error) {
	cmd := exec.Command(getBinDir()+"/bin/swarmctl", append([]string{fmt.Sprintf("--addr=127.0.0.1:%d", t.managerPorts[0])}, args...)...)
	output, err := cmd.Output()
	exitCode := 0
	if msg, ok := err.(*exec.ExitError); ok { // TODO(vieux): doesn't work on windows.
		exitCode = msg.Sys().(syscall.WaitStatus).ExitStatus()
	}
	return Output(output), exitCode, err

}

// Cleanup stops agents and managers.
func (t *Test) Cleanup() {
	t.StopAgents()
	t.StopManagers()
}

// Output represents the output of SwarmCtl.
type Output string

// Lines returns all the non-empty line in an Output.
func (o *Output) Lines() []string {
	var lines []string
	for _, line := range strings.Split(string(*o), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
