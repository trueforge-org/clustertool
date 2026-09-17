package sops

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
)

// EncryptAllFiles encrypts all unencrypted files as specified in the .sops.yaml configuration.
func EncryptAllFiles() error {
	log.Trace().Msg("Starting EncryptAllFiles function")

	files, err := ExecuteCheck(false) // Get the list of files and their encryption status
	if err != nil {
		return fmt.Errorf("check files for encryption: %w", err)
	}

	log.Debug().Int("fileCount", len(files)).Msg("Found files to process for encryption")

	for _, file := range files {
		if err := processFileEncryption(file); err != nil {
			return err
		}
	}

	log.Info().Msg("All Files encrypted successfully")
	return nil
}

// Helper function that handles the encryption process for an individual file
func processFileEncryption(file EncrFileData) error {
	// Skip files that are already encrypted
	if file.Encrypted {
		log.Info().Msgf("File %s is already encrypted, skipping.\n", file.Path)
		return nil
	}

	// Check if the file is partially staged
	fullyStaged, err := fthelper.IsFileFullyStaged(file.Path)
	if err != nil {
		return fmt.Errorf("error checking staged status of file %s: %w", file.Path, err)
	}

	// If the file is not fully staged, stage it
	if !fullyStaged {
		log.Info().Msgf("File %s is partially staged, staging fully...\n", file.Path)
		err := fthelper.StageFile(file.Path)
		if err != nil {
			return fmt.Errorf("error staging file %s: %w", file.Path, err)
		}
		log.Info().Msgf("File %s fully staged.\n", file.Path)
	}

	// Encrypt the file
	err = encryptFile(file.Path)
	if err != nil {
		return fmt.Errorf("error encrypting file %s: %w", file.Path, err)
	}

	log.Debug().Msgf("File %s encrypted successfully.\n", file.Path)
	return nil
}

// encryptFile encrypts the content of the specified file and replaces the file with the encrypted data.
func encryptFile(filePath string) error {
	log.Trace().Msgf("Starting encryption for file: %s", filePath)

	// Read the content of the file
	content, err := os.ReadFile(filePath)
	log.Debug().Msgf("Encrypting '%s'... \n", filePath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	// Load the settings used for this file.
	sopsConfig, err := LoadSopsConfig()
	if err != nil {
		return err
	}

	// Encrypt the content
	encryptedData, err := EncryptWithAgeKey(content, filePath, sopsConfig)
	if err != nil {
		return fmt.Errorf("error encrypting data: %w", err)
	}

	// Write the encrypted data back to the file
	if err := os.WriteFile(filePath, encryptedData, 0644); err != nil {
		return fmt.Errorf("error writing encrypted data to file: %w", err)
	}

	log.Debug().Msgf("Successfully encrypted file: %s", filePath)
	return nil
}

// encryptionSettings merges encrypted_regex from matching rules and uses
// mac_only_encrypted from the first match.
func encryptionSettings(filePath string, config SopsConfig) (string, bool) {
	var expressions []string
	macOnlyEncrypted := false
	filePath = filepath.ToSlash(filePath)
	for _, rule := range config.CreationRules {
		pattern, err := regexp.Compile(rule.PathRegex)
		if err != nil {
			log.Warn().Err(err).Msg("Error compiling regex")
			continue
		}
		if pattern.MatchString(filePath) {
			if len(expressions) == 0 {
				macOnlyEncrypted = rule.MACOnlyEncrypted
			}
			expressions = append(expressions, rule.EncryptedRegex)
		}
	}
	return strings.Join(expressions, "|"), macOnlyEncrypted
}
