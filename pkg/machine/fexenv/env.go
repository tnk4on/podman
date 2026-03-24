package fexenv

import (
	"fmt"
	"strings"

	"go.podman.io/podman/v6/pkg/machine"
	"go.podman.io/podman/v6/pkg/machine/vmconfigs"
	"github.com/sirupsen/logrus"
)

// fexCodeCacheScriptTemplate is a bash script that updates containers.conf 
// in the VM to enable/disable FEX code caching.
// It modifies the env line under [containers] for both rootless and rootful configs.
// When enabled, FEX_ENABLECODECACHINGWIP=1 is added; when disabled, it is removed.
const fexCodeCacheScriptTemplate = `#!/bin/bash

FEX_ENABLED=%s

update_containers_conf() {
    local CONF="$1"
    if [ ! -f "$CONF" ]; then
        return
    fi

    # Remove any existing FEX_ENABLECODECACHINGWIP env line
    sed -i '/FEX_ENABLECODECACHINGWIP/d' "$CONF"

    if [ "$FEX_ENABLED" = "true" ]; then
        # Add env line under [containers] section
        # If env line exists, append to it; otherwise add a new one
        if grep -q '^env\s*=' "$CONF"; then
            # env line exists — replace it adding FEX env var
            sed -i 's|^env\s*=.*|&\nenv = ["FEX_ENABLECODECACHINGWIP=1"]|' "$CONF"
            # Deduplicate: keep only last env line
            tac "$CONF" | awk '/^env\s*=/ && !seen {seen=1; print; next} /^env\s*=/ {next} {print}' | tac > "${CONF}.tmp" && mv "${CONF}.tmp" "$CONF"
        else
            # No env line — add after [containers]
            sed -i '/^\[containers\]/a env = ["FEX_ENABLECODECACHINGWIP=1"]' "$CONF"
        fi
    fi
}

# Update both rootless (core) and rootful (root) containers.conf
update_containers_conf /var/home/core/.config/containers/containers.conf
update_containers_conf /root/.config/containers/containers.conf
`

// ApplyFEXCodeCache injects or removes FEX code cache env var in VM containers.conf.
// This follows the same pattern as proxyenv.ApplyProxies().
func ApplyFEXCodeCache(mc *vmconfigs.MachineConfig) error {
	enabled := "true"
	if !mc.FEXCodeCache {
		enabled = "false"
	}

	script := fmt.Sprintf(fexCodeCacheScriptTemplate, enabled)
	logrus.Debugf("FEX code cache: setting to %s", enabled)

	return machine.LocalhostSSHWithStdin("root", mc.SSH.IdentityPath, mc.Name, mc.SSH.Port,
		[]string{"/usr/bin/bash"},
		strings.NewReader(script))
}
