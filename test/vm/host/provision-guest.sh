#!/usr/bin/env bash

set -euo pipefail

go_version="${GO_VERSION:-1.25.12}"
kubectl_version="${KUBECTL_VERSION:-1.34.8}"
kind_version="${KIND_VERSION:-0.30.0}"
helm_version="${HELM_VERSION:-4.2.3}"

sudo apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y \
  ca-certificates curl git gnupg jq make python3 rsync shellcheck

sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  | sudo gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

# Supplied by the supported Ubuntu guest image.
# shellcheck disable=SC1091
. /etc/os-release
architecture="$(dpkg --print-architecture)"
echo "deb [arch=$architecture signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null

sudo apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y \
  containerd.io docker-buildx-plugin docker-ce docker-ce-cli
sudo usermod -aG docker "$USER"
sudo systemctl enable --now docker

install_verified() {
  local url="$1"
  local checksum_url="$2"
  local destination="$3"
  local expected
  local temporary
  temporary="$(mktemp)"
  curl -fsSL "$url" -o "$temporary"
  expected="$(curl -fsSL "$checksum_url" | awk '{print $1}')"
  echo "$expected  $temporary" | sha256sum --check
  sudo install -m 0755 "$temporary" "$destination"
  rm -f "$temporary"
}

install_verified \
  "https://dl.k8s.io/release/v${kubectl_version}/bin/linux/amd64/kubectl" \
  "https://dl.k8s.io/release/v${kubectl_version}/bin/linux/amd64/kubectl.sha256" \
  /usr/local/bin/kubectl

install_verified \
  "https://github.com/kubernetes-sigs/kind/releases/download/v${kind_version}/kind-linux-amd64" \
  "https://github.com/kubernetes-sigs/kind/releases/download/v${kind_version}/kind-linux-amd64.sha256sum" \
  /usr/local/bin/kind

archive="$(mktemp)"
curl -fsSL "https://get.helm.sh/helm-v${helm_version}-linux-amd64.tar.gz" -o "$archive"
expected="$(curl -fsSL "https://get.helm.sh/helm-v${helm_version}-linux-amd64.tar.gz.sha256sum" | awk '{print $1}')"
echo "$expected  $archive" | sha256sum --check
helm_root="$(mktemp -d)"
tar -xzf "$archive" -C "$helm_root"
sudo install -m 0755 "$helm_root/linux-amd64/helm" /usr/local/bin/helm
rm -rf "$archive" "$helm_root"

go_archive="$(mktemp)"
curl -fsSL "https://go.dev/dl/go${go_version}.linux-amd64.tar.gz" -o "$go_archive"
go_filename="go${go_version}.linux-amd64.tar.gz"
expected="$(
  curl -fsSL 'https://go.dev/dl/?mode=json&include=all' \
    | jq -er --arg filename "$go_filename" \
      '.[] | .files[] | select(.filename == $filename) | .sha256'
)"
echo "$expected  $go_archive" | sha256sum --check
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "$go_archive"
rm -f "$go_archive"
sudo ln -sfn /usr/local/go/bin/go /usr/local/bin/go
sudo ln -sfn /usr/local/go/bin/gofmt /usr/local/bin/gofmt

sudo install -d -m 0755 /etc/cdi /var/run/cdi

echo "Provisioned versions:"
docker version --format '{{.Client.Version}}' || sudo docker version --format '{{.Client.Version}}'
kind version
kubectl version --client
helm version --short
go version
