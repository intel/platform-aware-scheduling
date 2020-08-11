export PCI1=$(kubectl exec -it dpdkcollectdl3fwd -- bash -c "printenv PCIDEVICE_INTEL_COM_INTEL_SRIOV_DPDK | cut -d , -f1 | tr -d [:space:]")
export PCI2=$(kubectl exec -it dpdkcollectdl3fwd -- bash -c "printenv PCIDEVICE_INTEL_COM_INTEL_SRIOV_DPDK | cut -d , -f2 | tr -d [:space:]")
kubectl exec -it dpdkcollectdl3fwd -- /usr/src/dpdk-20.05/examples/l3fwd-power/build/l3fwd-power --log-level lib.eal:debug -w $PCI2 -w $PCI1 --telemetry -l 2,3,46,47 -- -p 0x3 --telemetry  --config="(0,0,3),(1,0,46)"

