# tinyjpg

调用 TinyJPG 网页压缩接口，对 JPEG、PNG、WebP 图片做有损压缩。既提供 CLI，也提供可导入的 Go 包。

## 目录

```
cmd/tinyjpg/main.go        CLI
pkg/tinyjpg/tinyjpg.go     压缩库
```

## CLI

从 [Releases](https://github.com/zhuweiyou/tinyjpg/releases) 下载，或直接安装：

```bash
go install github.com/zhuweiyou/tinyjpg/cmd/tinyjpg@latest
```

也可以本地构建：

```bash
go build -o tinyjpg ./cmd/tinyjpg
```

使用：

```bash
./tinyjpg /path/to/images   # 递归扫描目录
./tinyjpg photo.png         # 单张图片
./tinyjpg                   # 当前目录
```

- 支持 `.jpg`、`.jpeg`、`.png`、`.webp`
- 最多同时处理 2 张
- 原子替换原文件
- 超过 5 MB 的文件自动跳过

Windows 可以把图片或目录直接拖到 `tinyjpg.exe` 上。

## 作为库使用

```go
import "github.com/zhuweiyou/tinyjpg/pkg/tinyjpg"

result, err := tinyjpg.Compress(ctx, data, "image/png", nil)
if err != nil {
    // 压缩失败，调用方可以回退到原图
    return
}
compressed := result.Data
```

- `data` 是原始图片字节，`contentType` 例如 `image/png`
- 返回值会校验与输入格式一致，格式不符会返回错误
- `Options` 支持自定义 `HTTPClient` 和上传/下载进度回调

## 限制

- 单张图片最大 5 MB；超过返回 `tinyjpg.ErrTooLarge`
- 使用 TinyJPG 网页接口，无 API key；接口行为可能变化
- 压缩失败时不会修改原文件（CLI 直接报错，库返回错误）
