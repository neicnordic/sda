#!/bin/sh
set -e

# install tools if missing
for t in curl expect jq openssh-client postgresql-client xxd; do
    if [ ! "$(command -v $t)" ]; then
        if [ "$(id -u)" != 0 ]; then
            echo "$t is missing, unable to install it"
            exit 1
        fi

        apt-get -o DPkg::Lock::Timeout=60 update >/dev/null
        apt-get -o DPkg::Lock::Timeout=60 install -y "$t" >/dev/null
    fi
done

pip -q install s3cmd
