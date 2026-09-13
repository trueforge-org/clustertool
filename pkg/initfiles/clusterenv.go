package initfiles

import (
	"bufio"
	"fmt"
	"net"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
	"gopkg.in/yaml.v3"
)

func LoadTalEnv(noFail bool) error {
	file := helper.ClusterPath + "/clusterenv.yaml"
	if _, err := os.Stat(file); err != nil {
		if noFail && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cluster environment %s: %w", file, err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read cluster environment %s: %w", file, err)
	}
	var document map[string]interface{}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse cluster environment %s: %w", file, err)
	}
	sourceEnv := make(map[string]string)
	if err := fthelper.LoadEnvFromFile(file, sourceEnv); err != nil {
		return fmt.Errorf("load cluster environment %s: %w", file, err)
	}
	if _, err := checkQuotedNumbersInFile(); err != nil {
		return err
	}
	helper.TalEnv = sourceEnv
	clusterName()
	if err := clusterEnvtoEnv(); err != nil {
		return err
	}
	log.Info().Msg("ClusterEnv loaded successfully")
	return nil
}

// Function to check if all numbers after ':' in a file are unquoted integers or floats
func checkQuotedNumbersInFile() (bool, error) {
	filePath := helper.ClusterPath + "/clusterenv.yaml"
	// Regular expression to find patterns like ': number' where number can be an int or float
	re := regexp.MustCompile(`:\s*(.+)`) // Matches anything after ': '

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("read clusterenv.yaml: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip lines that start with a number
		trimmedLine := strings.TrimSpace(line)
		if len(trimmedLine) > 0 && unicode.IsDigit(rune(trimmedLine[0])) {
			continue
		}

		// Find matches for entries in each line
		matches := re.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue // Skip lines without a colon and value
		}

		// Get the value after the colon
		value := strings.TrimSpace(matches[1])

		// Check if the value is a valid number (int or float with a single dot)
		isValidNumber := regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`).MatchString(value)

		// If it's a valid number, log an error
		if isValidNumber {
			return false, fmt.Errorf("unquoted number in %s; quote numeric environment values", filePath)
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read clusterenv.yaml: %w", err)
	}

	return true, nil
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
