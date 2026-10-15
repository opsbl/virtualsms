#!/usr/bin/env bash
set -euo pipefail

build_dir=$1
version=$2
flavor=$3
top_dir="$(pwd)/build/rpmbuild-${flavor}"
mkdir -p "$top_dir/BUILD" "$top_dir/BUILDROOT" "$top_dir/RPMS" "$top_dir/SOURCES" "$top_dir/SPECS" "$top_dir/SRPMS" build/dist
rpmbuild -bb packaging/virtualsms.spec \
    --define "_topdir $top_dir" \
    --define "version $version" \
    --define "flavor $flavor" \
    --define "binary $(pwd)/$build_dir/virtualsms" \
    --define "plugin_dir $(pwd)/$build_dir/plugins" \
    --define "project_dir $(pwd)"
find "$top_dir/RPMS" -name '*.rpm' -exec cp -v '{}' build/dist/ \;
