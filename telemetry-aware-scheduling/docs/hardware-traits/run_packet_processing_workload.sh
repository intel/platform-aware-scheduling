#!/bin/sh
PCI1="$(kubectl exec -it cloud-network-function -- bash -c "printenv PCIDEVICE_INTEL_COM_INTEL_SRIOV_DPDK | cut -d , -f1 | tr -d [:space:]")"
PCI2="$(kubectl exec -it cloud-network-function -- bash -c "printenv PCIDEVICE_INTEL_COM_INTEL_SRIOV_DPDK | cut -d , -f2 | tr -d [:space:]")"
export PCI1
export PCI2

kubectl exec -it cloud-network-function -- /usr/src/dpdk-stable-19.11.1/install/bin/testpmd -w "$PCI2" -w "$PCI1" -- --tx-first --eth-peer=0,ca:fe:c0:ff:ee:01 --eth-peer=1,ca:fe:c0:ff:ee:01 --forward-mode=mac --stats-period=1
