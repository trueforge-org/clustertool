package gencmd

import (
	"fmt"
	"path"

	"github.com/rs/zerolog/log"

	"github.com/trueforge-org/clustertool/pkg/fluxhandler"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/initfiles"
	"github.com/trueforge-org/clustertool/pkg/sops"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
)

func GenConfig(args []string) error {
	if initfiles.CheckRunAgainFileExists() {
		return fmt.Errorf("run init again after completing clusterenv.yaml")
	}
	if err := sops.DecryptFiles(); err != nil {
		return err
	}
	if err := confirmTalosVersion(fthelper.GetYesOrNo); err != nil {
		return err
	}
	if err := initfiles.GenTalEnvConfigMap(); err != nil {
		return err
	}
	if err := initfiles.CheckEnvVariables(); err != nil {
		return err
	}
	if err := talosconfig.Generate(); err != nil {
		return err
	}
	if err := initfiles.UpdateGitRepo(); err != nil {
		return err
	}

	if err := fluxhandler.ProcessDirectory(path.Join(helper.ClusterPath, "kubernetes")); err != nil {
		return err
	}
	if err := fluxhandler.ProcessDirectory(path.Join(helper.ClusterPath, "kubernetes")); err != nil {
		return err
	} else {
		log.Info().Msgf("Kustomizations processed successfully.")
	}
	helper.CreateEncrPreCommitHook()
	log.Info().Msg("GenConfig: Completed Successfully!")
	return nil
}
