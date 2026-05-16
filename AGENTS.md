# TUI-Player 项目指南

## 项目概述

跨平台（Windows/Linux）命令行音乐播放器，使用 Go 语言开发。

## 项目结构

```
TUI-Player/
├── cmd/
│   └── main.go           # 主入口
├── internal/
│   ├── audio/           # 音频解码与播放逻辑
│   ├── player/          # 播放器核心控制
│   ├── playlist/        # 播放列表管理
│   └── ui/              # 命令行用户界面
├── go.mod
└── AGENTS.md
```

## 代码风格

- **导出标识符**：PascalCase（`func PlayMusic()`、`type PlayerStatus`）
- **非导出标识符**：camelCase（`func decodeFile()`、`var currentIndex`）
- **包命名**：小写、简洁、单数形式（`audio`、`player`、`playlist`）
- **导入分组**：标准库 → 第三方包 → 内部包，每组空行分隔
- **错误变量**：使用 `fmt.Errorf()` + `%w` 包装错误链
- **注释语言**：中文注释，代码中关键逻辑处添加注释

## 架构约定

- **`cmd/`**：仅包含各入口点的 `main.go`，保持精简
- **`internal/`**：按功能拆分子包，包间通过接口依赖
- **播放器核心**：`player` 包管理播放状态、队列控制
- **音频层**：`audio` 包封装音频解码，隔离底层库依赖
- **UI 层**：`ui` 包负责终端渲染与用户输入处理
- **跨平台**：通过构建标签（build tags）或接口抽象处理平台差异

## 构建与测试

```bash
# 构建
go build -o tli-player ./cmd/player

# 运行
go run ./cmd/player

# 测试（含覆盖率）
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# 交叉编译
GOOS=windows GOARCH=amd64 go build -o tui-player.exe ./cmd/player
GOOS=linux GOARCH=amd64 go build -o tui-player ./cmd/player

# 代码检查
go vet ./...
```

## 依赖管理

- 使用 Go modules 管理依赖
- 添加依赖：`go get <module-path>`
- 整理依赖：`go mod tidy`

## 错误处理

- 内部错误使用 `fmt.Errorf("上下文信息: %w", err)` 包装
- 用户可见错误使用中文描述
- 启动时的致命错误使用 `log.Fatalf()`
- 运行时错误返回给调用方处理，避免 panic

## 并发模式

- 使用 channel 信号量控制并发数：`sem := make(chan struct{}, maxConcurrency)`
- 配合 `sync.WaitGroup` 等待任务完成
- HTTP 客户端复用连接池配置

## 跨平台注意事项

- 路径操作使用 `filepath.Join()` 而非字符串拼接
- 平台特定代码使用构建标签（`//go:build windows` / `//go:build linux`）
- 优先选择同时支持 Windows 和 Linux 的音频库
