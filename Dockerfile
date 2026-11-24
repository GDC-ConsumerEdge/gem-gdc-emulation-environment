# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Lightweight local toolbox image for setting up a GEM environment and
# provisioning GEM clusters without installing Terraform and Ansible locally.
ARG CLOUD_SDK_VERSION=584.0.0
FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:${CLOUD_SDK_VERSION}-slim

ARG TERRAFORM_VERSION=1.16.6
ARG TARGETARCH

ENV DEBIAN_FRONTEND=noninteractive \
    PIP_BREAK_SYSTEM_PACKAGES=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        jq \
        openssh-client \
        python3 \
        python3-pip \
        tmux \
        unzip \
    && rm -rf /var/lib/apt/lists/*

RUN ARCH="${TARGETARCH:-$(dpkg --print-architecture)}" \
    && curl -fsSL "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip" -o /tmp/tf.zip \
    && unzip -q /tmp/tf.zip -d /usr/local/bin \
    && rm /tmp/tf.zip \
    && terraform version

RUN pip3 install --no-cache-dir \
        "ansible-core>=2.21,<2.22" \
    && ansible --version \
    && ansible-galaxy collection install -p /usr/share/ansible/collections \
        ansible.posix \
        community.general \
        community.crypto

# Persist shell history across session
RUN printf '\nshopt -s histappend\n' >> /root/.bashrc

# Set USER/LOGNAME to `gem` so gcloud compute ssh and ansible/inventory.sh
# connect as `gem` rather than `root` on GCE instances.
ENV REPO_ROOT=/gem \
    HISTFILE=/gem/gem_toolbox_bash_history \
    HISTSIZE=10000 \
    HISTFILESIZE=20000 \
    PROMPT_COMMAND="history -a" \
    USER=gem \
    LOGNAME=gem \
    ANSIBLE_HOST_KEY_CHECKING=False \
    ANSIBLE_FORCE_COLOR=True

WORKDIR /gem

CMD ["/bin/bash"]
