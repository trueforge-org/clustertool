package helper

import "testing"

func TestExtractNode(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "short flag", cmd: "talosctl health -n 192.168.1.10", want: "192.168.1.10"},
		{name: "long nodes flag", cmd: "talosctl health --nodes=10.0.0.2", want: "10.0.0.2"},
		{name: "prefers first matching node token", cmd: "talosctl health -n 10.0.0.1 --nodes=10.0.0.2", want: "10.0.0.1"},
		{name: "missing short-flag value", cmd: "talosctl health -n", want: ""},
		{name: "missing node", cmd: "talosctl health", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractNode(tt.cmd)
			if got != tt.want {
				t.Fatalf("ExtractNode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractSchematic(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "image present", cmd: "talosctl upgrade --image=factory.talos.dev/installer/abcd1234:v1.9.0", want: "abcd1234"},
		{name: "image missing", cmd: "talosctl upgrade --preserve", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSchematic(tt.cmd)
			if got != tt.want {
				t.Fatalf("ExtractSchematic() = %q, want %q", got, tt.want)
			}
		})
	}
}
