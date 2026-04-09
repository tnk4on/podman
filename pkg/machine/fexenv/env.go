package fexenv

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/machine"
	"github.com/containers/podman/v5/pkg/machine/vmconfigs"
	"github.com/sirupsen/logrus"
)

// fexCodeCacheScriptTemplate is a bash script that toggles the FEX code cache
// by modifying FEX_ENABLECODECACHINGWIP in the base containers.conf.
// When enabled, sets FEX_ENABLECODECACHINGWIP=1 (default state from fex-activation.sh).
// When disabled, sets FEX_ENABLECODECACHINGWIP=0 to override Config.json.
// Also cleans up legacy drop-in files from the previous approach.
//
// WORKAROUND: Uses sed on base containers.conf instead of containers.conf.d
// drop-in because stock podman (brew/PKG) lacks this SSH injection.
// TODO: Migrate to drop-in approach when upstream adds native support.
const fexCodeCacheScriptTemplate = `#!/bin/bash

FEX_ENABLED=%s

# Clean up legacy drop-in files (from previous {append=true} approach)
for DROPIN in /var/home/core/.config/containers/containers.conf.d/fex-code-cache.conf \
              /root/.config/containers/containers.conf.d/fex-code-cache.conf; do
    rm -f "$DROPIN" 2>/dev/null || true
done

for CONF_FILE in /var/home/core/.config/containers/containers.conf \
                 /root/.config/containers/containers.conf; do
    if [ ! -f "$CONF_FILE" ]; then
        continue
    fi
    if [ "$FEX_ENABLED" = "true" ]; then
        sed -i 's/"FEX_ENABLECODECACHINGWIP=0"/"FEX_ENABLECODECACHINGWIP=1"/' "$CONF_FILE"
    else
        sed -i 's/"FEX_ENABLECODECACHINGWIP=1"/"FEX_ENABLECODECACHINGWIP=0"/' "$CONF_FILE"
    fi
done
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
