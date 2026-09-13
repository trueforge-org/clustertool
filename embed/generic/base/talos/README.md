# ClusterTool: native Talos configuration

This major version uses native Talos 1.14 configuration documents instead of
Talhelper. Use the new layout below; configuration from older ClusterTool versions
is not converted automatically. Run the commands from your cluster repository root.

## Before you start

**We strongly recommend a completely fresh cluster bootstrap for this major version.**

Before starting, verify that your VolSync backups and CloudNativePG (CNPG)
backups are available and accessible, and that you have the credentials required
to restore them.

You can reuse your existing Git repository for the new cluster, but **do not run
`clustertool init` in that repository**. Instead:

1. Run `clustertool init` in a temporary directory, complete `clusterenv.yaml`,
   then run `clustertool init` again and `clustertool genconfig`.
2. Copy the new Talos configuration structure, including
   `clusters/main/talos/secrets.sops.yaml`, into your existing repository.
3. Remove the old Talhelper configuration, generated files and old cluster
   secrets. Keep the newly generated secrets.
4. Adjust the node configuration and the Longhorn/OpenEBS values described below
   before bootstrapping.

This procedure creates a new cluster; it does not migrate an existing running
cluster or its storage data.

## Required Longhorn and OpenEBS values

**Your Longhorn and OpenEBS HelmRelease values must match the new Talos storage
paths.** The supplied templates already contain these settings. If you keep your
own HelmReleases, update the following values before using the new storage layout.

Longhorn — `clusters/main/kubernetes/system/longhorn/app/helm-release.yaml`:

```yaml
spec:
  values:
    defaultSettings:
      defaultDataPath: /var/mnt/longhorn
```

OpenEBS — `clusters/main/kubernetes/system/openebs/app/helm-release.yaml`:

```yaml
spec:
  values:
    localpv-provisioner:
      localpv:
        basePath: /var/mnt/openebs
```

Merge these values into your existing HelmReleases; keep their other settings.
Custom OpenEBS StorageClasses with an explicit `BasePath` must also use the intended
path. Flux/Helm must apply the changed values; `clustertool genconfig` alone does not
update an installed chart.

The Talos patch `patches/all/31-storage.yaml` creates directory-type user volumes
at these paths on the existing EPHEMERAL filesystem. They do not create separate
partitions or reserve disk space. Talos makes these directories available to
kubelet, replacing the old `machine.kubelet.extraMounts` configuration.

**Changing these defaults does not move existing data.** Existing Longhorn disks
and OpenEBS persistent volumes retain their recorded paths. If they contain data,
plan that storage change separately before replacing the old mounts.

## Initialize and edit

Run `clustertool init`, fill in `clusters/main/clusterenv.yaml`, then run
`clustertool init` again to complete setup. Existing Talos secrets are retained.

```text
clusters/main/talos/
├── README.md
├── clustertool.yaml
├── secrets.sops.yaml
├── patches/
│   ├── all/
│   ├── control-plane/
│   ├── worker/
│   └── nodes/
│       └── control-1/
├── examples/
└── generated/
```

- `clustertool.yaml`: Talos/Kubernetes versions and the list of nodes.
- `patches/all/`: shared Talos settings.
- `patches/control-plane/` and `patches/worker/`: settings for that role.
- `patches/nodes/<name>/`: a node's hostname, network, disk and installer image.
- `examples/`: inactive examples; copy the required YAML into a patch directory.
- `generated/`: generated node configurations and client configuration; do not edit.
- `secrets.sops.yaml`: the cluster's keys, certificates and tokens; retain this file
  when regenerating configuration or adding nodes.

Patches are applied in that order: shared, role, then node. Within each directory,
YAML files are read in filename order. Later patches can override earlier settings.
Encrypt secrets with `clustertool encrypt` before committing them to Git.

## Configure nodes and versions

`clustertool.yaml` is required even for a single node:

```yaml
apiVersion: clustertool/v1
kind: ClusterConfig
# renovate: datasource=github-releases depName=siderolabs/talos
talosVersion: v1.14.0
# renovate: datasource=docker depName=ghcr.io/siderolabs/kubelet
kubernetesVersion: v1.37.0
bootstrapNode: control-1
nodes:
  - name: control-1
    role: control-plane
    address: ${CONTROL1IP}

  # - name: control-2
  #   role: control-plane
  #   address: ${CONTROL2IP}
  # - name: worker-1
  #   role: worker
  #   address: ${WORKER1IP}
```

`apiVersion` identifies the ClusterTool file format. `bootstrapNode` selects the
control plane used for initial bootstrap, not a permanent leader. A node's `name`
matches its patch directory; its actual hostname is set in `HostnameConfig`.

Set bare IP addresses in `clusterenv.yaml`, for example `CONTROL1IP: 192.168.20.210`.
Specify the subnet prefix in the node's network patch, for example
`address: ${CONTROL1IP}/24`. Choose the correct interface and gateway.
ClusterTool substitutes variables as entered; it does not create `_IP`, `_CIDR`
or `_NETMASK` variants. The VIP must differ from the node addresses.

To add a node, add its entry and address variable, then create
`patches/nodes/<name>/` using the starter node files. Adjust its hostname, IP,
interface, disk selector and installer image. Keep the VIP configuration on
control planes; remove `Layer2VIPConfig` from worker node patches.
Use three control planes if you need control-plane availability during a reboot.

`kubernetesVersion` sets the generated Kubernetes component images unless a custom
patch overrides an image. Installer patches use `${TALOS_VERSION}` from
`talosVersion`. This does not change the Talosctl bundled with ClusterTool:
a different major/minor version produces a warning and asks whether to continue.
Changing version values alone does not perform an upgrade.

## Generate and apply

```sh
clustertool genconfig
clustertool talos apply
# Apply to one configured node:
clustertool talos apply control-2
```

`genconfig` generates the node configurations. `talos apply` regenerates them first,
then applies all nodes sequentially, control planes before workers. A node name
or IP selects one node. Do not run generation and apply concurrently.

For a new cluster, apply asks before bootstrapping. Initial setup installs the
bootstrap node, Cilium and the CSR approver, then joins the remaining nodes and
continues with the other charts and Flux.

If setup is interrupted, run apply again. When a matching
`.bootstrap-in-progress.json` exists, ClusterTool asks whether to resume.
The file is removed after successful setup. Keep the original cluster secrets.

## Change an image or upgrade

Each node can use its own Image Factory schematic in `00-install.yaml`:

```yaml
installer:
  image: factory.talos.dev/metal-installer/<schematic-id>:${TALOS_VERSION}
```

Register your customization with Image Factory and replace the schematic ID.
Keep the extensions and kernel arguments that node needs.
`genconfig` and `apply` update configuration but do not replace the running image.
To activate a new schematic or Talos version on one node:

```sh
clustertool talos upgrade control-2 --talos-only
```

The node reboots and ClusterTool waits for recovery. Without `--talos-only`, the
command also performs one cluster-wide Kubernetes upgrade, even when you select
only one node. Use `clustertool talos upgrade all` to upgrade all configured nodes.

A two-control-plane cluster cannot keep etcd quorum during a reboot: rolling
upgrades are refused and apply is restricted to changes that do not reboot.
A single-control-plane upgrade causes temporary control-plane downtime.
Tuppr independently uses each node's running schematic; it does not read
`clustertool.yaml`.

## Optional configuration

For registry authentication, copy `examples/44-registry-auth.yaml` into the
appropriate patch directory and configure `DOCKERHUB_USER` and
`DOCKERHUB_PASSWORD`. Setting the variables alone does not enable authentication.

For NVIDIA, copy `examples/50-nvidia.yaml` to the relevant node patch directories
and use an image schematic with the required NVIDIA extensions.
