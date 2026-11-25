package fluxhandler

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var scpGitURL = regexp.MustCompile(`^([^/@:\s]+@)?(\[[^\]]+\]|[^/:\s]+):(.+)$`)

// ParseGitURL normalizes repository URLs to the SSH URL used by Flux.
func ParseGitURL(input string) (*url.URL, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("Git repository URL cannot be empty")
	}
	if !strings.Contains(input, "://") {
		if parts := scpGitURL.FindStringSubmatch(input); parts != nil {
			input = parts[1] + parts[2] + "/" + parts[3]
		}
		input = "ssh://" + input
	}
	repository, err := url.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("invalid Git repository URL: %w", err)
	}
	switch repository.Scheme {
	case "ssh":
	case "http", "https":
		if repository.Port() != "" || repository.User != nil {
			return nil, fmt.Errorf("use an ssh:// repository URL to specify an SSH user or port")
		}
	default:
		return nil, fmt.Errorf("Git repository URL must use SSH or HTTP(S)")
	}
	host, port := strings.ToLower(repository.Hostname()), repository.Port()
	if host == "" || strings.Trim(repository.Path, "/") == "" {
		return nil, fmt.Errorf("Git repository URL must include a host and repository path")
	}
	if repository.RawQuery != "" || repository.ForceQuery || repository.Fragment != "" {
		return nil, fmt.Errorf("Git repository URL must not contain a query or fragment")
	}
	if strings.HasSuffix(repository.Host, ":") {
		return nil, fmt.Errorf("SSH port cannot be empty")
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(repository.Host, "[") {
		return nil, fmt.Errorf("IPv6 addresses in Git repository URLs must use brackets")
	}
	repository.Host = host
	if strings.Contains(host, ":") {
		repository.Host = "[" + host + "]"
	}
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, fmt.Errorf("SSH port must be between 1 and 65535")
		}
		repository.Host = net.JoinHostPort(host, strconv.Itoa(number))
	}
	if repository.User == nil {
		repository.User = url.User("git")
	} else if _, password := repository.User.Password(); password || repository.User.Username() == "" {
		return nil, fmt.Errorf("Git SSH repository URL must contain a username without a password")
	}
	repository.Scheme = "ssh"
	return repository, nil
}
