# tinyjpg

一个使用 Go 编写的命令行图片压缩工具，调用 TinyJPG / TinyPNG 官方压缩接口，对 JPEG、PNG、WebP 图片进行无感压缩，并直接覆盖原文件。

## 特性

- 支持 JPEG、PNG、WebP
- 递归扫描目录
- 自动跳过超过 5 MB 的图片
- 最多同时处理 2 张图片
- 原子替换原文件，避免写入失败导致文件损坏
- 显示实时上传、下载进度

## 安装

```bash
go install github.com/zhuweiyou/tinyjpg@latest
```

## 使用

```bash
# 压缩当前目录
tinyjpg

# 压缩指定目录
tinyjpg /path/to/images
```

## 工作流程

1. 递归扫描目标目录。
2. 查找 `.jpg`、`.jpeg`、`.png`、`.webp` 文件。
3. 上传到 `https://tinyjpg.com/backend/opt/shrink`。
4. 下载压缩后的图片。
5. 原子替换原文件。

## 免费接口限制

- 单张图片最大 5 MB
- 最大并发处理数 2

超过限制的图片会自动跳过，其余图片按并发队列处理。
