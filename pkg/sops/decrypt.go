package sops

import (
	"fmt"
	"os"

	"github.com/getsops/sops/v3/decrypt"
	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/initfiles"
)

func DecryptFiles() error {
	log.Trace().Msg("Starting DecryptFiles function")

	// Get a list of encrypted files
	files, err := ExecuteCheck(false)
	if err != nil {
		return fmt.Errorf("check files for decryption: %w", err)
	}

	// Flag to track if any files were marked as encrypted
	encryptedFound := false

	// Decrypt each encrypted file
	for _, file := range files {
		if file.Encrypted {
			encryptedFound = true
			log.Debug().Msgf("Decrypting file: %s", file.Path)

			data, err := os.ReadFile(file.Path)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", file.Path, err)
			}

			// Verify integrity before replacing the encrypted file.
			decrypted, err := decryptData(data, GetFormat(file.Path))
			if err != nil {
				return fmt.Errorf("error decrypting file %s: %w", file.Path, err)
			}

			// Write decrypted data back to file
			if err := os.WriteFile(file.Path, decrypted, 0644); err != nil {
				return fmt.Errorf("error writing decrypted data to file %s: %w", file.Path, err)
			}
			log.Debug().Msgf("Successfully decrypted file: %s", file.Path)
		}
	}

	// Check if any encrypted files were found
	if !encryptedFound {
		log.Info().Msg("Nothing to decrypt")
	}

	if err := initfiles.LoadTalEnv(true); err != nil {
		return err

	}
	log.Info().Msg("All files decrypted successfully")
	return nil
}

func decryptData(data []byte, format string) ([]byte, error) {
	log.Trace().Msg("Starting decryption of data")

	os.Setenv("SOPS_AGE_KEY_FILE", "age.agekey")
	// Decrypt data
	decrypted, err := decrypt.Data(data, format)
	if err != nil {
		return nil, err
	}

	log.Debug().Msg("Data decrypted successfully")
	return decrypted, nil
}
