package integration

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
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

// ManagerProcesses represents the managers processes.
var ManagerProcesses []*os.Process

// ManagerPorts represents the managers binding ports.
var ManagerPorts []int

// StartManagers starts `instances` managers binding on a random port each.
// Fills ManagerProcesses and ManagerPorts.
func StartManagers(instances int) {
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
		ManagerProcesses = append(ManagerProcesses, cmd.Process)
		ManagerPorts = append(ManagerPorts, port)
	}
}

// StopManagers stop all the managers listed in ManagerProcesses.
func StopManagers() {
	killProcesses(ManagerProcesses)
}

// AgentProcesses represents the agents processes.
var AgentProcesses []*os.Process

// StartAgents starts `instances` agents.
func StartAgents(instances int) {
	for i := 0; i < instances; i++ {
		cmd := exec.Command(getBinDir()+"/bin/swarmd", "--log-level=debug", "agent", fmt.Sprintf("--manager=127.0.0.1:%d", ManagerPorts[0]), fmt.Sprintf("--id=agent-%d", i), fmt.Sprintf("--hostname=agent-%d", i))
		go func() {
			if err := cmd.Start(); err != nil {
				panic(err)
			}
		}()
		time.Sleep(500 * time.Millisecond)
		AgentProcesses = append(AgentProcesses, cmd.Process)
	}
}

// StopAgents stop all the managers listed in AgentProcesses.
func StopAgents() {
	killProcesses(AgentProcesses)
}

// SwarmCtl invokes the swarmclt binary connected to the 1st manager started.
func SwarmCtl(args ...string) (Output, error) {
	cmd := exec.Command(getBinDir()+"/bin/swarmctl", append([]string{fmt.Sprintf("--addr=127.0.0.1:%d", ManagerPorts[0])}, args...)...)
	output, err := cmd.Output()
	return Output(output), err

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
