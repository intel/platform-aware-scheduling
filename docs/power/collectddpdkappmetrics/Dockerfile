FROM centos:8

ENV DPDK_VERSION=20.05

ENV DPDK_TARGET=x86_64-native-linuxapp-gcc
ENV DPDK_DIR=/usr/src/dpdk-${DPDK_VERSION}
ENV PATH="/opt/collectd/sbin:${PATH}"

ENV RTE_SDK=${DPDK_DIR}

RUN dnf groupinstall -y 'Development Tools'
RUN dnf install -y wget numactl-devel
RUN yum -y install 'dnf-command(config-manager)' && yum -y config-manager --set-enabled PowerTools
RUN yum -y install python3-devel protobuf-c-compiler protobuf-c libmicrohttpd-devel diffutils file git which bison automake autoconf pkg-config libtool flex
RUN yum -y install git protobuf-c-devel protobuf-devel jansson-devel
RUN cd /usr/src/ && \
  wget http://fast.dpdk.org/rel/dpdk-${DPDK_VERSION}.tar.xz && \
  tar xf dpdk-${DPDK_VERSION}.tar.xz && \
  rm -f dpdk-${DPDK_VERSION}.tar.xz && \
  cd ${DPDK_DIR} && \
  sed -i '$ a\CONFIG_RTE_LIBRTE_TELEMETRY=y' config/common_linux && \ 
  sed -i s/CONFIG_RTE_EAL_IGB_UIO=y/CONFIG_RTE_EAL_IGB_UIO=n/ config/common_linux && \
  sed -i s/CONFIG_RTE_LIBRTE_KNI=y/CONFIG_RTE_LIBRTE_KNI=n/ config/common_linux && \
  sed -i s/CONFIG_RTE_KNI_KMOD=y/CONFIG_RTE_KNI_KMOD=n/ config/common_linux && \
  make install T=${DPDK_TARGET} DESTDIR=install
RUN cd ${DPDK_DIR}/examples/l3fwd-power/ && \
    make

RUN git clone https://github.com/collectd/collectd  
WORKDIR collectd
RUN ./build.sh 
RUN ./configure --enable-write_prometheus --enable-python --enable-dpdk_telemetry
RUN make && make install && rm -rf *
