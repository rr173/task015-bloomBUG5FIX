# task015-bloom

布隆过滤器（Bloom Filter）服务，仅使用标准库实现：由期望容量 n 与期望误判率 p 经标准公式确定位图长度 m 与哈希个数 k，支持加入、查询与统计，不依赖任何第三方库、数据库或外部服务。

## 本地运行

```bash
go run . server --addr :8080 --capacity 1000 --fp-rate 0.01
go run . --smoke-test
```

主要接口：

- `GET /healthz`：健康检查。
- `POST /add`：提交 `{"item":"..."}`，返回 `{"added":true,"count":N,"bits_set":X}`。缺失 `item` 字段返回 400。
- `POST /test`：提交 `{"item":"..."}`，返回 `{"maybe":bool,"count":N,"bits_set":X,"estimated_fp":p}`。
- `GET /stats`：返回 `m`、`k`、`capacity`、`fp_rate`、`count`、`bits_set`、`fill_ratio`、`estimated_fp`。
- `POST /delete`：标准布隆过滤器不支持删除，返回 400。

## 设计要点

- 位图长度 `m = ⌈-(n·ln p)/(ln 2)²⌉`，哈希个数 `k = max(1, round((m/n)·ln 2))`。
- 多个哈希位置由两个基哈希经双哈希派生（Kirsch-Mitzenmacher）。
- 当前估算误判率由实际位图填充率计算：`estimated_fp = (bits_set / m)^k`，空过滤器为 0。
- 不支持删除：按位清除会破坏无假阴性保证。

## Docker

镜像使用国内 DaoCloud Go 1.26.3 Bookworm builder 和 Alpine 3.20 runtime；支持 `linux/amd64` 与 `linux/arm64` 双架构。
