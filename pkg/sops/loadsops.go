package sops

import (
	"fmt"
	"io/ioutil"

	"github.com/getsops/sops/v3/config"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type SopsConfig struct {
	Stores        config.StoresConfig `yaml:"stores"`
	CreationRules []struct {
		PathRegex        string `yaml:"path_regex"`
		EncryptedRegex   string `yaml:"encrypted_regex,omitempty"`
		Age              string `yaml:"age"`
		MACOnlyEncrypted bool   `yaml:"mac_only_encrypted"`
	} `yaml:"creation_rules"`
}

func LoadSopsConfig() (SopsConfig, error) {
	log.Trace().Msg("Starting LoadSopsConfig function")

	// Read .sops.yaml file
	data, err := ioutil.ReadFile(".sops.yaml")
	if err != nil {
		return SopsConfig{}, fmt.Errorf("read .sops.yaml: %w", err)
	}
	log.Debug().Msg("Successfully read .sops.yaml file")

	// Unmarshal YAML data into struct
	config := SopsConfig{Stores: *config.NewStoresConfig()}
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return SopsConfig{}, fmt.Errorf("parse .sops.yaml: %w", err)
	}
	log.Debug().Msg("Successfully unmarshaled YAML data into struct")

	// Log the loaded struct
	log.Debug().Msg("Loaded SopsConfig successfully")
	log.Debug().Interface("loaded SOPS Config", config)

	return config, nil
}
