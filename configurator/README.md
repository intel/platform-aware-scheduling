# PAS Configurator

Platform Aware Scheduling (PAS) Configurator is a tool for setting up Telemetry Aware Scheduling (TAS) or GPU Aware Scheduling (GAS) to a
Kubernetes scheduler installation. It does not work with all possible installations, but if your kubernetes installation
uses a static pod yaml at `/etc/kubernetes/manifests` for the scheduler, then you have a good chance of being able to
use the configurator tool. Other types of installations have not been tested.

- [PAS Configurator](#pas-configurator)
  - [Introduction](#introduction)
  - [Building](#building)
  - [Running](#running)
    - [Command line flags](#command-line-flags)
    - [Assumptions for running the binary](#assumptions-for-running-the-binary)
    - [Using the standalone binary](#using-the-standalone-binary)
    - [Using the container version](#using-the-container-version)
  - [Communication and contribution](#communication-and-contribution)

## Introduction

The configurator is a go executable. After building it and providing it with the correct command line flags,
it will update the kubernetes scheduler deployment YAML, install the chosen scheduler configuration file and
copy the chosen certificate. It will _not_ create the certificate secret, so that part of TAS and GAS installation needs to be done before hand.

Note: deploying the actual TAS and GAS Pods to the cluster are out of scope,
configurator only configures the scheduler.

The changes which the configurator does, are:
  * `/etc/certs/`
    * selected certificate (usually `/etc/kubernetes/pki/ca.crt` and the corresponding key file) is copied as `client.crt` and `client.key` to the specified folder. This is the certificate used for communicating between the scheduler, and the extender(s). Folder is created when it does not exist.
  * scheduler config
    * selected config file API-version is modified to match the scheduler version and then copied to `/etc/kubernetes/`
  * `/etc/kubernetes/manifests/kube-scheduler.yaml`
    * `dnsPolicy` of the scheduler is set to `ClusterFirstWithHostNet`
    * scheduler command line is set to use the previously copied scheduler config file
    * cert volume is added to the scheduler Pod spec and volume mount is added to the container

You can do a `-dry-run` first, which shows the scheduler config changes as a <span style="color:red">red</span>-<span style="color:green">green</span> diff output instead of actually changing the config. The dry-run will not copy the cert.

By default the configurator creates backups of the scheduler deployment yaml and config file before writing to them. The backup location is displayed upon running, and includes a random folder name part. If you do not like backups, use `-b /dev/null`.

## Building

There are no pre-built binaries or containers for the configurator.

You can choose to either build a container, or you can choose to build just the executable.

If you just want to build the executable and run it as a standalone binary, you have two alternatives:
  * host machine build
    * you need to have `go 1.19` and `make` in the host
    * run `make build` and you will get the binary `configurator` to folder `bin/` in the configurator top folder.
  * containerized build
    * you need to have `docker` in the host
    * run `make containerized-build` and you will get the binary `configurator` to folder `bin/` in the configurator top folder.

If you want to have the binary in a container:
  * you need to have `docker` in the host
  * `make image` creates the container with the `configurator` binary inside it (in the `bin` folder) and the relevant yaml files with
    the source repository folder structure.

Or you can do `make build` or `make image` in the Platform Aware Scheduling top folder, which propagates to the subfolders,
including `configurator`.

If you prefer podman over docker, substituting podman for docker may also work, but is not tested actively.
You can try building after running: `find -name Makefile | xargs sed -i 's/docker /podman /'`.

## Running

### Command line flags
The below flags can be passed to the binary at run time.

name |type | description| default|
-----|------|-----|-----|
|b| string | Folder where backup- prefixed backup folders are created. | /etc/kubernetes/ |
|cert| string| Path to the certificate file. | /etc/kubernetes/pki/ca.crt|
|cert-dir| string | Destination dir for certificates. | /etc/certs/|
|d| string | Destination folder for the kube scheduler config file. Required only from K8s v22 onwards. | /etc/kubernetes/ |
|dry-run| bool | Trial run with diff output for scheduler settings, but no real changes.| false|
|f| string | Path to the kube scheduler configuration file. Required only from K8s v22 onwards.| deploy/extender-configuration/scheduler-config.yaml|
|gas-depl| string | Path to the GAS scheduler extender deployment file. Required only for K8s v23 or older. |
|key| string | Path to the certificate key file. | /etc/kubernetes/pki/ca.key|
|m|string| Path to the Kubernetes manifest kube-scheduler.yaml file | /etc/kubernetes/manifests/kube-scheduler.yaml|
|tas-depl| string | Path to the TAS scheduler extender deployment file. Required only for K8s v23 or older. | deploy/tas-deployment.yaml|

### Assumptions for running the binary

Some of the default values assume that:
* Configurator is run on the control plane node, where the modified yaml-files are located
* Source tree is cloned to the control plane node, when you run the configurator
This does not apply when running Configurator from the container as it includes the relevant yaml-files with the correct folder structure.
You need to adjust the command line flags, if these assumptions do not apply in your case.

### Using the standalone binary

User needs to have read access to the source files and write access to the destination files. You will probably need to
run the Configurator as root or sudo when using it to modify files under `/etc/`. Configurator will output errors when it
does not have the required permissions.

It is probably a good idea to start by using the `-dry-run` which gives you a <span style="color:red">red</span>-<span style="color:green">green</span> diff of what the configurator would change. Try it:

```bash
git clone https://github.com/intel/platform-aware-scheduling
cd platform-aware-scheduling
make build
cd telemetry-aware-scheduling
sudo ../configurator/bin/configurator -dry-run
```

By default the configurator will create backups of the files that it changes (scheduler deployment file and config file). If you do not want that,
you can use `-b /dev/null` command line flag.

Before actually using the configurator, you should create the certificate as a secret in the cluster.

The configurator defaults to using `/etc/kubernetes/pki/ca.key` and `/etc/kubernetes/pki/ca.crt`, so the easiest thing to do
is to use those files when creating the secret. If you instead want to use your own cert with the configurator, you need to configure
the scheduler secret to match. Below are examples of creating the secret for the default values.

Assuming you are fine with the default folders, and the default configuration, TAS configuring to a recent k8s version would go like this:

```bash
kubectl create secret tls extender-secret --cert /etc/kubernetes/pki/ca.crt --key /etc/kubernetes/pki/ca.key -n telemetry-aware-scheduling # you may need to be sudo
git clone https://github.com/intel/platform-aware-scheduling
cd platform-aware-scheduling
make build
cd telemetry-aware-scheduling
sudo ../configurator/bin/configurator
```

You would then proceed by deploying TAS:

```bash
kubectl apply -f deploy/
```

Alternatively, if you want to configure for GAS:

```bash
kubectl create secret tls extender-secret --cert /etc/kubernetes/pki/ca.crt --key /etc/kubernetes/pki/ca.key # you may need to be sudo
git clone https://github.com/intel/platform-aware-scheduling
cd platform-aware-scheduling
make build
cd gpu-aware-scheduling
sudo ../configurator/bin/configurator
```

You would then proceed by deploying GAS:

```bash
kubectl apply -f deploy/
```

Alternatively, if you want to configure for both GAS and TAS at the same time:

```bash
kubectl create secret tls extender-secret --cert /etc/kubernetes/pki/ca.crt --key /etc/kubernetes/pki/ca.key -n telemetry-aware-scheduling # you may need to be sudo
kubectl create secret tls extender-secret --cert /etc/kubernetes/pki/ca.crt --key /etc/kubernetes/pki/ca.key # you may need to be sudo
git clone https://github.com/intel/platform-aware-scheduling
cd platform-aware-scheduling
make build
sudo configurator/bin/configurator -f gpu-aware-scheduling/deploy/extender-configuration/scheduler-config-tas+gas.yaml
```

(deploy both)

### Using the container version

First, [build](#building) the container.

The container will have the relevant yaml files with the source repository folder structure, and the configurator binary in `/bin/` folder.

You can take the container to the control-plane node and run it manually with docker, or you can push the image to a registry
of your own, and then run it with docker in the control-plane node.

Example of GAS configuring dry-run with Docker:

```bash
docker run --mount type=bind,src=/etc/kubernetes,dst=/etc/kubernetes pas-configurator -f gpu-aware-scheduling/deploy/extender-configuration/scheduler-config.yaml -cert-dir /etc/kubernetes/schedulercerts --dry-run
```
* if you are happy with the results
  * run without `--dry-run`
  * create extender-secret
  * deploy GAS ([and intel GPU-plugin and NFD](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/gpu_plugin/README.md#install-to-nodes-with-intel-gpus-with-fractional-resources))

## Communication and contribution

Report a bug by [filing a new issue](https://github.com/intel/platform-aware-scheduling/issues).

Contribute by [opening a pull request](https://github.com/intel/platform-aware-scheduling/pulls).

Learn [about pull requests](https://help.github.com/articles/using-pull-requests/).

**Reporting a Potential Security Vulnerability:** If you have discovered potential security vulnerability in the configurator, please send an e-mail to secure@intel.com. For issues related to Intel Products, please visit [Intel Security Center](https://security-center.intel.com).

It is important to include the following details:

- The projects and versions affected
- Detailed description of the vulnerability
- Information on known exploits

Vulnerability information is extremely sensitive. Please encrypt all security vulnerability reports using our [PGP key](https://www.intel.com/content/www/us/en/security-center/pgp-public-key.html).

A member of the Intel Product Security Team will review your e-mail and contact you to collaborate on resolving the issue. For more information on how Intel works to resolve security issues, see: [vulnerability handling guidelines](https://www.intel.com/content/www/us/en/security-center/vulnerability-handling-guidelines.html).

