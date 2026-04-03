# 样本分析（广州天气 / deepseek-reasoner-search）

- 样本来源：`/admin/dev/captures` 上游原始 SSE 抓包
- 采集时间（UTC）：2026-04-03 01:28:50
- 原始字节数：41043
- `FINISHED` 字符串出现次数：24
- JSON `data:` chunk 数：420

## 事件分布

- `ready`: 1
- `update_session`: 2
- `finish`: 1

## 高频路径（Top 12）

- `response/fragments/-1/content`: 13
- `response/fragments/-1`: 9
- `response`: 5
- `response/has_pending_fragment`: 4
- `response/fragments/-1/elapsed_secs`: 3
- `response/fragments/-5/status`: 2
- `response/fragments/-6/status`: 2
- `response/fragments/-3/status`: 2
- `response/fragments/-1/status`: 2
- `response/fragments/-4/status`: 2
- `response/fragments/-2/status`: 2
- `response/fragments/-5/results`: 1

## 关键泄露来源

以下状态路径会高频出现 `v=FINISHED`，如果解析器按普通文本透传，就会出现 `FINISHEDFINISHED...` 泄露：

- `response/fragments/-5/status`: 2
- `response/fragments/-6/status`: 2
- `response/fragments/-3/status`: 2
- `response/fragments/-1/status`: 2
- `response/fragments/-4/status`: 2
- `response/fragments/-2/status`: 2
- `response/fragments/-14/status`: 1
- `response/fragments/-12/status`: 1
- `response/fragments/-10/status`: 1
- `response/fragments/-9/status`: 1
- `response/fragments/-8/status`: 1
- `response/fragments/-7/status`: 1
- `response/fragments/-11/status`: 1
- `response/fragments/-16/status`: 1
- `response/fragments/-13/status`: 1
- `response/fragments/-15/status`: 1

## 适配建议

1. 跳过 `response/fragments/<index>/status`（所有 index，而非仅 `-1/-2/-3`）。
2. 保留 `response/status=FINISHED` 用于结束流判定，不应当输出正文。
3. 在样本仿真测试中对全部样本执行“不得输出 `FINISHED`”断言。
