# Provider 插件设计

VirtualSMS 使用 Go 原生插件：宿主扫描 `[plugins] dir` 下的 `.so`，读取每个插件导出的 `ProviderID`，按 ID 注册 Provider，与文件名无关。`[instances.*]` 根据 Provider ID 创建独立实例，设备引用实例。插件负责自身配置解析、发送与结构化日志；AT 命令、设备、三秒窗口和统一指标留在宿主。

```text
Zabbix → /dev/ttySMSn → 实例 → AT → Batch → provider.Provider
                                           │
                                           ├─ 内置 log/http
                                           └─ plugin.Open → pluginapi.Provider → 短信网关
```

公开接口位于 `pkg/pluginapi`。插件是 `package main`，必须导出：

```go
var APIVersion = pluginapi.Version
var ProviderID = "ablemobile"

func New(cfg pluginapi.Config) (pluginapi.Provider, error)
```

`ProviderID` 在所有插件中必须唯一，且不能使用内置的 `log`、`http`。`Config` 包含实例名、插件选项的 TOML 字节和 `*slog.Logger`。宿主不认识插件专有字段；插件自行解码、校验、设默认值并记录运行日志。每个实例分别调用一次 `New`。`SendBatch(ctx, Batch)` 返回与 `Batch.To` 等长、顺序一致的 `[]error`。同一实例、同一正文在窗口内合成一个批次；不同实例不合批。`check` 加载插件并验证所有实例选项，不发送短信。Go 插件不能卸载，因此配置修改需要重启服务。插件与宿主运行在同一进程；只加载管理员信任的插件文件。

配置示例：

```toml
[plugins]
dir = "/usr/lib/virtualsms/plugins"

[instances.hk]
provider = "ablemobile"

[instances.hk.options]
username = "account"
password = "secret"
timeout = "30s"

[instances.sg]
provider = "ablemobile"

[instances.sg.options]
username = "another-account"
password = "another-secret"

[[devices]]
instance = "hk"
path = "/dev/ttySMS0"

[[devices]]
instance = "sg"
path = "/dev/ttySMS1"
```

每个插件放在 `plugins/<名称>/main.go`；默认 `make` 自动发现并编译所有插件，`make plugin PLUGIN=<名称>` 只编译指定插件。`make musl` 使用 `musl-gcc`，也接受 `PLUGIN=<名称>`。宿主与插件不能混用不同编译产物。Go 原生插件要求 cgo 外部链接，musl 版的宿主与插件仍依赖 musl 动态运行时。完全静态链接的 musl 宿主无法使用 `plugin.Open`。musl 可降低对目标系统 glibc 版本的依赖，但它是 C 运行时，不代表零 C 依赖。

部署限制：Go 原生插件仅支持 Linux、FreeBSD 和 macOS，且宿主与插件需要相同的 Go 工具链、构建标签和共享依赖版本；VirtualSMS 的 PTY 与 systemd 部署当前面向 Linux。详见 [Go `plugin` 文档](https://pkg.go.dev/plugin)。

`make deb`、`make rpm`、`make deb-musl` 和 `make rpm-musl` 自动打包发现的全部插件，以及宿主、配置和 systemd unit。配置作为可保留的配置文件安装，凭据应由管理员在安装后填写。
