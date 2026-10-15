Name:           virtualsms
Version:        %{version}
Release:        1%{?dist}.%{flavor}
Summary:        Virtual serial SMS modem service with Go provider plugins
License:        Unspecified
URL:            https://github.com/opsbl/virtualsms

%description
VirtualSMS exposes virtual serial modems and submits SMS through Go provider plugins.

%prep

%build

%install
install -D -m 0755 %{binary} %{buildroot}/usr/bin/virtualsms
for source in %{project_dir}/plugins/*/main.go; do
    [ -f "$source" ] || continue
    name=$(basename "$(dirname "$source")")
    install -D -m 0644 %{plugin_dir}/$name.so %{buildroot}/usr/lib/virtualsms/plugins/$name.so
done
install -D -m 0644 %{project_dir}/virtualsms.service %{buildroot}/usr/lib/systemd/system/virtualsms.service
install -D -m 0600 %{project_dir}/config.example.toml %{buildroot}/etc/virtualsms/config.toml

%files
/usr/bin/virtualsms
/usr/lib/virtualsms/plugins/*.so
/usr/lib/systemd/system/virtualsms.service
%config(noreplace) %attr(0600,root,root) /etc/virtualsms/config.toml
