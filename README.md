# tinyjpg

TinyJPG / TinyPNG 命令行工具。通过 [TinyJPG](https://tinyjpg.com/) API 压缩 JPEG、PNG 和 WebP 图片，并原地覆盖原文件。

## 安装

```bash
go install .
```

需要 Go 1.26+。

## 使用方法

```bash
# 递归压缩当前目录下的图片
tinyjpg

# 递归压缩指定目录下的图片
tinyjpg /path/to/images
```

## 功能说明

1. 递归扫描目标目录，查找 `.jpg`、`.jpeg`、`.png`、`.webp` 文件
2. 将每个文件上传到 `https://tinyjpg.com/backend/opt/shrink` 进行压缩
3. 下载压缩后的文件，原子性替换原文件
4. 终端实时显示上传/下载进度

## 免费版限制

| 限制项 | 数值 |
|--------|------|
| 单文件最大 | 5 MB |
| 最大并发处理数 | 2 |

超过 5 MB 的文件会自动跳过。其他图片会全部处理，同时最多处理 2 张；一个任务完成后会继续处理下一张。

## 输出示例

```
TinyJPG CLI - Compressing images in /path/to/dir
==================================================
Found 3 image(s) to compress (max 2 concurrent)

Uploading photo.jpg: 100%
Downloading photo.jpg: 100%  photo.jpg: 2048 -> 876 bytes (42.8%%, saved 1172 bytes)

Summary:
  Scanned:    3 image(s)
  Skipped:    0 (too large for free tier)
  Attempted:  3
  Successful: 2
  Failed:     1
==================================================
All done!
```

## 许可证

MIT
