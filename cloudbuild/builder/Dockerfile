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

# Builder image for the GEM cluster-build Cloud Build pipeline.
#
# Bundles every tool an end-to-end cluster build needs into a single image so
# each pipeline step can use the same entrypoint. The Google Cloud SDK base
# image already includes `gcloud`, `gsutil`, and the IAP-tunneling component
# the dynamic Ansible inventory shells out to.
ARG CLOUD_SDK_VERSION=520.0.0
FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:${CLOUD_SDK_VERSION}-slim

ARG TERRAFORM_VERSION=1.15.3
ARG ANSIBLE_VERSION=10.7.0

ENV DEBIAN_FRONTEND=noninteractive \
    PIP_BREAK_SYSTEM_PACKAGES=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        gnupg \
        jq \
        openssh-client \
        python3 \
        python3-pip \
        unzip \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip" -o /tmp/tf.zip \
    && unzip -q /tmp/tf.zip -d /usr/local/bin \
    && rm /tmp/tf.zip \
    && terraform version

RUN pip3 install --no-cache-dir \
        "ansible==${ANSIBLE_VERSION}" \
        "ansible-core>=2.17,<2.18" \
    && ansible --version

# Collections required by the GEM playbooks.
RUN ansible-galaxy collection install \
        ansible.posix \
        community.general \
        community.crypto

ENV ANSIBLE_HOST_KEY_CHECKING=False \
    ANSIBLE_FORCE_COLOR=True

WORKDIR /workspace
