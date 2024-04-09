#!/bin/bash

set -e

DIR_NAME=$(dirname "$0")

os_family=$(uname)

if [ "$os_family" == "Linux" ]; then
    # Install dependencies
    sudo apt update
    sudo apt install -y \
        aria2 \
        fio \
        socat \
        qemu-kvm \
        libvirt-daemon-system \
        curl \
        debootstrap \
        libguestfs-tools \
        libvirt-dev \
        python3-pip \
        nfs-kernel-server \
        rpcbind \
        ssh-askpass \
        xsltproc

    if [ "$(uname -m )" == "aarch64" ]; then
        sudo apt install -y qemu-efi-aarch64
    fi

    sudo systemctl start nfs-kernel-server.service
else
    brew install \
        aria2 \
        fio \
        socat \
        curl \
        libvirt \
        gnu-sed
fi

is_python_unsupported="$(python3 -c 'import sys; print(sys.version_info.major > 3 or sys.version_info.minor > 11)')"

if [ "$is_python_unsupported" == "True" ]; then
    read -p "WARNING: Your python version ($(python -V)) is not tested, some packages might not be available. Do you want to continue? [y/N] " -n 1 -r
    if [[ ! $REPLY =~ ^[Yy]$ ]] ; then
        echo "Cancelling setup, change your python version or run this in a virtualenv"
        exit 1
    fi
fi


pip3 install -r "${DIR_NAME}"/requirements.txt

if ! command -v pulumi &>/dev/null; then
    curl -fsSL https://get.pulumi.com | sh
fi


# Pulumi Setup
# shellcheck disable=SC1090
source ~/.bashrc
pulumi login --local
