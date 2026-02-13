# 豆包多轮对话CLI
基于火山方舟API的命令行多轮对话工具。

## 快速开始
### 编译
```
go build
```

### 运行
```bash
./doubao-cli --apikey sk-xxxxxx --endpoint ep-xxxxxx --region cn-beijing --timeout 120
```

### 参数说明
- `--apikey`：火山方舟API Key（必填）
- `--endpoint`：火山方舟Endpoint ID（必填）
- `--region`：地域（可选，默认cn-beijing）
- `--timeout`：超时时间（秒，可选，默认120）

## 使用说明
- 必须首先注册并生成自己的apikey和endpoint
- 输入问题即可发起对话，支持多轮上下文
- 输入`q`/`quit`退出程序
- 输入`clear`清空对话上下文
