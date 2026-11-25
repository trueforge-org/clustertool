package fluxhandler

import "testing"

func TestParseGitURL(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"https://github.com/owner/repo.git", "ssh://git@github.com/owner/repo.git"},
		{"github.com/owner/repo", "ssh://git@github.com/owner/repo"},
		{"git@gitlab.com:group/subgroup/repo.git", "ssh://git@gitlab.com/group/subgroup/repo.git"},
		{"gitlab.com:group/subgroup/repo.git", "ssh://git@gitlab.com/group/subgroup/repo.git"},
		{"ssh://git@gitlab.com/group/subgroup/repo.git", "ssh://git@gitlab.com/group/subgroup/repo.git"},
		{"ssh://git@192.0.2.10:2222/group/repo.git", "ssh://git@192.0.2.10:2222/group/repo.git"},
		{"ssh://custom@git.example.test:2222/group/repo.git", "ssh://custom@git.example.test:2222/group/repo.git"},
		{"custom@git.example.test:group/subgroup/repo.git", "ssh://custom@git.example.test/group/subgroup/repo.git"},
		{"ssh://[2001:db8::10]:2222/group/repo.git", "ssh://git@[2001:db8::10]:2222/group/repo.git"},
		{"git@[2001:db8::10]:group/repo.git", "ssh://git@[2001:db8::10]/group/repo.git"},
		{"  https://GITLAB.COM/group/repo.git  ", "ssh://git@gitlab.com/group/repo.git"},
		{"ssh://git@example.test:022/group/my%20repo.git", "ssh://git@example.test:22/group/my%20repo.git"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseGitURL(tc.input)
			if err != nil || got.String() != tc.want {
				t.Fatalf("ParseGitURL = %v, %v; want %s", got, err, tc.want)
			}
		})
	}
	for _, input := range []string{
		"", "github.com", "ssh:///group/repo.git", "ssh://git@github.com/",
		"file:///tmp/repo.git", "ftp://github.com/owner/repo.git",
		"ssh://git@example.test:0/repo.git", "ssh://git@example.test:65536/repo.git",
		"ssh://git@example.test:bad/repo.git", "ssh://git@example.test:/repo.git",
		"ssh://git@example.test/repo.git?token=value", "ssh://git@example.test/repo.git#main",
		"ssh://git:password@example.test/repo.git", "https://token@example.test/repo.git",
		"https://example.test:8443/repo.git", "ssh://2001:db8::10/repo.git",
	} {
		t.Run("invalid/"+input, func(t *testing.T) {
			if _, err := ParseGitURL(input); err == nil {
				t.Fatal("invalid repository URL accepted")
			}
		})
	}
}
