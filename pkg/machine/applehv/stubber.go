//go:build darwin

package applehv

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"

	gvproxy "github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/podman/v6/pkg/machine"
	"github.com/containers/podman/v6/pkg/machine/apple"
	"github.com/containers/podman/v6/pkg/machine/apple/vfkit"
	"github.com/containers/podman/v6/pkg/machine/define"
	"github.com/containers/podman/v6/pkg/machine/ignition"
	"github.com/containers/podman/v6/pkg/machine/vmconfigs"
	"github.com/containers/podman/v6/utils"
	vfConfig "github.com/crc-org/vfkit/pkg/config"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
)

// applehcMACAddress is a pre-defined mac address that vfkit recognizes
// and is required for network flow

var vfkitCommand = "vfkit"

type AppleHVStubber struct {
	vmconfigs.AppleHVConfig
}

func (a *AppleHVStubber) UserModeNetworkEnabled(_ *vmconfigs.MachineConfig) bool {
	return true
}

func (a *AppleHVStubber) UseProviderNetworkSetup() bool {
	return false
}

func (a *AppleHVStubber) RequireExclusiveActive() bool {
	return true
}

func (a *AppleHVStubber) CreateVM(opts define.CreateVMOpts, mc *vmconfigs.MachineConfig, ignBuilder *ignition.IgnitionBuilder) error {
	mc.AppleHypervisor = new(vmconfigs.AppleHVConfig)
	mc.AppleHypervisor.Vfkit = vfkit.Helper{}
	bl := vfConfig.NewEFIBootloader(fmt.Sprintf("%s/efi-bl-%s", opts.Dirs.DataDir.GetPath(), opts.Name), true)
	mc.AppleHypervisor.Vfkit.VirtualMachine = vfConfig.NewVirtualMachine(uint(mc.Resources.CPUs), uint64(mc.Resources.Memory), bl)

	randPort, err := utils.GetRandomPort()
	if err != nil {
		return err
	}
	mc.AppleHypervisor.Vfkit.Endpoint = localhostURI + ":" + strconv.Itoa(randPort)

	virtiofsMounts := make([]machine.VirtIoFs, 0, len(mc.Mounts))
	for _, mnt := range mc.Mounts {
		virtiofsMounts = append(virtiofsMounts, machine.MountToVirtIOFs(mnt))
	}

	// Populate the ignition file with virtiofs stuff
	virtIOIgnitionMounts, err := apple.GenerateSystemDFilesForVirtiofsMounts(virtiofsMounts)
	if err != nil {
		return err
	}
	ignBuilder.WithUnit(virtIOIgnitionMounts...)

	cfg, err := config.Default()
	if err != nil {
		return err
	}
	rosetta := cfg.Machine.Rosetta
	if runtime.GOARCH != "arm64" {
		rosetta = false
	}
	mc.AppleHypervisor.Vfkit.Rosetta = rosetta
	mc.AppleHypervisor.Vfkit.RpcGpu = mc.RpcGpu

	return apple.ResizeDisk(mc, mc.Resources.DiskSize)
}

func (a *AppleHVStubber) Exists(_ string) (bool, error) {
	// not applicable for applehv
	return false, nil
}

func (a *AppleHVStubber) MountType() vmconfigs.VolumeMountType {
	return vmconfigs.VirtIOFS
}

func (a *AppleHVStubber) MountVolumesToVM(_ *vmconfigs.MachineConfig, _ bool) error {
	// virtiofs: nothing to do here
	return nil
}

func (a *AppleHVStubber) RemoveAndCleanMachines(_ *define.MachineDirs) error {
	return nil
}

func (a *AppleHVStubber) SetProviderAttrs(mc *vmconfigs.MachineConfig, opts define.SetOptions) error {
	state, err := a.State(mc, false)
	if err != nil {
		return err
	}
	return apple.SetProviderAttrs(mc, opts, state)
}

func (a *AppleHVStubber) StartNetworking(mc *vmconfigs.MachineConfig, cmd *gvproxy.GvproxyCommand) error {
	return apple.StartGenericNetworking(mc, cmd)
}

func (a *AppleHVStubber) StartVM(mc *vmconfigs.MachineConfig) (func() error, func() error, error) {
	bl := mc.AppleHypervisor.Vfkit.VirtualMachine.Bootloader
	if bl == nil {
		return nil, nil, fmt.Errorf("unable to determine boot loader for this machine")
	}

	cfg, err := config.Default()
	if err != nil {
		return nil, nil, err
	}
	rosetta := cfg.Machine.Rosetta
	rosettaNew := rosetta
	if runtime.GOARCH == "arm64" {
		rosettaMC := mc.AppleHypervisor.Vfkit.Rosetta
		if rosettaMC != rosettaNew {
			mc.AppleHypervisor.Vfkit.Rosetta = rosettaNew
		}
	}

	// Start rpc-server on host if RPC GPU is enabled
	if mc.AppleHypervisor.Vfkit.RpcGpu {
		if err := startRpcServer(); err != nil {
			logrus.Warnf("failed to start rpc-server: %v", err)
		}

		// Start socat bridge BEFORE vfkit launches.
		// vfkit vsock listen=true means vfkit connects to this Unix socket
		// when a guest process connects to vsock port 1026.
		// socat must be listening on the socket path before vfkit starts.
		rtDir, rtErr := mc.RuntimeDir()
		if rtErr == nil {
			socketPath := filepath.Join(rtDir.GetPath(), fmt.Sprintf("%s-rpc-gpu", mc.Name))
			if bridgeErr := startRpcGpuBridge(socketPath); bridgeErr != nil {
				logrus.Warnf("failed to start RPC GPU bridge: %v", bridgeErr)
			}
		} else {
			logrus.Warnf("failed to get runtime dir for RPC GPU bridge: %v", rtErr)
		}
	}

	return apple.StartGenericAppleVM(mc, vfkitCommand, bl, mc.AppleHypervisor.Vfkit.Endpoint)
}

func (a *AppleHVStubber) StopHostNetworking(_ *vmconfigs.MachineConfig, _ define.VMType) error {
	return nil
}

func (a *AppleHVStubber) UpdateSSHPort(_ *vmconfigs.MachineConfig, _ int) error {
	// managed by gvproxy on this backend, so nothing to do
	return nil
}

func (a *AppleHVStubber) VMType() define.VMType {
	return define.AppleHvVirt
}

func (a *AppleHVStubber) PrepareIgnition(_ *vmconfigs.MachineConfig, _ *ignition.IgnitionBuilder) (*ignition.ReadyUnitOpts, error) {
	return nil, nil
}

func (a *AppleHVStubber) PostStartNetworking(_ *vmconfigs.MachineConfig, _ bool) error {
	return nil
}

func (a *AppleHVStubber) GetRosetta(mc *vmconfigs.MachineConfig) (bool, error) {
	rosetta := mc.AppleHypervisor.Vfkit.Rosetta
	return rosetta, nil
}
