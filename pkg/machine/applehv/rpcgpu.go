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

// startRpcGpuBridge starts a socat process that bridges the vfkit vsock Unix
// socket to the rpc-server TCP port. The socat process is detached from the
// podman process so it survives after podman exits (same pattern as rpc-server).
//
// vfkit vsock listen=true means: vfkit connects to this Unix socket when a
// guest process connects to vsock port 1026. Therefore socat must LISTEN on
// the socket path before vfkit starts, and when a connection arrives, forward
// it to rpc-server TCP.
//
// socat args: UNIX-LISTEN:<socketPath>,fork TCP:localhost:50052
func startRpcGpuBridge(socketPath string) error {
	// Check if socat is already bridging this socket
	if isSocatBridgeRunning(socketPath) {
		logrus.Debug("socat bridge is already running, skipping start")
		return nil
	}

	socatPath, err := exec.LookPath("socat")
	if err != nil {
		return fmt.Errorf("socat not found in PATH; install with 'brew install socat'")
	}

	// Remove stale socket file if it exists
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		logrus.Warnf("failed to remove stale socket %s: %v", socketPath, err)
	}

	rpcAddr := fmt.Sprintf("TCP:localhost:%s,nodelay", rpcServerPort)
	unixAddr := fmt.Sprintf("UNIX-LISTEN:%s,fork", socketPath)

	logrus.Infof("Starting RPC GPU bridge: %s → %s (via socat)", socketPath, rpcAddr)

	cmd := exec.Command(socatPath, unixAddr, rpcAddr)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start socat bridge: %w", err)
	}

	logrus.Infof("socat bridge started with PID %d", cmd.Process.Pid)

	// Release the process so it runs independently of podman
	if err := cmd.Process.Release(); err != nil {
		logrus.Warnf("failed to release socat process: %v", err)
	}

	return nil
}

// isSocatBridgeRunning checks if a socat process bridging the given socket is already running
func isSocatBridgeRunning(socketPath string) bool {
	cmd := exec.Command("pgrep", "-f", fmt.Sprintf("socat.*%s", socketPath))
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
