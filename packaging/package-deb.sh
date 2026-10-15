#!/usr/bin/env bash
set -euo pipefail

build_dir=$1
version=$2
flavor=$3
arch=$(dpkg --print-architecture)
root="build/package/deb-${flavor}"
output="build/dist/virtualsms_${version}_${arch}_${flavor}.deb"

mkdir -p "$root/DEBIAN" "$root/usr/bin" "$root/usr/lib/virtualsms/plugins" "$root/usr/lib/systemd/system" "$root/etc/virtualsms" build/dist
install -m 0755 "$build_dir/virtualsms" "$root/usr/bin/virtualsms"
find "$root/usr/lib/virtualsms/plugins" -maxdepth 1 -type f -name '*.so' -delete
for source in plugins/*/main.go; do
    [[ -f $source ]] || continue
    name=$(basename "$(dirname "$source")")
    install -m 0644 "$build_dir/plugins/$name.so" "$root/usr/lib/virtualsms/plugins/$name.so"
done
install -m 0644 virtualsms.service "$root/usr/lib/systemd/system/virtualsms.service"
install -m 0600 config.example.toml "$root/etc/virtualsms/config.toml"

if [[ $flavor == musl ]]; then
    depends='musl'
else
    depends='libc6'
fi
cat > "$root/DEBIAN/control" <<EOF
Package: virtualsms
Version: $version-1+$flavor
Section: net
Priority: optional
Architecture: $arch
Maintainer: opsbl <opsbl@users.noreply.github.com>
Depends: $depends
Description: Virtual serial SMS modem service with Go provider plugins
EOF
printf '/etc/virtualsms/config.toml\n' > "$root/DEBIAN/conffiles"
dpkg-deb --root-owner-group --build "$root" "$output"
printf '%s\n' "$output"
