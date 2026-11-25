package fluxhandler

import (
	"bufio"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Official host keys: https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints
const githubKnownHosts = `github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl
github.com ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBEmKSENjQEezOmxkZMy7opKgwFB9nkt5YRrYMjNuG5N87uRgg6CLrbo5wAdT/y6v0mKV0U2w0WZ2YB/++Tpockg=`

// Official host keys: https://docs.gitlab.com/user/gitlab_com/#ssh-known_hosts-entries
const gitlabKnownHosts = `gitlab.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAfuCHKVTjquxvt6CM6tdG4SLp1Btn/nOeHHE5UOzRdf
gitlab.com ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBFSMqzJeV9rUzU4kWitGjeR4PWSa29SPqJ1fVkhtj3Hw9xjLVXVYrU9QlYWrOLXBpQ6KWjbjTDTdDkoohFzgbEY=`

func getKnownHostsEntry(gitURL string) (string, error) {
	// Preserve secret generation when the optional Flux repository is absent.
	if strings.TrimSpace(gitURL) == "" {
		return githubKnownHosts, nil
	}
	repository, err := ParseGitURL(gitURL)
	if err != nil {
		return "", err
	}
	host, port := repository.Hostname(), repository.Port()
	if port == "" {
		port = "22"
	}
	address := net.JoinHostPort(host, port)
	var entries string
	switch host {
	case "github.com":
		entries = githubKnownHosts
	case "gitlab.com":
		entries = gitlabKnownHosts
	}
	if entries != "" {
		return strings.ReplaceAll(entries, host+" ", knownhosts.Normalize(address)+" "), nil
	}

	log.Info().Msgf("Detected custom Git SSH server: %s", address)
	key, err := scanSSHHostKey(address, repository.User.Username(), 10*time.Second)
	if err != nil {
		return "", err
	}
	if err := confirmSSHHost(key, os.Stdin, os.Stdout); err != nil {
		return "", err
	}
	return knownhosts.Line([]string{address}, key), nil
}

func scanSSHHostKey(address, user string, timeout time.Duration) (ssh.PublicKey, error) {
	connection, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to Git SSH server %s: %w", address, err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("set SSH handshake deadline: %w", err)
	}
	var key ssh.PublicKey
	keyReceived := errors.New("SSH host key received")
	config := &ssh.ClientConfig{
		User: user,
		HostKeyAlgorithms: []string{
			ssh.KeyAlgoED25519,
			ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256,
		},
		HostKeyCallback: func(_ string, _ net.Addr, hostKey ssh.PublicKey) error {
			if publicKey, ok := hostKey.(ssh.CryptoPublicKey); ok {
				if rsaKey, ok := publicKey.CryptoPublicKey().(*rsa.PublicKey); ok && rsaKey.N.BitLen() < 2048 {
					return fmt.Errorf("RSA host key is %d bits; at least 2048 bits are required", rsaKey.N.BitLen())
				}
			}
			key = hostKey
			// The SSH library has verified the server's signature. Stop before authentication.
			return keyReceived
		},
	}
	_, _, _, err = ssh.NewClientConn(connection, address, config)
	if !errors.Is(err, keyReceived) {
		var negotiationError *ssh.AlgorithmNegotiationError
		if errors.As(err, &negotiationError) && negotiationError.What == "host key" {
			return nil, fmt.Errorf("Git SSH server %s offers no allowed SSH host key algorithm; require Ed25519, ECDSA, or RSA with SHA-2: %w", address, err)
		}
		return nil, fmt.Errorf("retrieve SSH host key from %s: %w", address, err)
	}
	return key, nil
}

func confirmSSHHost(key ssh.PublicKey, input io.Reader, output io.Writer) error {
	fmt.Fprintf(output, "SSH host key type: %s\nSSH host fingerprint: %s\n\n", key.Type(), ssh.FingerprintSHA256(key))
	fmt.Fprintln(output, "Verify this fingerprint with the Git server administrator.")
	reader := bufio.NewReader(input)
	for {
		fmt.Fprint(output, "Do you trust this SSH host? [y/N]: ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("SSH host trust was not confirmed: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return nil
		case "", "n", "no":
			return fmt.Errorf("SSH host key was not trusted; init cancelled")
		default:
			fmt.Fprintln(output, "Invalid input. Please enter yes/no or y/n.")
		}
	}
}
