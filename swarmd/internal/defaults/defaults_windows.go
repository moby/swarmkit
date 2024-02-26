//go:build windows
// +build windows

package defaults

// ControlAPISocket is the default path where clients can contact the swarmd control API.
var ControlAPISocket = "//./pipe/swarmd"

// EngineAddr is Docker default named pipe on Windows
var EngineAddr = "npipe:////./pipe/docker_engine"

// StateDir is the default path to the swarmd state directory
var StateDir = `C:\ProgramData\swarmd`
