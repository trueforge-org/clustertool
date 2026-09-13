package talosconfig

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

// Inventory contains cluster versions and command targeting metadata. Node settings stay in patches.
type Inventory struct {
	TalosVersion      string `yaml:"talosVersion"`
	KubernetesVersion string `yaml:"kubernetesVersion"`
	APIVersion        string `yaml:"apiVersion"`
	Kind              string `yaml:"kind"`
	BootstrapNode     string `yaml:"bootstrapNode"`
	Nodes             []Node `yaml:"nodes"`
}

type Node struct {
	Name    string `yaml:"name"`
	Role    string `yaml:"role"`
	Address string `yaml:"address"`
}

var nodeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func LoadInventory() (*Inventory, error) {
	data, err := os.ReadFile(filepath.Join(helper.TalosPath, "clustertool.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read required clustertool.yaml: %w", err)
	}
	// The embedded inventory is a template so init can copy one consistent
	// layout while the actual management addresses remain in clusterenv.yaml.
	// Render only when a template variable is present; hand-written inventories
	// remain ordinary YAML files.
	if strings.Contains(string(data), "${") {
		data, err = render(data, helper.TalEnv)
		if err != nil {
			return nil, fmt.Errorf("render clustertool.yaml: %w", err)
		}
	}
	var inv Inventory
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&inv); err != nil {
		return nil, fmt.Errorf("read clustertool.yaml: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("clustertool.yaml must contain exactly one YAML document")
	}
	if inv.APIVersion != "clustertool/v1" || inv.Kind != "ClusterConfig" || len(inv.Nodes) == 0 {
		return nil, fmt.Errorf("clustertool.yaml requires apiVersion: clustertool/v1, kind: ClusterConfig and at least one node")
	}
	for _, setting := range []struct{ name, value string }{
		{"talosVersion", inv.TalosVersion}, {"kubernetesVersion", inv.KubernetesVersion},
	} {
		if !strings.HasPrefix(setting.value, "v") {
			return nil, fmt.Errorf("clustertool.yaml requires %s in vMAJOR.MINOR.PATCH format", setting.name)
		}
		version, err := semver.StrictNewVersion(strings.TrimPrefix(setting.value, "v"))
		if err != nil || version.Metadata() != "" {
			return nil, fmt.Errorf("clustertool.yaml: invalid %s %q; use vMAJOR.MINOR.PATCH with an optional prerelease suffix", setting.name, setting.value)
		}
	}
	names, addresses := map[string]bool{}, map[string]bool{}
	bootstrap := false
	for index, n := range inv.Nodes {
		if !nodeName.MatchString(n.Name) || n.Name == "all" || names[n.Name] {
			return nil, fmt.Errorf("invalid or duplicate node name %q", n.Name)
		}
		if n.Role != "control-plane" && n.Role != "worker" {
			return nil, fmt.Errorf("node %s: invalid role %q", n.Name, n.Role)
		}
		ip, err := netip.ParseAddr(n.Address)
		if err != nil || ip.Zone() != "" || addresses[ip.String()] {
			return nil, fmt.Errorf("node %s: invalid or duplicate management IP %q", n.Name, n.Address)
		}
		names[n.Name], addresses[ip.String()] = true, true
		inv.Nodes[index].Address = ip.String()
		if n.Name == inv.BootstrapNode && n.Role == "control-plane" {
			bootstrap = true
		}
		dir := filepath.Join(helper.TalosPath, "patches", "nodes", n.Name)
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("node %s: missing or unsafe patch directory %s", n.Name, dir)
		}
	}
	if !bootstrap {
		return nil, fmt.Errorf("bootstrapNode must identify a control-plane node")
	}
	sort.SliceStable(inv.Nodes, func(i, j int) bool {
		if inv.Nodes[i].Role != inv.Nodes[j].Role {
			return inv.Nodes[i].Role == "control-plane"
		}
		return inv.Nodes[i].Name < inv.Nodes[j].Name
	})
	return &inv, nil
}

func (i *Inventory) Select(target string) ([]Node, error) {
	if ip, err := netip.ParseAddr(target); err == nil {
		target = ip.String()
	}
	if target == "" || target == "all" {
		return i.Nodes, nil
	}
	for _, n := range i.Nodes {
		if target == n.Name || target == n.Address {
			return []Node{n}, nil
		}
	}
	return nil, fmt.Errorf("node %q is not in clustertool.yaml", target)
}

func (i *Inventory) Bootstrap() Node {
	for _, n := range i.Nodes {
		if n.Name == i.BootstrapNode {
			return n
		}
	}
	panic("validated inventory has no bootstrap node")
}

func (i *Inventory) Endpoints() []string {
	var result []string
	for _, n := range i.Nodes {
		if n.Role == "control-plane" {
			result = append(result, n.Address)
		}
	}
	return result
}

func NodeConfigPath(n Node) string { return filepath.Join(helper.TalosGenerated, n.Name+".yaml") }
