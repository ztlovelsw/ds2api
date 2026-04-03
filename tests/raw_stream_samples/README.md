# 原始流数据样本目录

该目录用于存放**上游真实 SSE 原始流**样本，供本地仿真测试和解析适配使用。

## 目录规范

每个样本一个子目录：

- `meta.json`：样本元信息（问题、模型、采集时间、备注）
- `upstream.stream.sse`：完整原始 SSE 文本（`event:` / `data:` 行）

## 扩展方式

1. 抓取一次真实请求（建议开启 `DS2API_DEV_PACKET_CAPTURE=1`）。
2. 新建 `<sample-id>/` 目录并放入 `meta.json` + `upstream.stream.sse`。
3. 运行独立仿真工具（可被其他测试脚本调用）：

```bash
./tests/scripts/run-raw-stream-sim.sh
```

该工具会自动遍历本目录全部样本，按真实流顺序重放并验证：

- 不会把上游 `status=FINISHED` 片段当正文输出（防泄露）。
- 能正确检测 `response/status=FINISHED` 流结束信号。
- 生成可归档 JSON 报告（`artifacts/raw-stream-sim/`）。

> 注意：样本可能包含搜索结果正文与引用信息，请勿放入敏感账号/密钥。
