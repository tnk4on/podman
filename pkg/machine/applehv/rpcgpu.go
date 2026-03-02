//go:build darwin

package applehv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

const (
	rpcServerBinaryName = "rpc-server"
	rpcServerPort       = "50052"
	rpcServerDevice     = "MTL0"
)

// startRpcServer starts the GGML RPC server on the host for Metal GPU access.
// It searches for the rpc-server binary in PATH and common locations.
// If an rpc-server is already running, it is a no-op.
func startRpcServer() error {
	// Check if rpc-server is already running
	if isRpcServerRunning() {
		logrus.Debug("rpc-server is already running, skipping start")
		return nil
	}

	// Find rpc-server binary
	binaryPath, err := findRpcServerBinary()
	if err != nil {
		return err
	}

	logrus.Infof("Starting rpc-server at %s (port %s, device %s)", binaryPath, rpcServerPort, rpcServerDevice)

	cmd := exec.Command(binaryPath, "-H", "0.0.0.0", "-p", rpcServerPort, "-d", rpcServerDevice)
	cmd.Stdout = nil
	cmd.Stderr = nil
	// Detach from parent process group so it survives if podman exits
	cmd.SysProcAttr = nil

	if err := cmd.Start(); err != nil {
		return err
	}

	logrus.Infof("rpc-server started with PID %d", cmd.Process.Pid)

	// Release the process so it runs independently
	if err := cmd.Process.Release(); err != nil {
		logrus.Warnf("failed to release rpc-server process: %v", err)
	}

	return nil
}

// isRpcServerRunning checks if an rpc-server process is already running
func isRpcServerRunning() bool {
	cmd := exec.Command("pgrep", "-f", rpcServerBinaryName)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// findRpcServerBinary searches for the rpc-server binary in PATH and common locations
func findRpcServerBinary() (string, error) {
	// First, check PATH
	if path, err := exec.LookPath(rpcServerBinaryName); err == nil {
		return path, nil
	}

	// Check common locations
	homeDir, _ := os.UserHomeDir()
	searchPaths := []string{
		filepath.Join(homeDir, "bin", rpcServerBinaryName),
		filepath.Join(homeDir, ".local", "bin", rpcServerBinaryName),
		"/usr/local/bin/" + rpcServerBinaryName,
	}

	for _, p := range searchPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("rpc-server binary not found in PATH or common locations; install it or add to PATH")
}
