#!/bin/sh
## Note this step will only work if the scheduler is running in the cluster. If it's running as a service/binary/ex-K8s
## container the flags will need to be applied separately.

is_test=false
# [IMPORTANT] The script works for K8s clusters set-up via kubeadm with kube-scheduler.yaml configuration file 
# in /etc/kubernetes/manifest.
# For a different cluster set-up, users shoud provide the path to their kube-scheduler.yaml file
MANIFEST_FILE=/etc/kubernetes/manifests/kube-scheduler.yaml
scheduler_config_file_path=deploy/extender-configuration/scheduler-config.yaml
scheduler_config_destination=/etc/kubernetes
TAS_DEPLOYMENT_FILE=deploy/tas-deployment.yaml
SCHEDULER_VERSION=""
KUBE_SCHEDULER_API_VERSION=""
CONTROL_PLANE_NODE_AFINITY_LABEL="control-plane"
NODE_AFFINITY_LABEL_KEY=$CONTROL_PLANE_NODE_AFINITY_LABEL

help() {
  echo "Usage: $(basename "$0") [-m PATH_TO_MANIFEST_FILE] [-f PATH_TO_CONFIGURATION_FILE] [-d CONFIGURATION_DESTINATION_FOLDER] [-th]" 2>&1
  echo 'Configure the Kubernetes scheduler using one or more of the parameters below. If not entered, the script will be using default values. '
  echo 'Please ensure permissions for read & write to the files/directories mentioned below.'
  
  echo '   -m PATH_TO_MANIFEST_FILE               Specify the path to the Kubernetes manifest kube-scheduler.yaml file'
  echo '                                          If not provided, it will default to /etc/kubernetes/manifest/kube-scheduler.yaml'
  
  echo '   -f PATH_TO_CONFIGURATION_FILE          Specify the path to the kube scheduler configuration file. Required only from K8s v22 onwards.'
  echo '                                          If not provided, will default to scheduler-configuration (/deploy/extender-configuration)'

  echo '   -d CONFIGURATION_DESTINATION_FOLDER    Specify the destination folder for the kube scheduler config file. Required only from K8s v22 onwards.'
  echo '                                          If not provided, it will default to /etc/kubernetes/.'
  
  echo '   -t                                     Run script in test mode. It requires parameters to be provided to -m, -f, -d'
  echo '                                          In this state the script is intended to work with mock files and directories.'
  echo '                                          It will generate the expected configuration for the scheduler but it will'
  echo '                                          NOT configure the scheduler correctly.'

  echo '   -h                                     Show help menu.'
}

###### PARSE THE CUSTOMER INPUT AND ASSIGN THE VALUES
while getopts 'm:f:d:th' option; do
  case $option in
  h) # print help description
    help
    exit;;
  t) # start the script in test mode. We require parameters to be provided for -m, -f, -d in order to not accidentally
    # alter production configuration when in this mode
    if [ $# -lt 6 ]; then
      echo "Not enough input parameters were passed to the script. This requires -m, -f, -d to have values. Exiting..."
      exit
    fi
    is_test=true
    ;;
  m)
    user_manifest_file_path=$OPTARG
    if [ -n "$user_manifest_file_path" ];
      then MANIFEST_FILE=$user_manifest_file_path
    fi
    # MANIFEST_FILE CHECKS
    if [ ! -f "$MANIFEST_FILE" ]; then
      echo "Critical error. $MANIFEST_FILE doesn't exist. Please check the cluster configuration. Exiting..."
      exit 1
    fi
    if ! test -r "$MANIFEST_FILE" || ! test -w "$MANIFEST_FILE"; then
      echo "Critical error. The user used to run this script doesn't have read or write access to $MANIFEST_FILE. Exiting..."
      exit 1
    fi
    ;;
  f)
    # SCHEDULER CONFIG FILE CHECK
    user_scheduler_config_file_path=$OPTARG
    if [ -n "$user_scheduler_config_file_path" ]; then
      scheduler_config_file_path=$user_scheduler_config_file_path
    fi
    if ! test -r "$scheduler_config_file_path" || ! test -w "$scheduler_config_file_path"; then
      echo "Critical error. The user used to run this script doesn't have read or write access to $scheduler_config_file_path. Exiting..."
      exit 1
    fi
    ;;
  d)
    # SCHEDULER CONFIG DESTINATION FOLDER CHECK
    user_scheduler_config_destination=$OPTARG
    if [ -n "$user_scheduler_config_destination"  ]; then
      scheduler_config_destination=$user_scheduler_config_destination
    fi
    ;;
  \?) # invalid option
    echo 'Error: Invalid option. Exiting...'
    exit;;
  esac
done  

echo "Manifest file is located at: $MANIFEST_FILE"
echo "Scheduler config file is located at: $scheduler_config_file_path"
echo "Scheduler config destination will be: $scheduler_config_destination"

####### DETERMINE THE VERSION OF SCHEDULER USED IN THE K8S CLUSTER
get_scheduler_version() {
  scheduler_image=$( grep "image:" "$MANIFEST_FILE" | cut -d '.' -f 4 )

  if [ -z "$scheduler_image" ]; then
    echo "Unable to retrieve the scheduler image value from manifest file. We got: $scheduler_image. Exiting..."
    exit 1
  fi

  SCHEDULER_VERSION=$scheduler_image
}

get_kube_scheduler_api_version() {
  [ -z "${SCHEDULER_VERSION}" ] && echo "### Empty value for K8s scheduler value: $SCHEDULER_VERSION. Exit..." && exit 1

  scheduler_image_version_19=19
  scheduler_image_version_22=22
  scheduler_image_version_25=25
  scheduler_config_api_versions_v1beta1="v1beta1"
  scheduler_config_api_versions_v1beta2="v1beta2"
  scheduler_config_api_versions_v1="v1"

  currentKubeSchedulerApiVersion=""
  if [ "$SCHEDULER_VERSION" -lt $scheduler_image_version_19  ]; then
    echo "E2E tests will not execute for K8s version older than $scheduler_image_version_19. Exit..."
    exit 1
  elif [  "$SCHEDULER_VERSION" -ge $scheduler_image_version_19 ] && [ "$SCHEDULER_VERSION" -lt $scheduler_image_version_22 ]; then
    currentKubeSchedulerApiVersion=$scheduler_config_api_versions_v1beta1
  elif [  "$SCHEDULER_VERSION" -ge $scheduler_image_version_22 ] && [ "$SCHEDULER_VERSION" -lt $scheduler_image_version_25 ]; then
    currentKubeSchedulerApiVersion=$scheduler_config_api_versions_v1beta2
  else
    currentKubeSchedulerApiVersion=$scheduler_config_api_versions_v1
  fi

  [ -z "${currentKubeSchedulerApiVersion}" ] && echo "Invalid API version for Kube Scheduler Configuration, got: $currentKubeSchedulerApiVersion. Exit..." && exit 1

  KUBE_SCHEDULER_API_VERSION=$currentKubeSchedulerApiVersion
}

set_node_affinity_expression_label_key() {
  scheduler_image_version_24=24
  [ -z "${SCHEDULER_VERSION}" ] && echo "### Unable to get K8s scheduler value, got $SCHEDULER_VERSION. Exit..." && exit 1
  if [ "$SCHEDULER_VERSION" -lt $scheduler_image_version_24  ]; then
    NODE_AFFINITY_LABEL_KEY="master"
  fi

  [ -z "${NODE_AFFINITY_LABEL_KEY}" ] && echo "### Node affinity label key is empty: $NODE_AFFINITY_LABEL_KEY. Exit..." && exit 1
}

get_scheduler_version
echo "Version of the image used in the kube scheduler is: $SCHEDULER_VERSION"
get_kube_scheduler_api_version
echo "Version of the KubeScheduler API: $KUBE_SCHEDULER_API_VERSION"

# backup manifest file before changing it
echo "Backing up $MANIFEST_FILE..."
BACKUP_DIR=$(mktemp -p "$scheduler_config_destination" -t backup-manifest-XXX -d)
echo "Back-up dir is $BACKUP_DIR..."
# exit it we can't back-up
cp "$MANIFEST_FILE" "$BACKUP_DIR"
if [ ! "$(ls -A "$BACKUP_DIR")" ]; then
     echo "Take action $BACKUP_DIR is empty. Copy command failed, exiting...."
fi

echo "Back-up complete and available at $BACKUP_DIR."

####### CLEAN_UP MANIFEST FILE
# In case the previous run of this script was partially successful or unsuccessful, we'd like to start from a clean
# state, independent of any previous runs
# retrieve the scheduler config file name used in the manifest file, if any
old_scheduler_config_file_path=$(grep "   - --config" "$MANIFEST_FILE" | cut -d "=" -f 2)
old_scheduler_config_file=$(basename "$old_scheduler_config_file_path")
echo "current scheduler config file name from manifest is: $old_scheduler_config_file"

sed -i '/^    - --config/d' "$MANIFEST_FILE"
sed -i '/^    - --policy-configmap/d' "$MANIFEST_FILE"
sed -i '/^    - --policy-configmap-namespace/d' "$MANIFEST_FILE"
sed -i '/^  dnsPolicy: ClusterFirstWithHostNet/d' "$MANIFEST_FILE"
# retrieve the name of the scheduler config file
scheduler_config_file=$(basename "$scheduler_config_file_path")
# clean-up scheduler configuration
sed -i '/hostPath/d'  "$MANIFEST_FILE"
# if we're updating the scheduler config name we need to clean the manifest file of the old scheduler config file name
if [ -n "$old_scheduler_config_file" ] && [ "$old_scheduler_config_file" != "$scheduler_config_file" ]; then
  sed -i "/$old_scheduler_config_file/d" "$MANIFEST_FILE"
else
  sed -i "/$scheduler_config_file/d" "$MANIFEST_FILE"
fi
sed -i '/name: schedulerconfig/d' "$MANIFEST_FILE"
# clean-up certs configuration
sed -i '/certs/d' "$MANIFEST_FILE"
sed -i '/name: certdir/d' "$MANIFEST_FILE"
sed -i '/readOnly: true/d' "$MANIFEST_FILE"

####### SETTING UP NECESSARY CERTS
## Copy client cert/key pair into kube-scheduler
if [ ! -d "/etc/certs/" ]; then
  echo "Will proceed to create /etc/certs/..."
  mkdir /etc/certs/
fi

if [ ! -d "/etc/certs/" ]; then
  echo "Unable to successfully create the /etc/certs/ folder. Exiting..."
  exit 1
fi

cp /etc/kubernetes/pki/ca.key /etc/certs/client.key
cp /etc/kubernetes/pki/ca.crt /etc/certs/client.crt

####### INITIAL MANIFEST_FILE CHANGES

## The arguments are:
## 1) dnsPolicy as part of Pod spec allowing access to kubernetes services.
## 2) Set authorization certs
## 3) mount the necessary configuration files from the disk
sed -e "/spec/a\\
  dnsPolicy: ClusterFirstWithHostNet" "$MANIFEST_FILE" -i
sed -e "/      name: kubeconfig/a\\
      readOnly: true" "$MANIFEST_FILE" -i
sed -e "/    volumeMounts:/a\\
    - mountPath: /host/certs\n      name: certdir" "$MANIFEST_FILE" -i
sed -e "/  volumes:/a\\
  - hostPath:\n      path: /etc/certs\n    name: certdir\n  - hostPath:" "$MANIFEST_FILE" -i

####### VERSION SPECIFIC MANIFEST_FILE CHANGES. These are necessary, but change according to the version of Kubernetes

if [ -z "$KUBE_SCHEDULER_API_VERSION" ]; then
  echo "Invalid KubeSchedulerConfiguration API version: $KUBE_SCHEDULER_API_VERSION. Exiting..."
  exit 1
fi

if [ ! -f "$scheduler_config_file_path" ]; then
  echo "Critical error: $scheduler_config_file_path doesn't exist. Can't configure the scheduler for version $scheduler_image. Exiting..."
  exit 1
fi
# update the scheduler's version
sed -i "s/XVERSIONX/$KUBE_SCHEDULER_API_VERSION/g" "$scheduler_config_file_path"

if [ ! -d "$scheduler_config_destination" ]; then
  echo "Critical error. $scheduler_config_destination doesn't exist. Please check the cluster configuration. Exiting..."
  exit 1
fi

if ! $is_test ; then
  # copy the scheduler-config file to the expected folder
  echo "Will proceed to copy the scheduler configuration to its destination path: $scheduler_config_destination."
  cp "$scheduler_config_file_path" "$scheduler_config_destination"
fi

# generate the new path of the config file
scheduler_config_destination_path="$scheduler_config_destination/$scheduler_config_file"

## Add arguments to our kube-scheduler manifest. The arguments are:
## 1) Config file with the extender policy and other configuration
## 2) Mount the configuration file to make sure it's accessible by K8s
sed -e "/    - kube-scheduler/a\\
    - --config=$scheduler_config_destination_path" "$MANIFEST_FILE" -i
sed -e "/    volumeMounts:/a\\
    - mountPath: $scheduler_config_destination_path\n      name: schedulerconfig\n      readOnly: true" "$MANIFEST_FILE" -i
sed -e "/  volumes:/a\\
  - hostPath:\n      path: $scheduler_config_destination_path\n    name: schedulerconfig" "$MANIFEST_FILE" -i

### From v24 onwards the Kubernetes control plane labels have changes, node-role.kubernetes.io/master
### has been replaced with node-role.kubernetes.io/control-plane. This means that the nodeAffinity nodeSelector
### keys will be affected. The labels do not work together, so depending on the version of K8s used
### we need to use one or the other

set_node_affinity_expression_label_key
echo "Determined that the appropriate nodeSelector key would be: $NODE_AFFINITY_LABEL_KEY"
if [ "$CONTROL_PLANE_NODE_AFINITY_LABEL" != "$NODE_AFFINITY_LABEL_KEY"  ]; then
  sed "s/control-plane/$NODE_AFFINITY_LABEL_KEY/g" "$TAS_DEPLOYMENT_FILE" -i
fi

