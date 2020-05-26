FROM centos:centos7 AS builder
RUN yum -y install epel-release
RUN yum groupinstall "Development Tools" -y
RUN yum -y install python3-devel protobuf-c-compiler protobuf-c libmicrohttpd-devel diffutils file git which bison automake autoconf pkg-config libtool flex
RUN yum -y install git protobuf-c-devel protobuf-devel
RUN git clone https://github.com/collectd/collectd  
WORKDIR collectd
RUN ./build.sh 

RUN ./configure --enable-write_prometheus --enable-python
RUN make && make install && rm -rf *
RUN mkdir /opt/collectd/etc/python-scripts
ENV p="wal" 
RUN curl https://raw.githubusercontent.com/intel/CommsPowerManagement/master/telemetry/pkgpower.py -o /opt/collectd/etc/python-scripts/pkgpower.py
ENV PATH="/opt/collectd/sbin:${PATH}"

