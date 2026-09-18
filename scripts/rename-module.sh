#!/usr/bin/env bash
# Renames the Go module (and the docker image name) after copying the template.
#
#   ./scripts/rename-module.sh github.com/acme/billing-api
set -euo pipefail

old_module="github.com/boyulkim/api-template"
old_name="api-template"

new_module="${1:?usage: $0 <new module path, e.g. github.com/acme/billing-api>}"
new_name="${new_module##*/}"

cd "$(dirname "$0")/.."

files=$(grep -rlF --exclude-dir=.git --exclude-dir=pb_data --exclude-dir=bin "$old_module" . || true)
for f in $files; do
  sed -i.bak "s#${old_module}#${new_module}#g" "$f" && rm -f "$f.bak"
done

sed -i.bak "s#${old_name}:#${new_name}:#g" Makefile && rm -f Makefile.bak

go mod tidy
gofmt -w .
go build ./...

echo "Module renamed to ${new_module}."
