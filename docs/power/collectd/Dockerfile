FROM centos:centos7 AS builder
RUN yum -y install epel-release-7-11 && yum clean all
RUN yum groupinstall "Development Tools" -y && yum clean all
RUN yum -y install python3-devel-3.6.8 protobuf-c-compiler-1.0.2 protobuf-c-1.0.2 libmicrohttpd-devel-0.9.33 diffutils-3.3 file-5.11 git-1.8.3.1 which-2.20 bison-3.0.4 automake-1.13.4 autoconf-2.69 pkg-config  libtool-2.4.2 flex-2.5.37 && yum clean all
RUN yum -y install protobuf-c-devel-1.0.2 protobuf-devel-2.5.0 && yum clean all
RUN git clone https://github.com/collectd/collectd
WORKDIR /collectd
RUN ./build.sh

RUN ./configure --enable-write_prometheus --enable-python
RUN make && make install && rm -rf ./*
RUN mkdir /opt/collectd/etc/python-scripts
ENV p="wal"
RUN curl https://raw.githubusercontent.com/intel/CommsPowerManagement/master/telemetry/pkgpower.py -o /opt/collectd/etc/python-scripts/pkgpower.py
ENV PATH="/opt/collectd/sbin:${PATH}"