# rt

rt 是一个基于 **Wails + Go + React** 的 rsync 图形化同步工具。

目标：把 `rsync` 和 `crontab` 封装成更易用的桌面软件：

- 新建 / 编辑 / 删除同步任务
- 使用 `@hourly`、`@daily` 或 5 段 crontab 表达式配置定时同步
- 保存任务时自动写入当前用户的 `crontab`
- 支持立即执行 rsync
- 支持查询每个任务的同步日志
- 黑白灰极简设计，参考 manus.im 的克制风格

## 环境要求

- Go 1.23+
- Node.js / npm
- Wails CLI v2
- rsync
- crontab
- Linux 构建 Wails 桌面包还需要：

```bash
# Ubuntu 22.04 / Debian 12 常见包名
sudo apt install libgtk-3-dev libwebkit2gtk-4.0-dev

# Ubuntu 24.04 包名改为 4.1，构建时需要加 Wails build tag
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
wails build -tags "webkit2_41"
```

本机当前已安装用户态 Go：`~/.local/opt/go/bin/go`，Wails CLI：`~/go/bin/wails`。

## 开发

```bash
export PATH="$HOME/.local/opt/go/bin:$HOME/go/bin:$PATH"
cd ~/github/rt
wails dev
```

## 测试

```bash
export PATH="$HOME/.local/opt/go/bin:$HOME/go/bin:$PATH"
cd ~/github/rt
go test ./...
cd frontend && npm run build
```

## 构建

```bash
export PATH="$HOME/.local/opt/go/bin:$HOME/go/bin:$PATH"
cd ~/github/rt
wails build

# Ubuntu 24.04 使用 WebKitGTK 4.1：
wails build -tags "webkit2_41"
```

如果提示缺少 `gtk+-3.0` / `webkit2gtk-4.0`，Ubuntu 22.04 可安装 `libwebkit2gtk-4.0-dev`；Ubuntu 24.04 安装 `libwebkit2gtk-4.1-dev` 并使用 `-tags "webkit2_41"` 构建。

## 数据位置

rt 使用当前用户目录保存配置和日志：

- 任务配置：`~/.config/rt/jobs.json`
- 同步日志：`~/.config/rt/logs/<job-id>.log`
- crontab 标记：`# rt:<job-id>`

rt 只会管理带有 `# rt:` 标记的 crontab 行，不会删除用户已有的其它 crontab 任务。
