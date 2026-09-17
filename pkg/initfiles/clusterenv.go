package initfiles

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"gopkg.in/yaml.v3"
)

// LoadTalEnv reads the shared cluster settings from the Secret's stringData.
func LoadTalEnv(noFail bool) error {
	file := helper.ClusterSettingsFile
	data, err := os.ReadFile(file)
	if err != nil {
		if noFail && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cluster settings %s: %w", file, err)
	}
	var document struct {
		StringData map[string]interface{} `yaml:"stringData"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse cluster settings %s: %w", file, err)
	}
	if document.StringData == nil {
		return fmt.Errorf("cluster settings %s requires stringData", file)
	}
	sourceEnv := make(map[string]string, len(document.StringData))
	for key, value := range document.StringData {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("cluster setting %s must be a string; quote numbers and booleans", key)
		}
		sourceEnv[key] = text
	}
	helper.TalEnv = sourceEnv
	clusterName()
	if err := clusterEnvtoEnv(); err != nil {
		return err
	}
	log.Info().Msg("Cluster settings loaded successfully")
	return nil
}

func clusterName() {
	helper.TalEnv["CLUSTERNAME"] = helper.ClusterName
}

func clusterEnvtoEnv() error {
	for key, value := range helper.TalEnv {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("export environment variable %s: %w", key, err)
		}
	}
	return nil
}

func CheckEnvVariables() error {
	if err := LoadTalEnv(false); err != nil {
		return err
	}
	for _, key := range []string{"VIP", "HEADLAMP_IP", "GATEWAY", "METALLB_RANGE", "PODNET", "SVCNET", "DOMAIN_0", "DOMAIN_0_EMAIL", "DOMAIN_0_CLOUDFLARE_TOKEN"} {
		if helper.TalEnv[key] == "" {
			return fmt.Errorf("%s cannot be empty", key)
		}
	}
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return err
	}
	addresses := map[string]netip.Addr{}
	for _, key := range []string{"VIP", "GATEWAY", "HEADLAMP_IP"} {
		ip, err := netip.ParseAddr(helper.TalEnv[key])
		if err != nil || ip.Zone() != "" {
			return fmt.Errorf("%s must be an IP address without a subnet prefix", key)
		}
		addresses[key] = ip.Unmap()
	}
	parts := strings.Split(helper.TalEnv["METALLB_RANGE"], "-")
	if len(parts) != 2 {
		return fmt.Errorf("METALLB_RANGE must be a start-end IP range")
	}
	start, e1 := netip.ParseAddr(strings.TrimSpace(parts[0]))
	end, e2 := netip.ParseAddr(strings.TrimSpace(parts[1]))
	start, end = start.Unmap(), end.Unmap()
	if e1 != nil || e2 != nil || start.Zone() != "" || end.Zone() != "" || start.BitLen() != end.BitLen() || start.Compare(end) > 0 {
		return fmt.Errorf("METALLB_RANGE must contain valid, ordered IP addresses of the same family")
	}
	inRange := func(ip netip.Addr) bool {
		return ip.BitLen() == start.BitLen() && ip.Compare(start) >= 0 && ip.Compare(end) <= 0
	}
	for _, key := range []string{"VIP", "GATEWAY"} {
		if inRange(addresses[key]) {
			return fmt.Errorf("%s cannot be in METALLB_RANGE", key)
		}
	}
	if !inRange(addresses["HEADLAMP_IP"]) {
		return fmt.Errorf("HEADLAMP_IP must be in METALLB_RANGE")
	}
	for _, node := range inv.Nodes {
		ip := netip.MustParseAddr(node.Address).Unmap()
		if ip == addresses["VIP"] || ip == addresses["GATEWAY"] {
			return fmt.Errorf("node %s management address overlaps VIP or gateway", node.Name)
		}
		if inRange(ip) {
			return fmt.Errorf("node %s management address conflicts with METALLB_RANGE", node.Name)
		}
	}
	for _, key := range []string{"PODNET", "SVCNET"} {
		_, prefix, err := net.ParseCIDR(helper.TalEnv[key])
		if err != nil {
			return fmt.Errorf("invalid %s: %w", key, err)
		}
		for _, name := range []string{"VIP", "GATEWAY"} {
			if prefix.Contains(net.ParseIP(addresses[name].String())) {
				return fmt.Errorf("%s cannot be in %s", name, key)
			}
		}
		for _, node := range inv.Nodes {
			if prefix.Contains(net.ParseIP(node.Address)) {
				return fmt.Errorf("node %s management address conflicts with %s", node.Name, key)
			}
		}
		if prefix.Contains(net.ParseIP(start.String())) || prefix.Contains(net.ParseIP(end.String())) || inRange(netip.MustParseAddr(prefix.IP.String()).Unmap()) {
			return fmt.Errorf("METALLB_RANGE overlaps %s", key)
		}
	}
	return nil
}
