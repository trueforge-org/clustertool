package helper

import (
	"os"
	"path/filepath"
)

var (
	HelmCache       = filepath.Join(CacheDir, "tgz_cache")
	UserCacheDir, _ = os.UserCacheDir()
	TalEnv          = make(map[string]string)
	ClusterName     = "main"
	KubeCache       = filepath.Join(CacheDir, "kubernetes")
	BaseCache       = filepath.Join(CacheDir, "base")
	RootCache       = filepath.Join(CacheDir, "root")
	DocsCache       = filepath.Join(CacheDir, "docs")
	CacheDir        = filepath.Join(UserCacheDir, "clustertool")
	ClusterPath     = filepath.Join("./clusters", ClusterName)
	ClusterEnvFile  = filepath.Join(ClusterPath, "/clusterenv.yaml")
	TalosPath       = filepath.Join(ClusterPath, "/talos")
	KubernetesPath  = filepath.Join(ClusterPath, "/kubernetes")
	TalosGenerated  = filepath.Join(TalosPath, "/generated")
	TalosConfigFile = filepath.Join(TalosGenerated, "talosconfig")

	IndexCache = "./index_cache"
	GpgDir     = ".cr-gpg" // Adjust the path based on your project structure
	Logo       = `

  _______              ______                   
 |__   __|            |  ____|                  
    | |_ __ _   _  ___| |__ ___  _ __ __ _  ___ 
    | | '__| | | |/ _ \  __/ _ \| '__/ _` + "`" + ` |/ _ \
    | | |  | |_| |  __/ | | (_) | | | (_| |  __/
    |_|_|   \__,_|\___|_|  \___/|_|  \__, |\___|
                                      __/ |     
                 _______         __  |___/  ______          __
                / ___/ /_ _____ / /____ ___/_  __/__  ___  / /
               / /__/ / // (_-</ __/ -_) __// / / _ \/ _ \/ /
               \___/_/\_,_/___/\__/\__/_/  /_/  \___/\___/_/                  
                  
`
)
